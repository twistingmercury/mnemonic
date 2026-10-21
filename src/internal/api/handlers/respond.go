package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/twistingmercury/mnemonic/internal/api/middleware"
	"github.com/twistingmercury/mnemonic/internal/api/service"
	"go.opentelemetry.io/otel/trace"
)

// ProblemBaseURI is the base URI for problem type URIs.
const ProblemBaseURI = "https://mnemonic.example.com/problems/"

// ProblemDetail represents an RFC 7807 Problem Details response.
// @Description RFC 7807 Problem Details error response
type ProblemDetail struct {
	Type     string       `json:"type"             example:"https://mnemonic.example.com/problems/not-found"`
	Title    string       `json:"title"            example:"Not Found"`
	Status   int          `json:"status"           example:"404"`
	Detail   string       `json:"detail,omitempty" example:"pattern not found"`
	Instance string       `json:"instance,omitempty" example:"/v1/api/patterns/my-pattern"`
	TraceID  string       `json:"traceId,omitempty"  example:"trace-abc123"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// FieldError represents a single field-level validation error.
// @Description Field-level validation error returned inside a ProblemDetail
type FieldError struct {
	Field   string `json:"field"   example:"name"`
	Code    string `json:"code"    example:"REQUIRED" enums:"REQUIRED,MAX_LENGTH,MAX_ITEMS,TOO_LARGE,INVALID_FORMAT,INVALID_VALUE"`
	Message string `json:"message" example:"name is required"`
}

// Pagination represents cursor-based pagination metadata.
// @Description Cursor-based pagination metadata returned with list responses
type Pagination struct {
	Limit      int     `json:"limit"       example:"100"`
	Cursor     *string `json:"cursor"      example:"dGVzdA=="`
	NextCursor *string `json:"next_cursor" example:"dGVzdA=="`
	HasMore    bool    `json:"has_more"    example:"false"`
}

// cursorPayload encodes pagination state as an opaque string.
type cursorPayload struct {
	Offset int `json:"o"`
}

// EncodeCursor creates an opaque cursor from an offset.
func EncodeCursor(offset int) string {
	data, _ := json.Marshal(cursorPayload{Offset: offset})
	return base64.URLEncoding.EncodeToString(data)
}

// RespondError maps a service error to an RFC 7807 Problem Details response.
// Errors that map to a 5xx are recorded for the completion log and span, and
// their text is not sent to the client.
func RespondError(c *gin.Context, err error) {
	status, slug, title, detail := http.StatusInternalServerError, "internal-error", "Internal Error", "an unexpected error occurred"
	switch {
	case errors.Is(err, service.ErrNotFound):
		status, slug, title, detail = http.StatusNotFound, "not-found", "Not Found", err.Error()
	case errors.Is(err, service.ErrConflict):
		status, slug, title, detail = http.StatusConflict, "conflict", "Conflict", err.Error()
	case errors.Is(err, service.ErrInvalidInput):
		status, slug, title, detail = http.StatusBadRequest, "validation-error", "Validation Error", err.Error()
	case errors.Is(err, service.ErrServiceUnavailable):
		status, slug, title, detail = http.StatusServiceUnavailable, "service-unavailable", "Service Unavailable", "service temporarily unavailable"
	}
	if status >= http.StatusInternalServerError {
		middleware.RecordFailure(c, err)
	}

	c.JSON(status, ProblemDetail{
		Type:     ProblemBaseURI + slug,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: c.Request.URL.Path,
		TraceID:  activeTraceID(c),
	})
}

// RespondValidationError returns a 400 response with field-level validation errors.
func RespondValidationError(c *gin.Context, detail string, fieldErrors []FieldError) {
	traceID := activeTraceID(c)
	instance := c.Request.URL.Path

	c.JSON(http.StatusBadRequest, ProblemDetail{
		Type:     ProblemBaseURI + "validation-error",
		Title:    "Validation Error",
		Status:   http.StatusBadRequest,
		Detail:   detail,
		Instance: instance,
		TraceID:  traceID,
		Errors:   fieldErrors,
	})
}

// ParseIntQueryStrict returns (value, true) or (defaultVal, false) when the raw value is out of [minVal, maxVal].
// An empty string returns (defaultVal, true).
func ParseIntQueryStrict(c *gin.Context, key string, defaultVal, minVal, maxVal int) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal, true
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val < minVal || val > maxVal {
		return defaultVal, false
	}
	return val, true
}

// DecodeCursorStrict returns (offset, true). An empty cursor returns (0, true).
// A non-empty, non-decodable cursor returns (0, false).
func DecodeCursorStrict(cursor string) (int, bool) {
	if cursor == "" {
		return 0, true
	}
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, false
	}
	var payload cursorPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, false
	}
	return payload.Offset, true
}

// activeTraceID omits trace identity when no valid span context exists.
func activeTraceID(c *gin.Context) string {
	sc := trace.SpanContextFromContext(c.Request.Context())
	if sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}
