package enricher_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/twistingmercury/librarian-enricher/internal/config"
	"github.com/twistingmercury/librarian-enricher/internal/enricher"
	"github.com/twistingmercury/librarian-enricher/internal/metrics"
	queue "github.com/twistingmercury/librarian-enricher/internal/queue"
	enrichmentjob "github.com/twistingmercury/librarian-enricher/internal/repository/enrichmentjob"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeService implements enrichmentsvc.Service with function fields; a nil
// field returns zero values. The maintenance methods are no-ops: maintenance
// runs on a 5-minute ticker, longer than any test.
type fakeService struct {
	startJob func(ctx context.Context, id uuid.UUID) (*enrichmentjob.Job, error)
	claimDue func(ctx context.Context, limit int) ([]*enrichmentjob.Job, error)
	process  func(ctx context.Context, job *enrichmentjob.Job) error
}

func (s *fakeService) ClaimDueJobs(ctx context.Context, limit int) ([]*enrichmentjob.Job, error) {
	if s.claimDue != nil {
		return s.claimDue(ctx, limit)
	}
	return nil, nil
}

func (s *fakeService) StartJob(ctx context.Context, id uuid.UUID) (*enrichmentjob.Job, error) {
	if s.startJob != nil {
		return s.startJob(ctx, id)
	}
	return nil, nil
}

func (s *fakeService) ProcessJob(ctx context.Context, job *enrichmentjob.Job) error {
	if s.process != nil {
		return s.process(ctx, job)
	}
	return nil
}

func (s *fakeService) ReclaimStaleJobs(context.Context) (int64, error)     { return 0, nil }
func (s *fakeService) CleanupCompletedJobs(context.Context) (int64, error) { return 0, nil }
func (s *fakeService) CleanupFailedJobs(context.Context) (int64, error)    { return 0, nil }

// startsJobs returns a StartJob func that starts whichever of jobs has the
// requested ID.
func startsJobs(jobs ...*enrichmentjob.Job) func(context.Context, uuid.UUID) (*enrichmentjob.Job, error) {
	return func(_ context.Context, id uuid.UUID) (*enrichmentjob.Job, error) {
		for _, j := range jobs {
			if j.ID == id {
				return j, nil
			}
		}
		return nil, errors.New("job not found")
	}
}

// mockSubscriber implements queue.Subscriber for testing. It emits the
// configured deliveries, then blocks until ctx is done and closes the channel.
type mockSubscriber struct {
	deliveries []queue.Delivery
	name       string
}

func (s *mockSubscriber) Subscribe(ctx context.Context) <-chan queue.Delivery {
	out := make(chan queue.Delivery)
	go func() {
		defer close(out)
		for _, d := range s.deliveries {
			select {
			case out <- d:
			case <-ctx.Done():
				return
			}
		}
		<-ctx.Done()
	}()
	return out
}

func (s *mockSubscriber) Name() string { return s.name }

func (s *mockSubscriber) Close() error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testConfig returns an EnrichmentConfig with fast intervals for testing.
func testConfig(workerCount int) config.EnrichmentConfig {
	return config.EnrichmentConfig{
		WorkerCount:            workerCount,
		RetryDelay:             10 * time.Millisecond,
		JobTimeout:             1 * time.Second,
		DrainTimeout:           500 * time.Millisecond,
		CompletedRetention:     1 * time.Hour,
		FailedRetention:        1 * time.Hour,
		RelatedToMinSimilarity: 0.3,
	}
}

// noopJobMetrics records job metrics nowhere.
var noopJobMetrics, _ = metrics.NewJobs(noop.NewMeterProvider().Meter("test"))

// newTestJob creates a Job with a random ID and pattern ID.
func newTestJob() *enrichmentjob.Job {
	pid := uuid.New()
	return &enrichmentjob.Job{ID: uuid.New(), PatternID: &pid}
}

// newChunkOnlyJob creates a Job with ChunkID set and PatternID nil.
func newChunkOnlyJob() *enrichmentjob.Job {
	cid := uuid.New()
	return &enrichmentjob.Job{ID: uuid.New(), ChunkID: &cid}
}

// newBothIDsJob creates a Job with both PatternID and ChunkID set.
func newBothIDsJob() *enrichmentjob.Job {
	pid, cid := uuid.New(), uuid.New()
	return &enrichmentjob.Job{ID: uuid.New(), PatternID: &pid, ChunkID: &cid}
}

// makeDelivery builds a queue.Delivery whose Body encodes the given job ID and
// whose Ack/Nack callbacks record whether they were called.
func makeDelivery(jobID uuid.UUID, ackCalled, nackCalled *atomic.Bool) queue.Delivery {
	body, _ := json.Marshal(map[string]string{"job_id": jobID.String()})
	return queue.Delivery{
		Body: body,
		Ack: func() error {
			ackCalled.Store(true)
			return nil
		},
		Nack: func(_ bool) error {
			nackCalled.Store(true)
			return nil
		},
	}
}

