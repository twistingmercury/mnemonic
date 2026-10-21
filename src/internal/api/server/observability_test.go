package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/api/config"
	"github.com/twistingmercury/mnemonic/internal/api/handlers"
	"github.com/twistingmercury/mnemonic/internal/api/handlers/patterns"
	"github.com/twistingmercury/mnemonic/internal/api/service"
	searchsvc "github.com/twistingmercury/mnemonic/internal/api/service/search"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type stubSearch struct{ err error }

// SearchPatterns fails with the configured error, or echoes the caller's query
// back the way the real service does on success.
func (s stubSearch) SearchPatterns(_ context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &searchsvc.SearchResult{Query: opts.Query}, nil
}

func TestProductionHTTPObservability(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		err    error
		panic  bool
	}{
		{"success", 200, nil, false},
		{"internal", 500, errors.New("repository sentinel_failure"), false},
		{"unavailable", 503, fmt.Errorf("%w: sentinel_failure: API returned status 502: provider-secret-body", service.ErrServiceUnavailable), false},
		{"panic", 500, nil, true},
		{"escaped quotes", 500, errors.New(`repository sentinel_failure: "value \"private-escaped-marker\" end"`), false},
		{"escaped single quotes", 500, errors.New(`repository sentinel_failure: 'value \'private-escaped-marker\' end'`), false},
		{"truncated escaped quote", 500, errors.New(`repository sentinel_failure: "value \"private-escaped-marker` + strings.Repeat("x", 5000)), false},
		{"unclosed escaped quote", 500, errors.New(`repository sentinel_failure: "value \"private-escaped-marker`), false},
		{"invalid", 400, service.ErrInvalidInput, false},
		{"missing", 404, service.ErrNotFound, false},
		{"conflict", 409, service.ErrConflict, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exporter := useTraceExporter(t)
			router, logs, reader := newObservedRouter(t)
			var active trace.SpanContext
			router.Use(func(c *gin.Context) { active = trace.SpanContextFromContext(c.Request.Context()); c.Next() })
			if tc.panic {
				router.GET("/v1/api/patterns/search", func(*gin.Context) { panic("sentinel_failure password=private-panic-secret") })
			} else {
				h := patterns.New(nil, stubSearch{tc.err}, config.VocabularyConfig{})
				h.RegisterRoutes(router.Group("/v1/api"))
			}
			req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search?q=private-query-secret", strings.NewReader("private-body-secret"))
			req.Header.Set("Authorization", "Bearer private-header-secret")
			req.Header.Set("X-Request-ID", "request-distinct-123")
			req.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, "request-distinct-123", w.Header().Get("X-Request-ID"))
			const wantTraceID = "0123456789abcdef0123456789abcdef"
			require.Equal(t, wantTraceID, active.TraceID().String())
			var problem handlers.ProblemDetail
			if tc.status >= 400 {
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &problem))
				assert.Equal(t, tc.status, problem.Status)
				assert.Equal(t, req.URL.Path, problem.Instance)
				assert.Equal(t, active.TraceID().String(), problem.TraceID)
				assert.Equal(t, wantTraceID, problem.TraceID)
				assert.NotEqual(t, w.Header().Get("X-Request-ID"), problem.TraceID)
			} else {
				// A 2xx search returns the real response body, not a problem document.
				var body struct {
					Results  []json.RawMessage `json:"results"`
					Metadata struct {
						Query string `json:"query"`
					} `json:"metadata"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Empty(t, body.Results)
				// Echoing the caller's own query is the contract, not a leak.
				assert.Equal(t, "private-query-secret", body.Metadata.Query)
				assert.NotContains(t, w.Body.String(), "traceId")
			}
			spans := exporter.GetSpans()
			require.Len(t, spans, 1)
			assert.Equal(t, active, spans[0].SpanContext)
			assert.Equal(t, "0123456789abcdef", spans[0].Parent.SpanID().String())
			assert.True(t, spans[0].Parent.IsRemote())
			var log map[string]any
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			require.Len(t, lines, 1)
			require.NoError(t, json.Unmarshal([]byte(lines[0]), &log))
			assert.Equal(t, "request completed", log["message"])
			assert.Equal(t, float64(tc.status), log["status"])
			assert.Equal(t, wantTraceID, log["trace_id"])
			assert.Equal(t, active.SpanID().String(), log["span_id"])
			assert.Equal(t, "request-distinct-123", log["request_id"])
			assert.Equal(t, "/v1/api/patterns/search", log["route"])
			if tc.status >= 500 {
				assert.Equal(t, "error", log["level"])
				assert.Contains(t, log["error"], "sentinel_failure")
				assert.Equal(t, codes.Error, spans[0].Status.Code)
				require.Len(t, spans[0].Events, 1)
				assert.Equal(t, "exception", spans[0].Events[0].Name)
				assert.Contains(t, fmt.Sprint(spans[0].Events[0].Attributes), "sentinel_failure")
				assert.NotContains(t, w.Body.String(), "sentinel_failure")
				detail := "an unexpected error occurred"
				if tc.status == 503 {
					detail = "service temporarily unavailable"
				}
				assert.Equal(t, detail, problem.Detail)
			} else {
				assert.Equal(t, "info", log["level"])
				assert.NotContains(t, log, "error")
				assert.NotEqual(t, codes.Error, spans[0].Status.Code)
				assert.Empty(t, spans[0].Events)
			}
			for _, secret := range []string{"private-body-secret", "private-header-secret", "private-panic-secret", "provider-secret-body", "private-escaped-marker"} {
				assert.NotContains(t, logs.String(), secret)
				assert.NotContains(t, fmt.Sprint(spans), secret)
				assert.NotContains(t, w.Body.String(), secret)
			}
			// The query string reaches no log or span on any path; only a 2xx body echoes it.
			assert.NotContains(t, logs.String(), "private-query-secret")
			assert.NotContains(t, fmt.Sprint(spans), "private-query-secret")
			if tc.status >= 400 {
				assert.NotContains(t, w.Body.String(), "private-query-secret")
			}
			var data metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(context.Background(), &data))
			assertCompletionMetrics(t, data, tc.status)
		})
	}
}

// useTraceExporter installs an in-memory tracer provider and the W3C trace
// context propagator as globals for the test, and returns the span exporter.
func useTraceExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	oldTP, oldProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(oldTP)
		otel.SetTextMapPropagator(oldProp)
		require.NoError(t, tp.Shutdown(context.Background()))
	})
	return exporter
}

// newObservedRouter builds the production router with a manual metric reader
// installed as the global meter provider and logs captured in a buffer.
func newObservedRouter(t *testing.T) (*gin.Engine, *bytes.Buffer, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, mp.Shutdown(context.Background())) })
	useMeterProvider(t, mp)
	logs := new(bytes.Buffer)
	return setupRouter(zerolog.New(logs)), logs, reader
}

// TestPanicLeavesRouterUsable dispatches a healthy request through the same
// router and its pooled Gin contexts after a recovered panic, and checks that
// none of the panic's trace, error or log state carries over.
func TestPanicLeavesRouterUsable(t *testing.T) {
	exporter := useTraceExporter(t)
	router, logs, reader := newObservedRouter(t)
	var active trace.SpanContext
	router.Use(func(c *gin.Context) { active = trace.SpanContextFromContext(c.Request.Context()); c.Next() })
	router.GET("/v1/api/patterns/search", func(*gin.Context) { panic("sentinel_failure") })
	router.GET("/ready", func(c *gin.Context) { c.String(http.StatusOK, "ready") })

	panicReq := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search", nil)
	panicReq.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	router.ServeHTTP(httptest.NewRecorder(), panicReq)

	healthyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	healthyReq.Header.Set("X-Request-ID", "healthy-request-456")
	healthy := httptest.NewRecorder()
	router.ServeHTTP(healthy, healthyReq)
	require.Equal(t, http.StatusOK, healthy.Code)
	assert.Equal(t, "ready", healthy.Body.String())
	assert.Equal(t, "healthy-request-456", healthy.Header().Get("X-Request-ID"))

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	successSpan := spans[1]
	assert.True(t, successSpan.SpanContext.IsValid())
	assert.Equal(t, active, successSpan.SpanContext)
	assert.NotEqual(t, spans[0].SpanContext.TraceID(), successSpan.SpanContext.TraceID())
	assert.False(t, successSpan.Parent.IsValid())
	assert.Equal(t, codes.Unset, successSpan.Status.Code)
	assert.Empty(t, successSpan.Status.Description)
	assert.Empty(t, successSpan.Events)
	assert.NotContains(t, fmt.Sprint(successSpan), "sentinel_failure")

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 2)
	var successLog map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &successLog))
	assert.Equal(t, "info", successLog["level"])
	assert.Equal(t, float64(http.StatusOK), successLog["status"])
	assert.Equal(t, "request completed", successLog["message"])
	assert.Equal(t, "/ready", successLog["route"])
	assert.Equal(t, "healthy-request-456", successLog["request_id"])
	assert.Equal(t, active.TraceID().String(), successLog["trace_id"])
	assert.Equal(t, active.SpanID().String(), successLog["span_id"])
	assert.NotContains(t, successLog, "error")
	assert.NotContains(t, lines[1], "sentinel_failure")

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	assertCompletionMetrics(t, data, http.StatusInternalServerError, "/ready")
}

// useMeterProvider installs mp as the global meter provider for the test, the
// way otelx does in production, so otelgin records into it.
func useMeterProvider(t *testing.T, mp *sdkmetric.MeterProvider) {
	t.Helper()
	old := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(old) })
}

// assertCompletionMetrics checks that otelgin recorded exactly one
// http.server.request.duration observation for the search route with status,
// plus one 200 for each of successRoutes.
func assertCompletionMetrics(t *testing.T, data metricdata.ResourceMetrics, status int, successRoutes ...string) {
	t.Helper()
	type series struct {
		route  string
		status int64
	}
	want := map[series]bool{{"/v1/api/patterns/search", int64(status)}: true}
	for _, route := range successRoutes {
		want[series{route, http.StatusOK}] = true
	}
	var found bool
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "http.server.request.duration" {
				continue
			}
			found = true
			hist := m.Data.(metricdata.Histogram[float64])
			require.Len(t, hist.DataPoints, len(want))
			for _, point := range hist.DataPoints {
				route, _ := point.Attributes.Value("http.route")
				code, _ := point.Attributes.Value("http.response.status_code")
				assert.True(t, want[series{route.AsString(), code.AsInt64()}], "unexpected duration series: %v", point.Attributes)
				assert.EqualValues(t, 1, point.Count)
			}
		}
	}
	assert.True(t, found, "otelgin should record http.server.request.duration")
}

// TestRequestIDBoundedAlphabet pins the documented X-Request-ID contract: echo an
// identifier of up to 128 letters, digits, dots, underscores or hyphens, and
// replace anything else with a generated UUID. Deleting the character check while
// keeping the length check must fail here.
func TestRequestIDBoundedAlphabet(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		echoed       bool
	}{
		{"mixed valid alphabet", "Valid.id-123_OK", true},
		{"maximum length", strings.Repeat("a", 128), true},
		{"over maximum length", strings.Repeat("a", 129), false},
		{"absent", "", false},
		{"crlf injection", "abc\r\nX-Injected: evil", false},
		{"control character", "abc\x00def", false},
		{"embedded space", "abc def", false},
		{"markup", "<script>", false},
		{"non-ascii", "abcdéf", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, logs, _ := newObservedRouter(t)
			router.GET("/echo", func(c *gin.Context) { c.Status(http.StatusOK) })
			req := httptest.NewRequest(http.MethodGet, "/echo", nil)
			if tc.header != "" {
				req.Header.Set("X-Request-ID", tc.header)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			got := w.Header().Get("X-Request-ID")
			if tc.echoed {
				require.Equal(t, tc.header, got)
			} else {
				require.NotEqual(t, tc.header, got)
				// A rejected identifier is replaced by a generated UUID.
				assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, got)
			}
			// The logged request_id is whatever was echoed, so a rejected value
			// reaches neither the response header nor the log stream.
			var log map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &log))
			assert.Equal(t, got, log["request_id"])
		})
	}
}

func TestProductionHTTPNoTraceAndSkips(t *testing.T) {
	oldTP := otel.GetTracerProvider()
	// A noop provider supplies no locally active span or invented trace identity.
	otel.SetTracerProvider(trace.NewNoopTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(oldTP) })
	router, logs, reader := newObservedRouter(t)
	router.GET("/health", func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	assert.Equal(t, 200, w.Code)
	assert.Empty(t, logs.String())
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	assert.Empty(t, data.ScopeMetrics)
	router.GET("/failure", func(c *gin.Context) { handlers.RespondError(c, errors.New("failure")) })
	req := httptest.NewRequest("GET", "/failure", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("x", 129))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var problem handlers.ProblemDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &problem))
	assert.Empty(t, problem.TraceID)
	assert.NotContains(t, w.Body.String(), "traceId")
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
	assert.Len(t, w.Header().Get("X-Request-ID"), 36)
	assert.NotContains(t, logs.String(), "trace_id")
}

// TestTracingSkipsHealth verifies that a normal route records a server span
// and the /health probe does not.
func TestTracingSkipsHealth(t *testing.T) {
	exporter := useTraceExporter(t)
	router := setupRouter(zerolog.Nop())
	for _, path := range []string{"/health", "/v1/api/thing"} {
		router.GET(path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Empty(t, exporter.GetSpans(), "/health must not be traced")

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/api/thing", nil))
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, trace.SpanKindServer, spans[0].SpanKind)
	assert.Contains(t, spans[0].Name, "/v1/api/thing")
}
