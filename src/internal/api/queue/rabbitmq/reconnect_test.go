package rabbitmq_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/twistingmercury/mnemonic/internal/api/config"
	"github.com/twistingmercury/mnemonic/internal/api/queue/rabbitmq"
)

// fakeChannel fails the first failFirst publishes, then succeeds.
type fakeChannel struct {
	failFirst int32
	calls     atomic.Int32
}

func (c *fakeChannel) PublishWithContext(context.Context, string, string, bool, bool, amqp.Publishing) error {
	if c.calls.Add(1) <= c.failFirst {
		return errors.New("channel closed")
	}
	return nil
}

func (c *fakeChannel) Close() error { return nil }

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func failuresCounter(t *testing.T) metric.Int64Counter {
	t.Helper()
	c, err := noop.NewMeterProvider().Meter("test").Int64Counter("x")
	require.NoError(t, err)
	return c
}

// A publisher waiting out ReconnectDelay must not hold the lock: other
// requests keep publishing on the channel meanwhile.
func TestPublish_ReconnectDelayDoesNotBlockOtherPublishers(t *testing.T) {
	t.Parallel()

	const delay = 400 * time.Millisecond
	ch := &fakeChannel{failFirst: 1}
	var dials atomic.Int32
	p := rabbitmq.NewTestPublisher(
		config.RabbitMQConfig{Queue: "q", ReconnectDelay: delay},
		failuresCounter(t),
		ch,
		func() (io.Closer, rabbitmq.Channel, error) {
			dials.Add(1)
			return nopCloser{}, &fakeChannel{}, nil
		},
	)

	slowDone := make(chan error, 1)
	go func() { slowDone <- p.Publish(context.Background(), uuid.New()) }()

	// Wait until the first publish has failed and is sleeping.
	require.Eventually(t, func() bool { return ch.calls.Load() >= 1 }, time.Second, time.Millisecond)

	start := time.Now()
	require.NoError(t, p.Publish(context.Background(), uuid.New()))
	assert.Less(t, time.Since(start), delay/2, "second publish was stalled behind the reconnect delay")

	require.NoError(t, <-slowDone)
	assert.Equal(t, int32(1), dials.Load())
}

// When several publishers fail on the same channel, only the first to wake
// reconnects; the rest reuse its new channel instead of tearing it down.
func TestPublish_ConcurrentFailuresReconnectOnce(t *testing.T) {
	t.Parallel()

	broken := &fakeChannel{failFirst: 1 << 30}
	var dials atomic.Int32
	p := rabbitmq.NewTestPublisher(
		config.RabbitMQConfig{Queue: "q", ReconnectDelay: 20 * time.Millisecond},
		failuresCounter(t),
		broken,
		func() (io.Closer, rabbitmq.Channel, error) {
			dials.Add(1)
			return nopCloser{}, &fakeChannel{}, nil
		},
	)

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Go(func() { errs[i] = p.Publish(context.Background(), uuid.New()) })
	}
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(1), dials.Load())
}

func TestPublish_ReconnectFailureAndCancellation(t *testing.T) {
	t.Parallel()

	t.Run("dial failure is returned", func(t *testing.T) {
		t.Parallel()
		p := rabbitmq.NewTestPublisher(
			config.RabbitMQConfig{Queue: "q"},
			failuresCounter(t),
			&fakeChannel{failFirst: 1},
			func() (io.Closer, rabbitmq.Channel, error) { return nil, nil, errors.New("broker down") },
		)
		err := p.Publish(context.Background(), uuid.New())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "broker down")
		assert.Contains(t, err.Error(), "channel closed")
	})

	t.Run("cancelled context stops the wait", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		p := rabbitmq.NewTestPublisher(
			config.RabbitMQConfig{Queue: "q", ReconnectDelay: time.Hour},
			failuresCounter(t),
			&fakeChannel{failFirst: 1},
			func() (io.Closer, rabbitmq.Channel, error) {
				t.Error("must not dial after cancellation")
				return nil, nil, errors.New("unreachable")
			},
		)
		err := p.Publish(ctx, uuid.New())
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// closable reports a fixed IsClosed result, like amqp.Connection and amqp.Channel.
type closable struct{ closed bool }

func (c closable) IsClosed() bool { return c.closed }

type closableConn struct {
	nopCloser
	closable
}

type closableChannel struct {
	fakeChannel
	closable
}

func TestPublisherConnected(t *testing.T) {
	t.Parallel()

	newPublisher := func(conn io.Closer, ch rabbitmq.Channel) *rabbitmq.Publisher {
		p := rabbitmq.NewTestPublisher(config.RabbitMQConfig{Queue: "q"}, failuresCounter(t), ch, nil)
		p.SetConnForTest(conn)
		return p
	}

	tests := []struct {
		name               string
		connDown, chanDown bool
		want               bool
	}{
		{"both open", false, false, true},
		{"connection closed", true, false, false},
		{"channel closed", false, true, false},
	}
	for _, tt := range tests {
		p := newPublisher(closableConn{closable: closable{tt.connDown}}, &closableChannel{closable: closable{tt.chanDown}})
		assert.Equal(t, tt.want, p.Connected(), tt.name)
	}

	assert.False(t, newPublisher(nopCloser{}, &fakeChannel{}).Connected(),
		"connections that cannot report their state count as disconnected")
}
