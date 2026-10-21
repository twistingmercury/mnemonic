package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/api/handlers"
	"github.com/twistingmercury/librarian/internal/api/service"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestEncodeCursor_DecodeCursorStrict_Roundtrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		offset int
	}{
		{name: "zero offset", offset: 0},
		{name: "positive offset", offset: 20},
		{name: "large offset", offset: 10000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cursor := handlers.EncodeCursor(tt.offset)
			assert.NotEmpty(t, cursor)
			got, ok := handlers.DecodeCursorStrict(cursor)
			assert.True(t, ok)
			assert.Equal(t, tt.offset, got)
		})
	}
}

func TestDecodeCursorStrict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		cursor string
		wantOK bool
	}{
		{name: "empty string is the first page", cursor: "", wantOK: true},
		{name: "invalid base64", cursor: "not-valid-base64!!!", wantOK: false},
		{name: "valid base64 but invalid json", cursor: "aGVsbG8=", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := handlers.DecodeCursorStrict(tt.cursor)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, 0, got)
		})
	}
}

func TestRespondError_ServiceErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
	}{
		{
			name:       "not found",
			err:        service.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantType:   "not-found",
		},
		{
			name:       "wrapped not found",
			err:        errors.Join(service.ErrNotFound, errors.New("agent not found")),
			wantStatus: http.StatusNotFound,
			wantType:   "not-found",
		},
		{
			name:       "conflict",
			err:        service.ErrConflict,
			wantStatus: http.StatusConflict,
			wantType:   "conflict",
		},
		{
			name:       "invalid input",
			err:        service.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
			wantType:   "validation-error",
		},
		{
			name:       "service unavailable",
			err:        service.ErrServiceUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantType:   "service-unavailable",
		},
		{
			name:       "unknown error",
			err:        errors.New("internal-sentinel-detail"),
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal-error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

			handlers.RespondError(c, tt.err)

			assert.Equal(t, tt.wantStatus, w.Code)
			var problem handlers.ProblemDetail
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &problem))
			assert.Equal(t, handlers.ProblemBaseURI+tt.wantType, problem.Type)
			assert.Equal(t, tt.wantStatus, problem.Status)
			assert.Equal(t, "/test", problem.Instance)
			assert.Empty(t, problem.TraceID)
			assert.Empty(t, problem.Errors)
			switch tt.wantStatus {
			case 500:
				assert.Equal(t, "Internal Error", problem.Title)
				assert.Equal(t, "an unexpected error occurred", problem.Detail)
				assert.NotContains(t, w.Body.String(), tt.err.Error())
			case 503:
				assert.Equal(t, "Service Unavailable", problem.Title)
				assert.Equal(t, "service temporarily unavailable", problem.Detail)
			case 400:
				assert.Equal(t, "Validation Error", problem.Title)
				assert.Equal(t, tt.err.Error(), problem.Detail)
			default:
				assert.Equal(t, http.StatusText(tt.wantStatus), problem.Title)
				assert.Equal(t, tt.err.Error(), problem.Detail)
			}
			if tt.wantStatus < 500 {
				assert.Empty(t, c.Errors)
			} else {
				require.Len(t, c.Errors, 1)
			}
		})
	}
}

func TestParseIntQueryStrict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		query  string
		want   int
		wantOK bool
	}{
		{name: "missing param uses default", query: "", want: 20, wantOK: true},
		{name: "valid param", query: "50", want: 50, wantOK: true},
		{name: "below min", query: "0", want: 20, wantOK: false},
		{name: "above max", query: "500", want: 20, wantOK: false},
		{name: "non-numeric", query: "abc", want: 20, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/test?limit="+tt.query, nil)

			got, ok := handlers.ParseIntQueryStrict(c, "limit", 20, 1, 100)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRespondValidationTraceIdentity(t *testing.T) {
	t.Parallel()
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "active"}[valid], func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/test", nil)
			c.Request.Header.Set("X-Request-ID", "request-id-is-not-trace")
			var want string
			if valid {
				traceID, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
				require.NoError(t, err)
				spanID, err := trace.SpanIDFromHex("0123456789abcdef")
				require.NoError(t, err)
				c.Request = c.Request.WithContext(trace.ContextWithSpanContext(c.Request.Context(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})))
				want = traceID.String()
			}
			fields := []handlers.FieldError{{Field: "name", Code: "REQUIRED", Message: "name is required"}}
			handlers.RespondValidationError(c, "invalid fields", fields)
			var problem handlers.ProblemDetail
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &problem))
			assert.Equal(t, handlers.ProblemDetail{Type: handlers.ProblemBaseURI + "validation-error", Title: "Validation Error", Status: 400, Detail: "invalid fields", Instance: "/test", TraceID: want, Errors: fields}, problem)
			assert.NotContains(t, w.Body.String(), "request-id-is-not-trace")
		})
	}
}
