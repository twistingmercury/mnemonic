package enricher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/mnemonic/internal/enricher/config"
	"github.com/twistingmercury/mnemonic/internal/enricher/metrics"
	queue "github.com/twistingmercury/mnemonic/internal/enricher/queue"
	"github.com/twistingmercury/mnemonic/internal/enricher/repository/enrichmentjob"
	enrichmentsvc "github.com/twistingmercury/mnemonic/internal/enricher/service/enrichment"
)

// defaultMaintenanceInterval is the interval between maintenance loop iterations.
// Maintenance reclaims stale jobs and cleans up old completed/failed jobs.
const defaultMaintenanceInterval = 5 * time.Minute

// defaultRetryInterval is how often the worker claims jobs due for a retry.
// It is shorter than the default retry delay so retries run close to when
// they are scheduled.
const defaultRetryInterval = 15 * time.Second

var tracer = otel.Tracer("mnemonic/enricher")

// jobMessage is the JSON payload delivered by the queue for each enrichment job.
type jobMessage struct {
	JobID uuid.UUID `json:"job_id"`
}

// Worker consumes enrichment job messages from a queue.Subscriber and processes
// them using the EnrichmentService. A configurable semaphore bounds concurrent
// job goroutines, and a single maintenance goroutine handles periodic cleanup.
type Worker struct {
	svc        enrichmentsvc.Service
	sub        queue.Subscriber
	cfg        config.EnrichmentConfig
	logger     zerolog.Logger
	jobMetrics *metrics.Jobs

	// retryInterval is how often Run claims jobs due for a retry.
	retryInterval time.Duration
}

// New creates a Worker that processes enrichment jobs using svc, consuming
// messages from sub, configured by cfg. The logger is used for structured
// logging of worker lifecycle events and errors. jobMetrics records the
// count and duration of every handled delivery.
func New(svc enrichmentsvc.Service, sub queue.Subscriber, cfg config.EnrichmentConfig, logger zerolog.Logger, jobMetrics *metrics.Jobs) *Worker {
	return &Worker{
		svc:        svc,
		sub:        sub,
		cfg:        cfg,
		logger:     logger.With().Str("component", "enrichment_worker").Logger(),
		jobMetrics: jobMetrics,

		retryInterval: defaultRetryInterval,
	}
}

// Run subscribes to the queue and fans out deliveries to bounded goroutines.
// Every retryInterval it also claims jobs due for a retry, since a failed
// job's message is already gone from the queue. It blocks until ctx is
// cancelled.
//
// On shutdown (ctx cancellation), Run performs a two-phase graceful drain:
//  1. Stop consuming new deliveries (the subscriber stops emitting when ctx is cancelled).
//  2. Wait for in-flight handleDelivery calls to complete, up to cfg.DrainTimeout.
//
// If the drain timeout expires, in-flight job contexts are cancelled and Run
// returns. Run always returns nil on shutdown; worker goroutines never crash
// the server.
func (w *Worker) Run(ctx context.Context) error {
	// drainCtx is intentionally derived from Background, not ctx — it must
	// remain live after ctx is cancelled to give in-flight jobs time to
	// complete before shutdown.
	drainCtx, drainCancel := context.WithCancel(context.Background())
	defer drainCancel()

	// inflight tracks the currently executing job handlers so Run can wait
	// for them during the drain phase.
	var inflight sync.WaitGroup

	// semaphore limits the number of concurrently executing job handlers.
	semaphore := make(chan struct{}, w.cfg.WorkerCount)

	deliveries := w.sub.Subscribe(ctx)

	var maintenance sync.WaitGroup
	maintenance.Go(func() { w.runMaintenance(ctx) })

	w.logger.Info().
		Int("worker_count", w.cfg.WorkerCount).
		Dur("drain_timeout", w.cfg.DrainTimeout).
		Str("queue", w.sub.Name()).
		Msg("enrichment worker started")

	// dispatch runs work in a goroutine once a semaphore slot is free. Work
	// gets drainCtx so it can finish after ctx is cancelled.
	dispatch := func(work func(context.Context)) {
		semaphore <- struct{}{}
		inflight.Go(func() {
			defer func() { <-semaphore }()
			work(drainCtx)
		})
	}

	sweep := time.NewTicker(w.retryInterval)
	defer sweep.Stop()

	// Fan out deliveries and due retries. The loop exits when the deliveries
	// channel is closed, which the subscriber does when ctx is cancelled.
loop:
	for {
		select {
		case d, ok := <-deliveries:
			if !ok {
				break loop
			}
			dispatch(func(ctx context.Context) { w.handleDelivery(ctx, d) })
		case <-sweep.C:
			free := cap(semaphore) - len(semaphore)
			if ctx.Err() != nil || free == 0 {
				continue
			}
			jobs, err := w.svc.ClaimDueJobs(ctx, free)
			if err != nil {
				w.logger.Error().Err(err).Msg("failed to claim jobs due for retry")
				continue
			}
			for _, job := range jobs {
				dispatch(func(ctx context.Context) { w.handleRetry(ctx, job) })
			}
		}
	}

	// Drain phase: wait for in-flight jobs to complete. If they do not finish
	// within the drain timeout, drainCancel cancels their context and Run
	// waits for them to return.
	w.logger.Info().
		Dur("drain_timeout", w.cfg.DrainTimeout).
		Msg("draining in-flight jobs")

	timeout := time.AfterFunc(w.cfg.DrainTimeout, func() {
		w.logger.Warn().
			Dur("drain_timeout", w.cfg.DrainTimeout).
			Msg("drain timeout expired, cancelling in-flight jobs")
		drainCancel()
	})
	inflight.Wait()
	if timeout.Stop() {
		w.logger.Info().Msg("all in-flight jobs drained successfully")
	}

	if err := w.sub.Close(); err != nil {
		w.logger.Error().Err(err).Msg("error closing subscriber")
	}

	maintenance.Wait()

	w.logger.Info().Msg("enrichment worker stopped")
	return nil
}

