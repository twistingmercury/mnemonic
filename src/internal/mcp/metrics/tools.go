package metrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Tools holds instruments for MCP tool calls.
type Tools struct {
	calls    metric.Int64Counter
	duration metric.Float64Histogram
}

// NewTools creates MCP tool metric instruments using the provided meter.
func NewTools(meter metric.Meter) (*Tools, error) {
	calls, err := meter.Int64Counter(
		"mnemonic.mcp.tool_calls",
		metric.WithDescription("Number of MCP tool calls, by tool and outcome"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		return nil, fmt.Errorf("tool calls counter: %w", err)
	}

	duration, err := meter.Float64Histogram(
		"mnemonic.mcp.tool_duration",
		metric.WithDescription("MCP tool call latency in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000),
	)
	if err != nil {
		return nil, fmt.Errorf("tool duration histogram: %w", err)
	}

	return &Tools{calls: calls, duration: duration}, nil
}

// Record records one tool call. tool must be a registered tool name and
// outcome a fixed value, so both attributes stay low-cardinality.
func (m *Tools) Record(ctx context.Context, tool, outcome string, duration time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("tool", tool),
		attribute.String("outcome", outcome),
	)
	m.calls.Add(ctx, 1, attrs)
	m.duration.Record(ctx, milliseconds(duration), attrs)
}
