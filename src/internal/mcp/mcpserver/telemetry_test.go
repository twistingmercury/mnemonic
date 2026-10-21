package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/twistingmercury/librarian/internal/mcp/config"
	"github.com/twistingmercury/librarian/internal/mcp/metrics"
	patternrepo "github.com/twistingmercury/librarian/internal/mcp/repository/pattern"
	searchsvc "github.com/twistingmercury/librarian/internal/mcp/service/search"
)

// The package tracer binds to the first global provider set, so every test
// shares one provider and swaps the recorder its spans go to.
var (
	tracingOnce  sync.Once
	spanRecorder atomic.Pointer[tracetest.SpanRecorder]
)

// swapProcessor forwards ended spans to the current test's recorder.
type swapProcessor struct{ sdktrace.SpanProcessor }

func (swapProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if r := spanRecorder.Load(); r != nil {
		r.OnEnd(s)
	}
}

// recordSpans routes spans to a fresh recorder for this test and sets the
// W3C trace context propagator. Tests that use it must not run in parallel.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	tracingOnce.Do(func() {
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(swapProcessor{tracetest.NewSpanRecorder()})))
		otel.SetTextMapPropagator(propagation.TraceContext{})
	})
	recorder := tracetest.NewSpanRecorder()
	spanRecorder.Store(recorder)
	t.Cleanup(func() { spanRecorder.Store(nil) })
	return recorder
}

// TestToolCallTelemetry drives real tool calls over Streamable HTTP and checks
// the HTTP span, the tool span beneath it, and the tool metrics. It must not
// run in parallel.
func TestToolCallTelemetry(t *testing.T) {
	recorder := recordSpans(t)

	reader := sdkmetric.NewManualReader()
	toolMetrics, err := metrics.NewTools(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	missing := uuid.New()
	related := uuid.New()
	svc := Services{
		SearchPatterns:      searchStub(t, nil, &searchsvc.SearchResult{}, nil),
		GetPatternWithGraph: getPatternStub(t, missing, nil, nil, patternrepo.ErrNotFound),
		FindRelatedPatterns: findRelatedStub(t, related, defaultRelatedLimit, "", nil, errors.New("neo4j down")),
	}

	cfg := config.MCPConfig{DefaultSearchThreshold: 0.5}
	srv := NewMCPHTTPServer(cfg, "localhost", NewMCPHTTPHandler(NewMCPServer(svc, zerolog.Nop(), cfg, toolMetrics)))
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", MaxRetries: -1}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	call := func(name string, args map[string]any) {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err)
	}
	call("search_patterns", map[string]any{"query": "retry with backoff"})
	// Arguments that fail the input schema are rejected by the SDK.
	call("search_patterns", map[string]any{})
	call("search_patterns", map[string]any{"query": 42})
	call("get_pattern", map[string]any{"id": missing.String()})
	call("get_pattern", map[string]any{"id": "not-a-uuid"})
	call("find_related_patterns", map[string]any{"pattern_id": related.String()})

	// Metrics: one record per call, by tool and outcome.
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "mnemonic.mcp.tool_calls" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				tool, _ := dp.Attributes.Value("tool")
				outcome, _ := dp.Attributes.Value("outcome")
				got[tool.AsString()+"/"+outcome.AsString()] = dp.Value
			}
		}
	}
	assert.Equal(t, map[string]int64{
		"search_patterns/ok":                1,
		"search_patterns/invalid_input":     2,
		"get_pattern/not_found":             1,
		"get_pattern/invalid_input":         1,
		"find_related_patterns/unavailable": 1,
	}, got)

	// Traces: each tool span is a child of an /mcp HTTP server span. otelhttp
	// ends that span after the handler returns, which can be after the client
	// has read the reply, so wait until every tool span's parent has ended.
	var spans []sdktrace.ReadOnlySpan
	byID := map[string]sdktrace.ReadOnlySpan{}
	require.Eventually(t, func() bool {
		spans = recorder.Ended()
		clear(byID)
		for _, s := range spans {
			byID[s.SpanContext().SpanID().String()] = s
		}
		for _, s := range spans {
			if _, ok := byID[s.Parent().SpanID().String()]; s.Parent().IsValid() && !ok {
				return false
			}
		}
		return true
	}, 2*time.Second, 5*time.Millisecond)
	status := map[string]codes.Code{}
	for _, s := range spans {
		if s.Name() != "tools/call search_patterns" && s.Name() != "tools/call find_related_patterns" {
			continue
		}
		parent, ok := byID[s.Parent().SpanID().String()]
		require.True(t, ok, "%s has a recorded parent", s.Name())
		assert.Equal(t, "POST /mcp", parent.Name())
		assert.Equal(t, s.SpanContext().TraceID(), parent.SpanContext().TraceID())
		status[s.Name()] = s.Status().Code
	}
	assert.Equal(t, map[string]codes.Code{
		"tools/call search_patterns":       codes.Unset,
		"tools/call find_related_patterns": codes.Error,
	}, status)
}

func TestToolOutcome(t *testing.T) {
	t.Parallel()
	assert.Equal(t, outcomeOK, toolOutcome(nil))
	assert.Equal(t, outcomeInvalid, toolOutcome(ErrInvalidInput))
	assert.Equal(t, outcomeInvalid, toolOutcome(errors.New(`validating "arguments": validating root: required: missing properties: ["query"]`)))
	assert.Equal(t, outcomeNotFound, toolOutcome(mapServiceError(patternrepo.ErrNotFound)))
	assert.Equal(t, outcomeUnavailable, toolOutcome(mapServiceError(errors.New("boom"))))
}

// TestToolCallTraceFromMeta checks that a traceparent in the request's _meta
// parents the tool span, with a link back to the /mcp HTTP span, and that the
// HTTP span carries the MCP method and protocol version. It must not run in
// parallel.
func TestToolCallTraceFromMeta(t *testing.T) {
	recorder := recordSpans(t)

	url := newTestEndpoint(t)
	const clientTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	meta := modernMeta()
	meta["traceparent"] = "00-" + clientTrace + "-00f067aa0ba902b7-01"
	status, _ := post(t, url,
		map[string]string{"MCP-Protocol-Version": protocolVersion, "Mcp-Method": "tools/call", "Mcp-Name": "search_patterns"},
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
			"name": "search_patterns", "arguments": map[string]any{"query": "x"}, "_meta": meta,
		}})
	require.Equal(t, http.StatusOK, status)

	var toolSpan, httpSpan sdktrace.ReadOnlySpan
	require.Eventually(t, func() bool {
		for _, s := range recorder.Ended() {
			switch s.Name() {
			case "tools/call search_patterns":
				toolSpan = s
			case "POST /mcp":
				httpSpan = s
			}
		}
		return toolSpan != nil && httpSpan != nil
	}, 2*time.Second, 5*time.Millisecond)

	assert.Equal(t, clientTrace, toolSpan.SpanContext().TraceID().String())
	assert.Equal(t, "00f067aa0ba902b7", toolSpan.Parent().SpanID().String())
	require.Len(t, toolSpan.Links(), 1)
	assert.Equal(t, httpSpan.SpanContext().SpanID(), toolSpan.Links()[0].SpanContext.SpanID())

	attrs := map[string]string{}
	for _, kv := range httpSpan.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	assert.Equal(t, "tools/call", attrs["mcp.method.name"])
	assert.Equal(t, protocolVersion, attrs["mcp.protocol.version"])
}