// runFor runs w until timeout and requires a clean return.
func runFor(t *testing.T, w *enricher.Worker, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	require.NoError(t, w.Run(ctx))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestWorkerProcessesAvailableJobs runs one job of each ID shape (the worker
// logs and traces PatternID and ChunkID only when set).
func TestWorkerProcessesAvailableJobs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		job  *enrichmentjob.Job
	}{
		{"pattern only", newTestJob()},
		{"chunk only", newChunkOnlyJob()},
		{"both IDs", newBothIDsJob()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var ackCalled, nackCalled, processed atomic.Bool
			svc := &fakeService{
				startJob: startsJobs(tt.job),
				process: func(_ context.Context, j *enrichmentjob.Job) error {
					processed.Store(j == tt.job)
					return nil
				},
			}
			sub := &mockSubscriber{
				name:       "test-queue",
				deliveries: []queue.Delivery{makeDelivery(tt.job.ID, &ackCalled, &nackCalled)},
			}

			runFor(t, enricher.New(svc, sub, testConfig(1), zerolog.Nop(), noopJobMetrics), 500*time.Millisecond)

			assert.True(t, processed.Load(), "the started job should be processed")
			assert.True(t, ackCalled.Load(), "delivery should be acked after successful processing")
			assert.False(t, nackCalled.Load(), "delivery should not be nacked on success")
		})
	}
}

func TestWorkerNacksOnStartJobFailure(t *testing.T) {
	t.Parallel()

	job := newTestJob()
	var ackCalled, nackCalled, processed atomic.Bool

	svc := &fakeService{
		startJob: func(context.Context, uuid.UUID) (*enrichmentjob.Job, error) { return nil, errors.New("db error") },
		process:  func(context.Context, *enrichmentjob.Job) error { processed.Store(true); return nil },
	}
	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	runFor(t, enricher.New(svc, sub, testConfig(1), zerolog.Nop(), noopJobMetrics), 500*time.Millisecond)

	assert.False(t, ackCalled.Load(), "delivery should not be acked when StartJob fails")
	assert.True(t, nackCalled.Load(), "delivery should be nacked when StartJob fails")
	assert.False(t, processed.Load())
}

func TestWorkerAcksUnavailableJob(t *testing.T) {
	t.Parallel()

	job := newTestJob()
	var ackCalled, nackCalled, processed atomic.Bool

	svc := &fakeService{
		startJob: func(context.Context, uuid.UUID) (*enrichmentjob.Job, error) {
			return nil, enrichmentjob.ErrUnavailable
		},
		process: func(context.Context, *enrichmentjob.Job) error { processed.Store(true); return nil },
	}
	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	runFor(t, enricher.New(svc, sub, testConfig(1), zerolog.Nop(), noopJobMetrics), 500*time.Millisecond)

	assert.True(t, ackCalled.Load(), "a job that is not available to start should be acked")
	assert.False(t, nackCalled.Load())
	assert.False(t, processed.Load())
}

func TestWorkerRunsJobsDueForRetry(t *testing.T) {
	t.Parallel()

	job := newChunkOnlyJob()
	var claims atomic.Int32
	processed := make(chan *enrichmentjob.Job, 1)

	svc := &fakeService{
		claimDue: func(_ context.Context, limit int) ([]*enrichmentjob.Job, error) {
			if claims.Add(1) == 1 {
				assert.Equal(t, 2, limit, "claims only as many jobs as there are free workers")
				return []*enrichmentjob.Job{job}, nil
			}
			return nil, nil
		},
		process: func(_ context.Context, j *enrichmentjob.Job) error {
			processed <- j
			return nil
		},
	}
	sub := &mockSubscriber{name: "test-queue"}

	w := enricher.New(svc, sub, testConfig(2), zerolog.Nop(), noopJobMetrics)
	enricher.SetRetryInterval(w, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case got := <-processed:
		assert.Equal(t, job, got)
	case <-time.After(2 * time.Second):
		t.Fatal("job due for retry was not processed")
	}
	cancel()
	require.NoError(t, <-done)
}

func TestWorkerNacksOnProcessJobFailure(t *testing.T) {
	t.Parallel()

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := &fakeService{
		startJob: startsJobs(job),
		process:  func(context.Context, *enrichmentjob.Job) error { return errors.New("pipeline error") },
	}
	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	runFor(t, enricher.New(svc, sub, testConfig(1), zerolog.Nop(), noopJobMetrics), 500*time.Millisecond)

	assert.False(t, ackCalled.Load(), "delivery should not be acked when ProcessJob returns error")
	assert.True(t, nackCalled.Load(), "delivery should be nacked when ProcessJob returns unrecoverable error")
}

func TestWorkerNacksOnInvalidDeliveryBody(t *testing.T) {
	t.Parallel()

	var nackCalled atomic.Bool

	sub := &mockSubscriber{
		name: "test-queue",
		deliveries: []queue.Delivery{
			{
				Body: []byte("not json"),
				Ack:  func() error { return nil },
				Nack: func(_ bool) error { nackCalled.Store(true); return nil },
			},
		},
	}

	runFor(t, enricher.New(&fakeService{}, sub, testConfig(1), zerolog.Nop(), noopJobMetrics), 500*time.Millisecond)

	assert.True(t, nackCalled.Load(), "delivery with invalid body should be nacked")
}

