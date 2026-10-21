package rabbitmq

import (
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/mnemonic/internal/enricher/config"
)

// minimalConfig returns a RabbitMQConfig with enough fields set for unit tests
// that do not dial a real broker.
func minimalConfig() config.RabbitMQConfig {
	return config.RabbitMQConfig{
		Host:           "localhost",
		Port:           5672,
		User:           "guest",
		Password:       "guest",
		VHost:          "/",
		Queue:          "test-queue",
		PrefetchCount:  10,
		ReconnectDelay: 100 * time.Millisecond,
	}
}

// TestAMQPURL verifies the URL round-trips, including credentials and a vhost
// that need escaping.
func TestAMQPURL(t *testing.T) {
	t.Parallel()

	for _, cfg := range []config.RabbitMQConfig{
		minimalConfig(),
		{Host: "mq.example.com", Port: 5673, User: "app@corp", Password: "p@ss:w/rd%", VHost: "team/a b"},
	} {
		uri, err := amqp.ParseURI(amqpURL(cfg))
		require.NoError(t, err)
		assert.Equal(t, cfg.Host, uri.Host)
		assert.Equal(t, cfg.Port, uri.Port)
		assert.Equal(t, cfg.User, uri.Username)
		assert.Equal(t, cfg.Password, uri.Password)
		assert.Equal(t, cfg.VHost, uri.Vhost)
	}
}

func TestRabbitMQSubscriber_Name(t *testing.T) {
	t.Parallel()
	// Build a subscriber directly without dialling so the test works offline.
	s := &RabbitMQSubscriber{cfg: minimalConfig()}
	assert.Equal(t, "test-queue", s.Name())
}

func TestAdapt_headers(t *testing.T) {
	t.Parallel()
	d := adapt(amqp.Delivery{
		Body: []byte("body"),
		Headers: amqp.Table{
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"x-retries":   int32(3),
		},
	})
	assert.Equal(t, []byte("body"), d.Body)
	assert.Equal(t, map[string]string{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}, d.Headers, "only string headers are kept")
}
