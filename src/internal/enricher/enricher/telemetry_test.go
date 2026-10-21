package enricher_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

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
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/mnemonic/internal/enricher/enricher"
	"github.com/twistingmercury/mnemonic/internal/enricher/metrics"
	queue "github.com/twistingmercury/mnemonic/internal/enricher/queue"
	enrichmentjob "github.com/twistingmercury/mnemonic/internal/enricher/repository/enrichmentjob"
	enrichmentsvc "github.com/twistingmercury/mnemonic/internal/enricher/service/enrichment"
)

// runOne runs a worker over a single delivery whose ProcessJob returns
// processErr, and returns the collected job metrics.
func runOne(t *testing.T, d queue.Delivery, processErr error) metricdata.ResourceMetrics {
	t.Helper()
	job := newTestJob()
	d.Body = []byte(fmt.Sprintf(`{"job_id":%q}`, job.ID))

	svc := &fakeService{
		startJob: startsJobs(job),
		process:  func(context.Context, *enrichmentjob.Job) error { return processErr },
	}

	reader := sdkmetric.NewManualReader()
	jm, err := metrics.NewJobs(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	sub := &mockSubscriber{name: "test-queue", deliveries: []queue.Delivery{d}}
	w := enricher.New(svc, sub, testConfig(1), zerolog.Nop(), jm)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	require.NoError(t, w.Run(ctx))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

// outcomes returns the mnemonic.enrichment.jobs count per outcome.
func outcomes(rm metricdata.ResourceMetrics) map[string]int64 {
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "mnemonic.enrichment.jobs" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				v, _ := dp.Attributes.Value("outcome")
				got[v.AsString()] = dp.Value
			}
		}
	}
	return got
}

func TestWorkerRecordsJobOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		processErr error
		outcome    string
		acked      bool
	}{
		{"completed", nil, metrics.OutcomeCompleted, true},
		{"recorded failure is acked", fmt.Errorf("%w: embed: boom", enrichmentsvc.ErrJobFailed), metrics.OutcomeFailed, true},
		{"unrecorded failure is nacked", fmt.Errorf("mark job failed: db down"), metrics.OutcomeError, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var ack, nack atomic.Bool
			d := makeDelivery([16]byte{}, &ack, &nack)

			rm := runOne(t, d, tt.processErr)

			assert.Equal(t, map[string]int64{tt.outcome: 1}, outcomes(rm))
			assert.Equal(t, tt.acked, ack.Load(), "ack")
			assert.Equal(t, !tt.acked, nack.Load(), "nack")
		})
	}
}

// TestWorkerContinuesProducerTrace swaps the global tracer provider and
// propagator, so it must not run in parallel.
func TestWorkerContinuesProducerTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	parentID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")

	var ack, nack atomic.Bool
	d := makeDelivery([16]byte{}, &ack, &nack)
	d.Headers = map[string]string{"traceparent": "00-" + traceID.String() + "-" + parentID.String() + "-01"}

	runOne(t, d, fmt.Errorf("%w: extract: boom", enrichmentsvc.ErrJobFailed))

	var span sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == "process test-queue" {
			span = s
		}
	}
	require.NotNil(t, span, "consumer span is recorded")
	assert.Equal(t, trace.SpanKindConsumer, span.SpanKind())
	assert.Equal(t, traceID, span.SpanContext().TraceID(), "continues the producer's trace")
	assert.Equal(t, parentID, span.Parent().SpanID(), "is a child of the publish span")
	assert.Equal(t, codes.Error, span.Status().Code, "a recorded failure marks the span")
}
