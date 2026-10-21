// Package rabbitmq provides a RabbitMQ implementation of the queue.Subscriber
// interface backed by github.com/rabbitmq/amqp091-go.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"

	"github.com/twistingmercury/librarian-enricher/internal/config"
	"github.com/twistingmercury/librarian-enricher/internal/queue"
)

// amqpURL builds an AMQP connection URL from cfg, escaping the credentials
// and virtual host.
func amqpURL(cfg config.RabbitMQConfig) string {
	return amqp.URI{
		Scheme:   "amqp",
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.User,
		Password: cfg.Password,
		Vhost:    cfg.VHost,
	}.String()
}

// RabbitMQSubscriber implements queue.Subscriber for RabbitMQ.
type RabbitMQSubscriber struct {
	cfg  config.RabbitMQConfig
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewSubscriber dials RabbitMQ, sets up the QoS prefetch, and returns a
// ready-to-use queue.Subscriber. The caller is responsible for calling Close
// when the subscriber is no longer needed.
func NewSubscriber(cfg config.RabbitMQConfig) (queue.Subscriber, error) {
	conn, ch, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	return &RabbitMQSubscriber{cfg: cfg, conn: conn, ch: ch}, nil
}

// dial opens a new connection and channel, and configures QoS. A prefetch
// count of 0 means no limit.
func dial(cfg config.RabbitMQConfig) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(amqpURL(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("rabbitmq: dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq: open channel: %w", err)
	}

	if err = ch.Qos(cfg.PrefetchCount, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq: set qos: %w", err)
	}

	return conn, ch, nil
}

// Name returns the queue name from the configuration.
func (s *RabbitMQSubscriber) Name() string {
	return s.cfg.Queue
}

// Close releases the channel and connection, attempting both and joining
// their errors.
func (s *RabbitMQSubscriber) Close() error {
	return errors.Join(s.ch.Close(), s.conn.Close())
}

// Subscribe starts consuming from the configured queue and returns a channel
// of provider-agnostic queue.Delivery values. The goroutine runs until ctx is
// cancelled. If the broker connection is lost the subscriber reconnects after
// cfg.ReconnectDelay, honouring ctx cancellation during the wait.
func (s *RabbitMQSubscriber) Subscribe(ctx context.Context) <-chan queue.Delivery {
	out := make(chan queue.Delivery)
	go s.consumeLoop(ctx, out)
	return out
}

// consumeLoop is the long-running goroutine that reads from the broker and
// adapts amqp091.Delivery values to queue.Delivery. On error it reconnects
// with backoff.
func (s *RabbitMQSubscriber) consumeLoop(ctx context.Context, out chan<- queue.Delivery) {
	defer close(out)

	for {
		deliveries, err := s.ch.Consume(
			s.cfg.Queue,
			"",    // consumer tag — let the broker assign one
			false, // auto-ack: false — callers must call Ack/Nack
			false, // exclusive
			false, // no-local
			false, // no-wait
			nil,   // args
		)
		if err != nil {
			log.Error().Err(err).Str("queue", s.cfg.Queue).Msg("rabbitmq: consume failed, will reconnect")
			if !s.waitAndReconnect(ctx) {
				return
			}
			continue
		}

		log.Info().Str("queue", s.cfg.Queue).Msg("rabbitmq: consuming")

		if done := s.drain(ctx, deliveries, out); done {
			return
		}

		// Channel was closed by the broker; attempt reconnect.
		log.Warn().Str("queue", s.cfg.Queue).Msg("rabbitmq: delivery channel closed, reconnecting")
		if !s.waitAndReconnect(ctx) {
			return
		}
	}
}

// drain reads from the amqp delivery channel and forwards adapted messages to
// out. It returns true when ctx is done (caller should exit) and false when
// the amqp channel closed (caller should reconnect).
func (s *RabbitMQSubscriber) drain(ctx context.Context, deliveries <-chan amqp.Delivery, out chan<- queue.Delivery) bool {
	for {
		select {
		case <-ctx.Done():
			return true
		case d, ok := <-deliveries:
			if !ok {
				return false
			}
			adapted := adapt(d)
			select {
			case out <- adapted:
			case <-ctx.Done():
				return true
			}
		}
	}
}

// adapt converts an amqp091.Delivery to a provider-agnostic queue.Delivery.
// Only string headers are kept; trace context is always a string.
func adapt(d amqp.Delivery) queue.Delivery {
	headers := make(map[string]string, len(d.Headers))
	for k, v := range d.Headers {
		if s, ok := v.(string); ok {
			headers[k] = s
		}
	}
	return queue.Delivery{
		Body:    d.Body,
		Headers: headers,
		Ack: func() error {
			return d.Ack(false)
		},
		Nack: func(requeue bool) error {
			return d.Nack(false, requeue)
		},
	}
}

// waitAndReconnect sleeps for cfg.ReconnectDelay (or until ctx is cancelled)
// then re-dials the broker. It returns false if the caller should stop.
func (s *RabbitMQSubscriber) waitAndReconnect(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(s.cfg.ReconnectDelay):
	}

	conn, ch, err := dial(s.cfg)
	if err != nil {
		log.Error().Err(err).Str("queue", s.cfg.Queue).Msg("rabbitmq: reconnect failed")
		return true // keep the loop alive; next iteration will retry
	}

	// Close stale resources before replacing them.
	_ = s.ch.Close()
	_ = s.conn.Close()

	s.conn = conn
	s.ch = ch
	log.Info().Str("queue", s.cfg.Queue).Msg("rabbitmq: reconnected")
	return true
}
