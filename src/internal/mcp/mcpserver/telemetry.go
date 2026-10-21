package mcpserver

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/librarian-mcp/internal/metrics"
)

var tracer = otel.Tracer("mnemonic/mcp")

// Tool call outcomes recorded on spans and metrics.
const (
	outcomeOK          = "ok"
	outcomeInvalid     = "invalid_input"
	outcomeNotFound    = "not_found"
	outcomeUnavailable = "unavailable"
)

// toolTelemetry returns middleware that gives every tools/call request a span
// (a child of the /mcp HTTP span, or of the trace context the client put in
// the request's _meta) and a count and latency record by tool and outcome. Only unavailable outcomes mark the
// span as an error; invalid input and unknown patterns are caller mistakes.
func toolTelemetry(toolMetrics *metrics.Tools) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok {
				return next(ctx, method, req)
			}

			tool := call.Params.Name
			start := time.Now()
			opts := []trace.SpanStartOption{trace.WithAttributes(attribute.String("mcp.tool.name", tool))}
			ctx, opts = metaTraceParent(ctx, call.Params.Meta, opts)
			ctx, span := tracer.Start(ctx, "tools/call "+tool, opts...)
			defer span.End()

			res, err := next(ctx, method, req)

			toolErr := err
			if r, ok := res.(*mcp.CallToolResult); ok && r != nil && r.IsError && toolErr == nil {
				toolErr = r.GetError()
			}

			outcome := toolOutcome(toolErr)
			span.SetAttributes(attribute.String("mcp.tool.outcome", outcome))
			if toolErr != nil {
				span.RecordError(toolErr)
				if outcome == outcomeUnavailable {
					span.SetStatus(codes.Error, toolErr.Error())
				}
			}
			toolMetrics.Record(ctx, tool, outcome, time.Since(start))
			return res, err
		}
	}
}

// metaTraceParent returns ctx carrying the trace context from a request's
// _meta (traceparent, tracestate and baggage, per the 2026-07-28 spec) when it
// holds a valid one from a different trace than the /mcp HTTP span. The tool
// span then joins the client's trace, with a link back to the HTTP span.
// Otherwise ctx and opts are returned unchanged.
func metaTraceParent(ctx context.Context, meta mcp.Meta, opts []trace.SpanStartOption) (context.Context, []trace.SpanStartOption) {
	carrier := propagation.MapCarrier{}
	for _, k := range []string{"traceparent", "tracestate", "baggage"} {
		if v, ok := meta[k].(string); ok {
			carrier[k] = v
		}
	}
	if carrier["traceparent"] == "" {
		return ctx, opts
	}
	metaCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)
	remote := trace.SpanContextFromContext(metaCtx)
	httpSpan := trace.SpanContextFromContext(ctx)
	if !remote.IsValid() || remote.TraceID() == httpSpan.TraceID() {
		return ctx, opts
	}
	if httpSpan.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: httpSpan}))
	}
	return metaCtx, opts
}

// sdkArgumentsErrorPrefix starts the error go-sdk sets on the result when the
// arguments fail the tool's input schema (missing required field, wrong type,
// out-of-range value). The SDK formats the cause with %v, so the prefix is
// the only thing to match (go-sdk v1.8 mcp/server.go, toolForErr).
const sdkArgumentsErrorPrefix = `validating "arguments": `

// toolOutcome maps a tool error to a fixed outcome value.
func toolOutcome(err error) string {
	switch {
	case err == nil:
		return outcomeOK
	case errors.Is(err, ErrInvalidInput), strings.HasPrefix(err.Error(), sdkArgumentsErrorPrefix):
		return outcomeInvalid
	case errors.Is(err, ErrPatternNotFound):
		return outcomeNotFound
	default:
		return outcomeUnavailable
	}
}

// withTrace adds the active trace and span IDs to logger, so tool logs can be
// joined to their trace.
func withTrace(ctx context.Context, logger zerolog.Logger) *zerolog.Logger {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		logger = logger.With().Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String()).Logger()
	}
	return &logger
}
