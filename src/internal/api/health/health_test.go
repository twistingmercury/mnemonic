package health_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
	"github.com/twistingmercury/mnemonic/internal/api/config"
	"github.com/twistingmercury/mnemonic/internal/api/health"
)

// --- Mocks ---

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

type mockConnVerifier struct {
	err error
}

func (m *mockConnVerifier) VerifyConnectivity(_ context.Context) error {
	return m.err
}

type mockQueue struct{ connected bool }

func (m *mockQueue) Connected() bool { return m.connected }

// offlineOpenAI keeps the OpenAI check off the network: nothing listens on
// port 0.
var offlineOpenAI = config.OpenAIConfig{BaseURL: "http://127.0.0.1:0"}

// descriptors builds the checks for the given database results, with OpenAI
// offline and no queue.
func descriptors(pgErr, neo4jErr error) []heartbeat.DependencyDescriptor {
	return health.Descriptors(health.Dependencies{
		PGPool:      &mockPinger{err: pgErr},
		Neo4jDriver: &mockConnVerifier{err: neo4jErr},
		OpenAI:      offlineOpenAI,
	})
}

func TestDescriptors(t *testing.T) {
	descs := descriptors(nil, nil)

	require.Len(t, descs, 3)
	assert.Equal(t, "PostgreSQL", descs[0].Name)
	assert.Equal(t, "database", descs[0].Type)
	assert.Equal(t, "Neo4j", descs[1].Name)
	assert.Equal(t, "database", descs[1].Type)
	assert.Equal(t, "OpenAI embedding model", descs[2].Name)
	assert.Equal(t, "external_api", descs[2].Type)
}

// --- Individual check tests ---

func TestPostgreSQLCheck_PingSuccess(t *testing.T) {
	result := descriptors(nil, nil)[0].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, "ok", result.Message)
}

func TestPostgreSQLCheck_PingFailure(t *testing.T) {
	result := descriptors(errors.New("connection refused"), nil)[0].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "ping failed")
	assert.Contains(t, result.Message, "connection refused")
}

func TestNeo4jCheck_VerifySuccess(t *testing.T) {
	result := descriptors(nil, nil)[1].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, "ok", result.Message)
}

func TestNeo4jCheck_VerifyFailure(t *testing.T) {
	result := descriptors(nil, errors.New("neo4j unreachable"))[1].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "connectivity check failed")
	assert.Contains(t, result.Message, "neo4j unreachable")
}

// --- RabbitMQ check tests ---

func TestDescriptors_WithQueue(t *testing.T) {
	tests := []struct {
		name      string
		connected bool
		want      heartbeat.Status
	}{
		{"connected", true, heartbeat.StatusOK},
		{"disconnected is a warning", false, heartbeat.StatusWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			descs := health.Descriptors(health.Dependencies{
				PGPool:      &mockPinger{},
				Neo4jDriver: &mockConnVerifier{},
				OpenAI:      offlineOpenAI,
				Queue:       &mockQueue{connected: tt.connected},
			})

			require.Len(t, descs, 4)
			assert.Equal(t, "RabbitMQ", descs[3].Name)
			assert.Equal(t, "queue", descs[3].Type)
			assert.Equal(t, tt.want, descs[3].CheckFunc(context.Background()).Status)
		})
	}
}
