package rabbitmq_test

import (
	"context"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/mnemonic/internal/api/queue/rabbitmq"
)

// TestHeaderCarrier_RoundTrip verifies that trace context injected into AMQP
// headers can be extracted again, which is what the consumer relies on.
func TestHeaderCarrier_RoundTrip(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "publish")
	defer span.End()

	headers := amqp.Table{}
	prop := propagation.TraceContext{}
	prop.Inject(ctx, rabbitmq.HeaderCarrier(headers))

	require.Contains(t, headers, "traceparent")

	got := trace.SpanContextFromContext(prop.Extract(context.Background(), rabbitmq.HeaderCarrier(headers)))
	assert.Equal(t, span.SpanContext().TraceID(), got.TraceID())
	assert.Equal(t, span.SpanContext().SpanID(), got.SpanID())
}
