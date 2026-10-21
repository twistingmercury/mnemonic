package rabbitmq

import (
	"io"

	"go.opentelemetry.io/otel/metric"

	"github.com/twistingmercury/librarian/internal/api/config"
)

// HeaderCarrier exposes headerCarrier for unit testing.
type HeaderCarrier = headerCarrier

// Channel exposes the channel interface so tests can supply fakes.
type Channel = channel

// NewTestPublisher builds a Publisher around a fake channel and dialer, so the
// publish and reconnect paths can run without a broker.
func NewTestPublisher(cfg config.RabbitMQConfig, failures metric.Int64Counter, ch Channel, dial func() (io.Closer, Channel, error)) *Publisher {
	return &Publisher{cfg: cfg, ch: ch, dial: dial, publishFailures: failures}
}

// SetConnForTest replaces the publisher's broker connection.
func (p *Publisher) SetConnForTest(conn io.Closer) { p.conn = conn }
