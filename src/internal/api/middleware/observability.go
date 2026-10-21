package middleware

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

const diagnosticLimit = 512

var (
	upstreamBody     = regexp.MustCompile(`(?is)(API returned status [0-9]{3}):.*`)
	diagnosticURL    = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s]+`)
	diagnosticBearer = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	diagnosticSecret = regexp.MustCompile(`(?is)\b(authorization|bearer|password|passwd|token|api[_-]?key|secret|content|body|query)\b(?:\s*[:=]|\s+).*`)
)

// RecordFailure attaches a bounded failure diagnostic for completion logging and tracing.
// Raw upstream bodies, URLs, quoted values and credential fields are excluded.
func RecordFailure(c *gin.Context, err error) {
	if err == nil {
		return
	}
	safe := errors.New(safeDiagnostic(err.Error()))
	c.Errors = nil
	_ = c.Error(safe)
}

func safeDiagnostic(message string) string {
	// Bound work before applying the redaction rules as well as bounding output.
	if len(message) > 4096 {
		message = message[:4096]
	}
	message = upstreamBody.ReplaceAllString(message, "$1: [redacted]")
	message = diagnosticURL.ReplaceAllString(message, "[redacted URL]")
	message = redactQuoted(message)
	message = diagnosticBearer.ReplaceAllString(message, "Bearer [redacted]")
	message = diagnosticSecret.ReplaceAllString(message, "$1=[redacted]")
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	if len(message) > diagnosticLimit {
		message = message[:diagnosticLimit]
	}
	return strings.ToValidUTF8(message, "")
}

// redactQuoted omits quoted values, honoring backslash-escaped delimiters.
// An unfinished value (including a trailing escape) is redacted to the end.
// Its input is already bounded by safeDiagnostic.
func redactQuoted(message string) string {
	var result strings.Builder
	for i := 0; i < len(message); i++ {
		delimiter := message[i]
		if delimiter != '\'' && delimiter != '"' && delimiter != '`' {
			result.WriteByte(delimiter)
			continue
		}
		result.WriteString("[redacted]")
		for i++; i < len(message); i++ {
			if message[i] == '\\' {
				// Consume the escaped byte, so an escaped quote cannot close the value.
				i++
			} else if message[i] == delimiter {
				break
			}
		}
	}
	return result.String()
}

// CompletionLogging emits one request summary after inner recovery has run.
// It records route templates instead of paths and never records request content.
func CompletionLogging(logger zerolog.Logger, skipPaths []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		// Other middleware may also attach errors. Retain one bounded failure
		// before the outer tracing middleware can consume Gin's error state.
		if len(c.Errors) > 0 {
			c.Errors = c.Errors[:1]
			c.Errors[0].Err = errors.New(safeDiagnostic(c.Errors[0].Err.Error()))
			c.Errors[0].Meta = nil
			if c.Writer.Status() >= 500 {
				trace.SpanFromContext(c.Request.Context()).RecordError(c.Errors[0].Err)
			}
		}
		if slices.Contains(skipPaths, c.Request.URL.Path) && c.Writer.Status() < 500 {
			return
		}
		event := logger.Info()
		if c.Writer.Status() >= 500 {
			event = logger.Error()
		}
		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		event.Str("method", c.Request.Method).Str("route", route).
			Int("status", c.Writer.Status()).Dur("duration_ms", time.Since(start))
		if rid := c.Writer.Header().Get("X-Request-ID"); rid != "" {
			event.Str("request_id", rid)
		}
		sc := trace.SpanContextFromContext(c.Request.Context())
		if sc.IsValid() {
			event.Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String())
		}
		if len(c.Errors) > 0 {
			event.Str("error", c.Errors[0].Error())
		}
		event.Msg("request completed")
	}
}
