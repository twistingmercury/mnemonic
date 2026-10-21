package metrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Job outcomes recorded on the enrichment job metrics.
const (
	OutcomeCompleted = "completed" // pipeline succeeded
	OutcomeFailed    = "failed"    // a step failed and the job was marked for retry
	OutcomeError     = "error"     // a step failed and the failure could not be recorded
	OutcomeRejected  = "rejected"  // message unreadable, job missing, or not claimable
)

// Jobs holds instruments for enrichment job processing.
type Jobs struct {
	processed metric.Int64Counter
	duration  metric.Float64Histogram
}

// NewJobs creates enrichment job metric instruments using the provided meter.
func NewJobs(meter metric.Meter) (*Jobs, error) {
	processed, err := meter.Int64Counter(
		"mnemonic.enrichment.jobs",
		metric.WithDescription("Number of enrichment job deliveries handled, by outcome"),
		metric.WithUnit("{job}"),
	)
	if err != nil {
		return nil, fmt.Errorf("jobs counter: %w", err)
	}

	duration, err := meter.Float64Histogram(
		"mnemonic.enrichment.job_duration",
		metric.WithDescription("Time to handle one enrichment job delivery in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000),
	)
	if err != nil {
		return nil, fmt.Errorf("job duration histogram: %w", err)
	}

	return &Jobs{processed: processed, duration: duration}, nil
}

// Record records one handled delivery. outcome must be one of the Outcome
// constants so the attribute stays low-cardinality.
func (m *Jobs) Record(ctx context.Context, outcome string, duration time.Duration) {
	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	m.processed.Add(ctx, 1, attrs)
	m.duration.Record(ctx, milliseconds(duration), attrs)
}
