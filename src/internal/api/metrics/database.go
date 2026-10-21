package metrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Database holds instruments for database-related metrics.
// It tracks query latency and errors for Neo4j; PostgreSQL queries and pool
// statistics are measured by otelpgx instead.
type Database struct {
	queryLatency metric.Float64Histogram
	queryErrors  metric.Int64Counter
}

// NewDatabase creates database metric instruments using the provided meter.
func NewDatabase(meter metric.Meter) (*Database, error) {
	queryLatency, err := meter.Float64Histogram(
		"mnemonic.db.query_latency",
		metric.WithDescription("Database query latency in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
	)
	if err != nil {
		return nil, fmt.Errorf("query latency histogram: %w", err)
	}

	queryErrors, err := meter.Int64Counter(
		"mnemonic.db.query_errors",
		metric.WithDescription("Number of database query errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("query errors counter: %w", err)
	}

	return &Database{
		queryLatency: queryLatency,
		queryErrors:  queryErrors,
	}, nil
}

// Record records the latency of a database operation and, when err is
// non-nil, counts it as an error. Both database and operation should be
// predefined values with bounded cardinality; do not use user-provided or
// dynamic values, to avoid metric explosion.
func (m *Database) Record(ctx context.Context, database, operation string, duration time.Duration, err error) {
	attrs := metric.WithAttributes(
		attribute.String("database", database),
		attribute.String("operation", operation),
	)
	m.queryLatency.Record(ctx, milliseconds(duration), attrs)
	if err != nil {
		m.queryErrors.Add(ctx, 1, attrs)
	}
}

// milliseconds converts d to fractional milliseconds, so sub-millisecond
// calls land in the lowest bucket instead of being truncated to zero.
func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
