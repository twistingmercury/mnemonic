package metrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Pattern holds instruments for pattern retrieval metrics.
// It tracks query latency and the number of patterns returned per query.
type Pattern struct {
	queryLatency     metric.Float64Histogram
	patternsReturned metric.Int64Histogram
}

// NewPattern creates pattern metric instruments using the provided meter.
func NewPattern(meter metric.Meter) (*Pattern, error) {
	queryLatency, err := meter.Float64Histogram(
		"mnemonic.patterns.query_latency",
		metric.WithDescription("Pattern query latency in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500),
	)
	if err != nil {
		return nil, fmt.Errorf("query latency histogram: %w", err)
	}

	patternsReturned, err := meter.Int64Histogram(
		"mnemonic.patterns.returned",
		metric.WithDescription("Number of patterns returned per query"),
		metric.WithUnit("{pattern}"),
		metric.WithExplicitBucketBoundaries(0, 1, 5, 10, 25, 50, 100),
	)
	if err != nil {
		return nil, fmt.Errorf("patterns returned histogram: %w", err)
	}

	return &Pattern{
		queryLatency:     queryLatency,
		patternsReturned: patternsReturned,
	}, nil
}

// RecordQuery records a pattern query with its latency and result count.
// The database parameter identifies the data source (e.g., "postgres", "pgvector", "neo4j").
// The database should be a predefined value with bounded cardinality.
// Do not use user-provided or dynamic values to avoid metric explosion.
func (m *Pattern) RecordQuery(ctx context.Context, database string, duration time.Duration, count int) {
	attrs := metric.WithAttributes(attribute.String("database", database))
	m.queryLatency.Record(ctx, milliseconds(duration), attrs)
	m.patternsReturned.Record(ctx, int64(count), attrs)
}