// handleDelivery unmarshals a queue delivery, starts the job, and runs the
// enrichment pipeline. It calls d.Ack when the pipeline ran (successfully or
// with a recorded failure) or the job is not available to start (already
// started, finished, or waiting for a retry), and d.Nack(false) on
// unrecoverable errors (parse failures, start failures, unrecorded pipeline
// failures). drainCtx remains live through the drain phase so that in-flight
// work can complete gracefully after the main ctx is cancelled.
//
// Each delivery gets a consumer span that continues the producer's trace from
// the message headers, and one record in the job metrics.
func (w *Worker) handleDelivery(drainCtx context.Context, d queue.Delivery) {
	start := time.Now()
	ctx := otel.GetTextMapPropagator().Extract(drainCtx, propagation.MapCarrier(d.Headers))
	ctx, span := tracer.Start(ctx, "process "+w.sub.Name(),
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.operation.type", "process"),
			attribute.String("messaging.destination.name", w.sub.Name()),
		),
	)

	outcome := metrics.OutcomeRejected
	var spanErr error
	defer func() { w.finish(ctx, span, spanErr, outcome, start) }()

	log := w.spanLogger(span)
	nack := func(msg string) {
		log.Error().Err(spanErr).Msg(msg + "; nacking")
		if nackErr := d.Nack(false); nackErr != nil {
			log.Error().Err(nackErr).Msg("nack failed")
		}
	}

	// Parse the job message.
	var msg jobMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		spanErr = err
		nack("failed to parse delivery body")
		return
	}

	jobID := msg.JobID
	span.SetAttributes(attribute.String("mnemonic.job_id", jobID.String()))
	log = log.With().Str("job_id", jobID.String()).Logger()

	// Mark the job as processing and load what it targets.
	job, err := w.svc.StartJob(ctx, jobID)
	switch {
	case errors.Is(err, enrichmentjob.ErrUnavailable):
		// A redelivered message, a job the retry sweep already took, or one
		// waiting out its retry delay: the sweep owns it now.
		log.Info().Msg("job not available to start; acking")
		if ackErr := d.Ack(); ackErr != nil {
			log.Error().Err(ackErr).Msg("ack failed")
		}
		return
	case err != nil:
		spanErr = err
		nack("failed to start job")
		return
	}

	outcome, spanErr = w.runJob(ctx, span, log, job)
	if outcome == metrics.OutcomeError {
		// The failure could not be recorded (unrecoverable). Nack without
		// requeue so we don't loop on a job stuck in an unrecoverable state.
		nack("enrichment job failed with unrecoverable error")
		return
	}

	if ackErr := d.Ack(); ackErr != nil {
		log.Error().Err(ackErr).Msg("ack failed after processing")
	}
}

