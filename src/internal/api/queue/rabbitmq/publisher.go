// Package rabbitmq provides a RabbitMQ-backed implementation of queue.Publisher.
//
// Operational note: if Publish returns an error the enrichmentjob row remains in PostgreSQL
// with status "pending". Operators should alert on:
//
//	SELECT count(*) FROM enrichment_jobs WHERE status = 'pending' AND created_at < NOW() - INTERVAL '10 minutes'.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/mnemonic/internal/api/config"
)

// channel is the part of *amqp.Channel the publisher uses, so tests can drive
// the reconnect path without a broker.
type channel interface {
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Close() error
}

// Publisher implements queue.Publisher backed by a RabbitMQ broker.
type Publisher struct {
	cfg             config.RabbitMQConfig
	conn            io.Closer
	ch              channel
	dial            func() (io.Closer, channel, error)
	mu              sync.Mutex
	publishFailures metric.Int64Counter
}

// NewPublisher dials the broker, opens a channel, and declares the destination
// queue. It returns an error if any step fails, cleaning up partial resources
// before returning. The meter is used to record publish failure counts.
func NewPublisher(cfg config.RabbitMQConfig, meter metric.Meter) (*Publisher, error) {
	counter, err := meter.Int64Counter(
		"mnemonic.queue.publish_failures_total",
		metric.WithDescription("Total number of times Publish failed after all retry attempts"),
		metric.WithUnit("{failure}"),
	)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: create publish failures counter: %w", err)
	}

	p := &Publisher{cfg: cfg, publishFailures: counter}
	p.dial = p.dialBroker

	conn, ch, err := p.dial()
	if err != nil {
		return nil, err
	}
	p.conn, p.ch = conn, ch
	return p, nil
}

// dialBroker opens a connection and channel and declares the queue, closing
// whatever it opened if a later step fails. Credentials go through SASL and
// are never embedded in the URL.
func (p *Publisher) dialBroker() (io.Closer, channel, error) {
	conn, err := amqp.DialConfig(
		fmt.Sprintf("amqp://%s:%d/", p.cfg.Host, p.cfg.Port),
		amqp.Config{
			SASL:  []amqp.Authentication{&amqp.PlainAuth{Username: p.cfg.User, Password: p.cfg.Password}}, // #nosec G101 — value supplied via config, not hardcoded in source
			Vhost: p.cfg.VHost,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("rabbitmq: dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq: open channel: %w", err)
	}

	if _, err = ch.QueueDeclare(p.cfg.Queue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq: declare queue %q: %w", p.cfg.Queue, err)
	}

	return conn, ch, nil
}

// jobPayload is the JSON body published for each enrichment job.
type jobPayload struct {
	JobID string `json:"job_id"`
}

// headerCarrier adapts amqp.Table to propagation.TextMapCarrier so trace
// context travels to the consumer in the message headers.
type headerCarrier amqp.Table

func (c headerCarrier) Get(key string) string {
	v, _ := c[key].(string)
	return v
}

func (c headerCarrier) Set(key, value string) { c[key] = value }

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// Connected reports whether the broker connection and channel are open. After
// the broker drops them the publisher stays disconnected until the next
// Publish reconnects.
func (p *Publisher) Connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	conn, connOK := p.conn.(interface{ IsClosed() bool })
	ch, chOK := p.ch.(interface{ IsClosed() bool })
	return connOK && chOK && !conn.IsClosed() && !ch.IsClosed()
}

// Publish serialises jobID into a JSON message and delivers it to the
// configured queue with persistent delivery mode. The message headers carry
// the trace context of a producer span, so the consumer can continue the
// trace. On publish failure it waits ReconnectDelay, reconnects once (unless
// another caller already has) and retries before returning the error.
//
// The mutex guards the channel, not the wait: the delay is slept without the
// lock so concurrent publishers are not all stalled behind one reconnect.
func (p *Publisher) Publish(ctx context.Context, jobID uuid.UUID) (err error) {
	ctx, span := otel.Tracer("mnemonic/queue").Start(ctx, "publish "+p.cfg.Queue,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.operation.type", "send"),
			attribute.String("messaging.destination.name", p.cfg.Queue),
			attribute.String("mnemonic.job_id", jobID.String()),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	body, err := json.Marshal(jobPayload{JobID: jobID.String()})
	if err != nil {
		return fmt.Errorf("rabbitmq: marshal payload: %w", err)
	}

	msg := amqp.Publishing{
		Headers:      amqp.Table{},
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		Body:         body,
	}
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier(msg.Headers))

	p.mu.Lock()
	failed := p.ch
	err = failed.PublishWithContext(ctx, "", p.cfg.Queue, false, false, msg)
	p.mu.Unlock()
	if err == nil {
		return nil
	}

	select {
	case <-time.After(p.cfg.ReconnectDelay):
	case <-ctx.Done():
		p.publishFailures.Add(ctx, 1)
		return fmt.Errorf("rabbitmq: publish failed and reconnect cancelled: %w", errors.Join(err, ctx.Err()))
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Another publisher may have reconnected while this one waited; reuse its
	// channel rather than tearing down a healthy connection.
	if p.ch == failed {
		if reconnErr := p.reconnect(); reconnErr != nil {
			p.publishFailures.Add(ctx, 1)
			return fmt.Errorf("rabbitmq: publish failed and reconnect failed: %w", errors.Join(err, reconnErr))
		}
	}
	if retryErr := p.ch.PublishWithContext(ctx, "", p.cfg.Queue, false, false, msg); retryErr != nil {
		p.publishFailures.Add(ctx, 1)
		return fmt.Errorf("rabbitmq: publish retry: %w", retryErr)
	}

	return nil
}

// reconnect closes the stale channel and connection and dials afresh,
// replacing p.conn and p.ch on success. The caller must hold p.mu.
func (p *Publisher) reconnect() error {
	// Ignore close errors on stale resources.
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}

	conn, ch, err := p.dial()
	if err != nil {
		return fmt.Errorf("rabbitmq: reconnect: %w", err)
	}

	p.conn = conn
	p.ch = ch
	return nil
}

// Close releases the channel and connection. Both close errors are joined and
// returned together so callers observe the full picture.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var chErr, connErr error
	if p.ch != nil {
		chErr = p.ch.Close()
	}
	if p.conn != nil {
		connErr = p.conn.Close()
	}
	return errors.Join(chErr, connErr)
}
