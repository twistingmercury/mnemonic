// Package queue defines the provider-agnostic messaging abstractions used by
// the enricher to consume job messages from a broker.
package queue

import "context"

// Delivery is a provider-agnostic message wrapper. It carries the raw message
// body and two callbacks that the consumer must call to acknowledge or reject
// the message once processing is complete.
type Delivery struct {
	// Body is the raw message payload.
	Body []byte

	// Headers holds the string-valued message headers, including the W3C
	// trace context the producer injected. It satisfies
	// propagation.MapCarrier, so the consumer can continue the trace.
	Headers map[string]string

	// Ack signals the broker that the message was processed successfully.
	Ack func() error

	// Nack signals the broker that processing failed. When requeue is true the
	// broker may redeliver the message; when false the message is discarded or
	// dead-lettered according to broker policy.
	Nack func(requeue bool) error
}

// Subscriber is implemented by all queue provider adapters. The interface is
// intentionally free of provider-specific types so that callers remain
// decoupled from the underlying broker.
type Subscriber interface {
	// Subscribe returns a read-only channel of Delivery messages. The channel
	// is closed when ctx is cancelled. Reconnect logic is handled internally by
	// the implementation; callers need not manage connection lifecycle.
	Subscribe(ctx context.Context) <-chan Delivery

	// Name returns a human-readable description of the subscription, typically
	// the queue name, suitable for logging and metrics labels.
	Name() string

	// Close cleanly shuts down the subscriber, releasing broker resources.
	Close() error
}