// handleRetry runs the pipeline for a job the retry sweep claimed. It gets its
// own span and one record in the job metrics, like a delivery.
func (w *Worker) handleRetry(drainCtx context.Context, job *enrichmentjob.Job) {
	start := time.Now()
	ctx, span := tracer.Start(drainCtx, "retry enrichment job",
		trace.WithAttributes(attribute.String("mnemonic.job_id", job.ID.String())),
	)
	log := w.spanLogger(span).With().Str("job_id", job.ID.String()).Bool("retry", true).Logger()

	outcome, spanErr := w.runJob(ctx, span, log, job)
	if outcome == metrics.OutcomeError {
		// Left in processing; ReclaimStale returns it to pending after the
		// job timeout.
		log.Error().Err(spanErr).Msg("enrichment job retry failed with unrecoverable error")
	}
	w.finish(ctx, span, spanErr, outcome, start)
}

// runJob runs the enrichment pipeline for a started job and returns its
// outcome and the error to record on the span, if any.
func (w *Worker) runJob(ctx context.Context, span trace.Span, log zerolog.Logger, job *enrichmentjob.Job) (string, error) {
	logEvent := log.Info()
	if job.PatternID != nil {
		span.SetAttributes(attribute.String("mnemonic.pattern_id", job.PatternID.String()))
		logEvent = logEvent.Str("pattern_id", job.PatternID.String())
	}
	if job.ChunkID != nil {
		span.SetAttributes(attribute.String("mnemonic.chunk_id", job.ChunkID.String()))
		logEvent = logEvent.Str("chunk_id", job.ChunkID.String())
	}
	logEvent.Msg("processing enrichment job")

	err := w.svc.ProcessJob(ctx, job)
	switch {
	case err == nil:
		log.Info().Msg("enrichment job completed")
		return metrics.OutcomeCompleted, nil
	case errors.Is(err, enrichmentsvc.ErrJobFailed):
		// The failure is recorded on the job, which the sweep retries.
		log.Warn().Err(err).Msg("enrichment job failed; failure recorded")
		return metrics.OutcomeFailed, err
	default:
		return metrics.OutcomeError, err
	}
}

// spanLogger returns the worker logger tagged with span's trace and span IDs.
func (w *Worker) spanLogger(span trace.Span) zerolog.Logger {
	if sc := span.SpanContext(); sc.IsValid() {
		return w.logger.With().Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String()).Logger()
	}
	return w.logger
}

// finish ends span, marking it failed when err is non-nil, and records the
// job's outcome and duration.
func (w *Worker) finish(ctx context.Context, span trace.Span, err error, outcome string, start time.Time) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
	w.jobMetrics.Record(ctx, outcome, time.Since(start))
}

// runMaintenance periodically reclaims stale jobs and cleans up old
// completed/failed jobs. It runs until ctx is cancelled.
func (w *Worker) runMaintenance(ctx context.Context) {
	log := w.logger.With().Str("loop", "maintenance").Logger()
	log.Debug().Msg("maintenance goroutine started")

	ticker := time.NewTicker(defaultMaintenanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("maintenance goroutine stopping")
			return
		case <-ticker.C:
			w.doMaintenance(ctx, log)
		}
	}
}

// doMaintenance performs a single maintenance cycle: reclaim stale jobs,
// cleanup completed jobs, and cleanup failed jobs.
func (w *Worker) doMaintenance(ctx context.Context, log zerolog.Logger) {
	if ctx.Err() != nil {
		return
	}

	reclaimed, err := w.svc.ReclaimStaleJobs(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to reclaim stale jobs")
	} else if reclaimed > 0 {
		log.Info().Int64("count", reclaimed).Msg("reclaimed stale jobs")
	}

	completedCleaned, err := w.svc.CleanupCompletedJobs(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to cleanup completed jobs")
	} else if completedCleaned > 0 {
		log.Info().Int64("count", completedCleaned).Msg("cleaned up completed jobs")
	}

	failedCleaned, err := w.svc.CleanupFailedJobs(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to cleanup failed jobs")
	} else if failedCleaned > 0 {
		log.Info().Int64("count", failedCleaned).Msg("cleaned up failed jobs")
	}
}
