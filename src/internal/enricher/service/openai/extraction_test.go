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
	"github.com/twistingmercury/librarian/internal/enricher/config"
	"github.com/twistingmercury/librarian/internal/enricher/service/openai"
)

func testExtractionConfig() config.OpenAIConfig {
	return config.OpenAIConfig{
		APIKey:          "test-api-key",
		ExtractionModel: "gpt-4o-mini",
		RetryAttempts:   3,
		RetryDelay:      10 * time.Millisecond,
	}
}

// chatCompletionResponse builds a mock chat completion response with the given content.
func chatCompletionResponse(content string) map[string]any {
	return map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": content,
				},
			},
		},
	}
}

func TestExtract_Success(t *testing.T) {
	t.Parallel()

	extractionJSON := `{"concepts": ["Error Handling", "retry logic"], "technologies": ["Go", "Context Package"], "practices": ["Defensive Programming"]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "gpt-4o-mini", req.Model)
		require.Len(t, req.Messages, 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Equal(t, "user", req.Messages[1].Role)
		assert.Contains(t, req.Messages[1].Content, "test content")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatCompletionResponse(extractionJSON))
	}))
	defer srv.Close()

	svc := openai.NewExtractionService(withBaseURL(testExtractionConfig(), srv.URL))

	concepts, err := svc.Extract(context.Background(), "test content")
	require.NoError(t, err)

	expected := []openai.Concept{
		{Name: "error handling", Type: "domain"},
		{Name: "retry logic", Type: "domain"},
		{Name: "go", Type: "technology"},
		{Name: "context package", Type: "technology"},
		{Name: "defensive programming", Type: "practice"},
	}
	assert.Equal(t, expected, concepts)
}

func TestExtract_RetryThenSuccess(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	extractionJSON := `{"concepts": ["testing"], "technologies": [], "practices": []}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error": "overloaded"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatCompletionResponse(extractionJSON))
	}))
	defer srv.Close()

	svc := openai.NewExtractionService(withBaseURL(testExtractionConfig(), srv.URL))

	concepts, err := svc.Extract(context.Background(), "retry content")
	require.NoError(t, err)
	assert.Len(t, concepts, 1)
	assert.Equal(t, "testing", concepts[0].Name)
	assert.Equal(t, "domain", concepts[0].Type)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestExtract_AllRetriesExhausted(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "always fails"}`))
	}))
	defer srv.Close()

	svc := openai.NewExtractionService(withBaseURL(testExtractionConfig(), srv.URL))

	_, err := svc.Extract(context.Background(), "will fail")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrExtractionFailed)
	// 1 initial attempt + 3 retries = 4 total calls
	assert.Equal(t, int32(4), callCount.Load())
}

func TestExtract_ContextCancellation(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test signals completion.
		<-done
	}))
	defer srv.Close()
	defer close(done)

	svc := openai.NewExtractionService(withBaseURL(testExtractionConfig(), srv.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := svc.Extract(ctx, "cancelled")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrExtractionFailed)
}

func TestExtract_InvalidJSONResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a valid chat response wrapping invalid extraction JSON.
		json.NewEncoder(w).Encode(chatCompletionResponse(`not valid json`))
	}))
	defer srv.Close()

	cfg := testExtractionConfig()
	cfg.RetryAttempts = 0
	svc := openai.NewExtractionService(withBaseURL(cfg, srv.URL))

	_, err := svc.Extract(context.Background(), "bad json content")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrExtractionFailed)
}

func TestExtract_EmptyChoicesResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	cfg := testExtractionConfig()
	cfg.RetryAttempts = 0
	svc := openai.NewExtractionService(withBaseURL(cfg, srv.URL))

	_, err := svc.Extract(context.Background(), "empty choices")
	require.Error(t, err)
	assert.ErrorIs(t, err, openai.ErrExtractionFailed)
}

func TestExtract_NormalizesToLowercase(t *testing.T) {
	t.Parallel()

	extractionJSON := `{"concepts": ["  Error Handling  "], "technologies": ["  GO  "], "practices": ["  TDD  "]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatCompletionResponse(extractionJSON))
	}))
	defer srv.Close()

	svc := openai.NewExtractionService(withBaseURL(testExtractionConfig(), srv.URL))

	concepts, err := svc.Extract(context.Background(), "normalization test")
	require.NoError(t, err)
	require.Len(t, concepts, 3)
	assert.Equal(t, "error handling", concepts[0].Name)
	assert.Equal(t, "go", concepts[1].Name)
	assert.Equal(t, "tdd", concepts[2].Name)
}

func TestExtract_EmptyCategories(t *testing.T) {
	t.Parallel()

	extractionJSON := `{"concepts": [], "technologies": [], "practices": []}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatCompletionResponse(extractionJSON))
	}))
	defer srv.Close()

	svc := openai.NewExtractionService(withBaseURL(testExtractionConfig(), srv.URL))

	concepts, err := svc.Extract(context.Background(), "nothing to extract")
	require.NoError(t, err)
	assert.Empty(t, concepts)
}
