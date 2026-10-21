package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
	"github.com/twistingmercury/mnemonic/internal/mcp/config"
	"github.com/twistingmercury/mnemonic/internal/mcp/health"
)

func TestOpenAIModelCheck_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/models/text-embedding-3-small", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"id":"text-embedding-3-small"}`))
	}))
	defer srv.Close()

	result := openAICheck(srv.URL, "sk-test", "text-embedding-3-small")(context.Background())

	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "text-embedding-3-small", result.Resource)
}

func TestOpenAIModelCheck_ErrorStatusIsWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	result := openAICheck(srv.URL, "bad-key", "gpt-4o-mini")(context.Background())

	assert.Equal(t, heartbeat.StatusWarning, result.Status)
	assert.Equal(t, http.StatusUnauthorized, result.StatusCode)
	assert.Contains(t, result.Message, "401")
}

func TestOpenAIModelCheck_UnreachableIsWarning(t *testing.T) {
	result := openAICheck("http://127.0.0.1:0", "sk-test", "gpt-4o-mini")(context.Background())

	assert.Equal(t, heartbeat.StatusWarning, result.Status)
	assert.Equal(t, "request failed", result.Message)
}

func TestDescriptors_OpenAIWarningDoesNotFail(t *testing.T) {
	status, results := heartbeat.CheckDependencies(context.Background(), descriptors(nil, nil))
	assert.Equal(t, heartbeat.StatusWarning, status)
	for _, r := range results[2:] {
		assert.Equal(t, heartbeat.StatusWarning, r.Status, r.Name)
	}
}

func TestDescriptors_OpenAIBaseURLOverridesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models/text-embedding-3-large", r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"text-embedding-3-large"}`))
	}))
	defer srv.Close()

	descs := health.Descriptors(health.Dependencies{
		PGPool:      &mockPinger{},
		Neo4jDriver: &mockConnVerifier{},
		OpenAI: config.OpenAIConfig{
			APIKey:         "sk-test",
			BaseURL:        srv.URL + "/v1/",
			EmbeddingModel: "text-embedding-3-large",
		},
	})

	require.Len(t, descs, 3)
	result := descs[2].CheckFunc(context.Background())
	assert.Equal(t, heartbeat.StatusOK, result.Status)
}

// openAICheck returns the OpenAI descriptor's check for the given settings.
func openAICheck(baseURL, apiKey, model string) heartbeat.CheckFunc {
	return health.Descriptors(health.Dependencies{
		PGPool:      &mockPinger{},
		Neo4jDriver: &mockConnVerifier{},
		OpenAI:      config.OpenAIConfig{BaseURL: baseURL, APIKey: apiKey, EmbeddingModel: model},
	})[2].CheckFunc
}
