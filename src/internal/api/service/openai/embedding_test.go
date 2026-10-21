package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/api/config"
	"github.com/twistingmercury/librarian/internal/api/service/openai"
)

func testEmbeddingConfig() config.OpenAIConfig {
	return config.OpenAIConfig{
		APIKey:              "test-api-key",
		EmbeddingModel:      "text-embedding-3-small",
		EmbeddingDimensions: 4,
		RetryAttempts:       3,
		RetryDelay:          10 * time.Millisecond,
	}
}

// newTestService points an EmbeddingService at a test server via openai.base_url.
func newTestService(cfg config.OpenAIConfig, baseURL string) openai.EmbeddingService {
	cfg.BaseURL = baseURL
	return openai.NewEmbeddingService(cfg)
}

func TestEmbed_Success(t *testing.T) {
	t.Parallel()

	want := []float32{0.1, 0.2, 0.3, 0.4}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/embeddings", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

		var req struct {
			Input      string `json:"input"`
			Model      string `json:"model"`
			Dimensions int    `json:"dimensions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "test input text", req.Input)
		assert.Equal(t, "text-embedding-3-small", req.Model)
		assert.Equal(t, 4, req.Dimensions)

		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": want},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	svc := newTestService(testEmbeddingConfig(), srv.URL)

	got, err := svc.Embed(context.Background(), "test input text")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestEmbed_RetryThenSuccess(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "server error"}`))
			return
		}

		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{1.0, 2.0, 3.0, 4.0}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	svc := newTestService(testEmbeddingConfig(), srv.URL)

	got, err := svc.Embed(context.Background(), "retry test")
	require.NoError(t, err)
	assert.Equal(t, []float32{1.0, 2.0, 3.0, 4.0}, got)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestEmbed_AllRetriesExhausted(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "always fails"}`))
	}))
	defer srv.Close()

	svc := newTestService(testEmbeddingConfig(), srv.URL)

	_, err := svc.Embed(context.Background(), "will fail")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrEmbeddingFailed)
	// 1 initial attempt + 3 retries = 4 total calls
	assert.Equal(t, int32(4), callCount.Load())
}

func TestEmbed_ContextCancellation(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test signals completion.
		<-done
	}))
	defer srv.Close()
	defer close(done)

	svc := newTestService(testEmbeddingConfig(), srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := svc.Embed(ctx, "cancelled")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrEmbeddingFailed)
}

func TestEmbed_InvalidJSONResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	cfg := testEmbeddingConfig()
	cfg.RetryAttempts = 0
	svc := newTestService(cfg, srv.URL)

	_, err := svc.Embed(context.Background(), "bad json")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrEmbeddingFailed)
}

func TestEmbed_EmptyDataResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	cfg := testEmbeddingConfig()
	cfg.RetryAttempts = 0
	svc := newTestService(cfg, srv.URL)

	_, err := svc.Embed(context.Background(), "empty response")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrEmbeddingFailed)
}
