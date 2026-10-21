package health_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
	"github.com/twistingmercury/librarian/internal/enricher/config"
	"github.com/twistingmercury/librarian/internal/enricher/health"
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

// descriptors builds the descriptors with an OpenAI base URL nothing listens
// on, which keeps the checks off the network.
func descriptors(pgErr, neo4jErr error) []heartbeat.DependencyDescriptor {
	return health.Descriptors(health.Dependencies{
		PGPool:      &mockPinger{err: pgErr},
		Neo4jDriver: &mockConnVerifier{err: neo4jErr},
		OpenAI:      config.OpenAIConfig{BaseURL: "http://127.0.0.1:0"},
	})
}

func TestDescriptors(t *testing.T) {
	descs := descriptors(nil, nil)

	require.Len(t, descs, 4)
	assert.Equal(t, "PostgreSQL", descs[0].Name)
	assert.Equal(t, "database", descs[0].Type)
	assert.Equal(t, "Neo4j", descs[1].Name)
	assert.Equal(t, "database", descs[1].Type)
	assert.Equal(t, "OpenAI embedding model", descs[2].Name)
	assert.Equal(t, "external_api", descs[2].Type)
	assert.Equal(t, "OpenAI extraction model", descs[3].Name)
	assert.Equal(t, "external_api", descs[3].Type)
}

// --- Individual check tests ---

func TestCheckPostgreSQLHealth_PingSuccess(t *testing.T) {
	result := descriptors(nil, nil)[0].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, "ok", result.Message)
}

func TestCheckPostgreSQLHealth_PingFailure(t *testing.T) {
	result := descriptors(errors.New("connection refused"), nil)[0].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "ping failed")
	assert.Contains(t, result.Message, "connection refused")
}

func TestCheckNeo4jHealth_VerifySuccess(t *testing.T) {
	result := descriptors(nil, nil)[1].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, "ok", result.Message)
}

func TestCheckNeo4jHealth_VerifyFailure(t *testing.T) {
	result := descriptors(nil, errors.New("neo4j unreachable"))[1].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "connectivity check failed")
	assert.Contains(t, result.Message, "neo4j unreachable")
}