func TestWorkerGracefulShutdown(t *testing.T) {
	t.Parallel()

	// No deliveries; worker will block until ctx is cancelled.
	sub := &mockSubscriber{name: "test-queue"}

	w := enricher.New(&fakeService{}, sub, testConfig(2), zerolog.Nop(), noopJobMetrics)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Give the worker time to start, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "Run should return nil on graceful shutdown")
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not shut down within timeout")
	}
}

func TestMultipleWorkersConcurrency(t *testing.T) {
	t.Parallel()

	jobs := make([]*enrichmentjob.Job, 4)
	deliveries := make([]queue.Delivery, 4)
	for i := range jobs {
		jobs[i] = newTestJob()
		var ack, nack atomic.Bool
		deliveries[i] = makeDelivery(jobs[i].ID, &ack, &nack)
	}

	var processedCount atomic.Int32

	svc := &fakeService{
		startJob: startsJobs(jobs...),
		process: func(_ context.Context, _ *enrichmentjob.Job) error {
			processedCount.Add(1)
			return nil
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: deliveries,
	}

	w := enricher.New(svc, sub, testConfig(2), zerolog.Nop(), noopJobMetrics)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int32(4), processedCount.Load(), "all jobs should be processed")
}

// --- Graceful drain tests ---

func TestGracefulDrainWaitsForInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that when the context is cancelled while a job is
	// being processed, Run waits for the in-flight job to complete before
	// returning.

	var (
		processStarted = make(chan struct{})
		processDone    atomic.Bool
	)

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := &fakeService{
		startJob: startsJobs(job),
		process: func(ctx context.Context, _ *enrichmentjob.Job) error {
			close(processStarted)
			// Simulate a long-running job (100ms).
			select {
			case <-time.After(100 * time.Millisecond):
				processDone.Store(true)
				return nil
			case <-ctx.Done():
				// If the drain context is cancelled, the job was not given
				// enough time.
				return ctx.Err()
			}
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	cfg := testConfig(1)
	cfg.DrainTimeout = 2 * time.Second // Generous drain timeout.

	w := enricher.New(svc, sub, cfg, zerolog.Nop(), noopJobMetrics)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Wait for the job to start processing, then cancel.
	<-processStarted
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
		assert.True(t, processDone.Load(), "in-flight job should have completed before Run returned")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within expected time")
	}
}

func TestGracefulDrainTimeoutCancelsInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that if in-flight jobs take longer than the drain
	// timeout, their context is cancelled and Run returns.

	var (
		processStarted   = make(chan struct{}, 1)
		contextCancelled atomic.Bool
	)

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := &fakeService{
		startJob: startsJobs(job),
		process: func(ctx context.Context, _ *enrichmentjob.Job) error {
			select {
			case processStarted <- struct{}{}:
			default:
			}
			// Simulate a very long job that exceeds the drain timeout.
			select {
			case <-time.After(10 * time.Second):
				return nil
			case <-ctx.Done():
				contextCancelled.Store(true)
				return ctx.Err()
			}
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	cfg := testConfig(1)
	cfg.DrainTimeout = 50 * time.Millisecond // Short drain timeout.

	w := enricher.New(svc, sub, cfg, zerolog.Nop(), noopJobMetrics)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Wait for the job to start processing, then cancel.
	<-processStarted
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
		assert.True(t, contextCancelled.Load(), "drain timeout should have cancelled the in-flight job's context")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within expected time")
	}
}

func TestGracefulDrainMultipleInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that the drain phase waits for multiple in-flight
	// jobs across multiple workers.

	const workerCount = 3

	var (
		allStarted = make(chan struct{})
		started    atomic.Int32
		completed  atomic.Int32
	)

	jobs := make([]*enrichmentjob.Job, workerCount)
	deliveries := make([]queue.Delivery, workerCount)
	for i := range jobs {
		jobs[i] = newTestJob()
		var ack, nack atomic.Bool
		deliveries[i] = makeDelivery(jobs[i].ID, &ack, &nack)
	}

	svc := &fakeService{
		startJob: startsJobs(jobs...),
		process: func(ctx context.Context, _ *enrichmentjob.Job) error {
			count := started.Add(1)
			if int(count) == workerCount {
				close(allStarted)
			}
			// Simulate work that takes 100ms.
			select {
			case <-time.After(100 * time.Millisecond):
				completed.Add(1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: deliveries,
	}

	cfg := testConfig(workerCount)
	cfg.DrainTimeout = 2 * time.Second

	w := enricher.New(svc, sub, cfg, zerolog.Nop(), noopJobMetrics)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Wait for all workers to start processing, then cancel.
	<-allStarted
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
		require.Equal(t, int32(workerCount), completed.Load(),
			"all in-flight jobs should complete during drain")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within expected time")
	}
}
