package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/twistingmercury/mnemonic/internal/api/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ErrEmbeddingFailed is returned when embedding generation fails after all retries.
var ErrEmbeddingFailed = errors.New("embedding generation failed")

// EmbeddingService generates vector embeddings from text.
// MVP implementation calls OpenAI text-embedding-3-large.
type EmbeddingService interface {
	// Embed generates a vector embedding for the given text.
	// Returns a float32 slice of length matching the configured dimensions.
	// Returns ErrEmbeddingFailed if the API call fails after retries.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// embeddingsEndpoint is the OpenAI embeddings API URL used when
// openai.base_url is not set.
const embeddingsEndpoint = "https://api.openai.com/v1/embeddings"

type openaiEmbedding struct {
	client     *http.Client
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	retries    int
	retryDelay time.Duration
}

// NewEmbeddingService creates an EmbeddingService backed by the OpenAI embeddings API.
func NewEmbeddingService(cfg config.OpenAIConfig) EmbeddingService {
	endpoint := embeddingsEndpoint
	if cfg.BaseURL != "" {
		endpoint = strings.TrimRight(cfg.BaseURL, "/") + "/embeddings"
	}
	return &openaiEmbedding{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		baseURL:    endpoint,
		apiKey:     cfg.APIKey,
		model:      cfg.EmbeddingModel,
		dimensions: cfg.EmbeddingDimensions,
		retries:    cfg.RetryAttempts,
		retryDelay: cfg.RetryDelay,
	}
}

// embeddingRequest is the OpenAI embeddings API request body.
type embeddingRequest struct {
	Input      string `json:"input"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
}

// embeddingResponse is the OpenAI embeddings API response body.
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed generates a vector embedding for the given text via the OpenAI API.
func (e *openaiEmbedding) Embed(ctx context.Context, text string) ([]float32, error) {
	var lastErr error

	for attempt := range e.retries + 1 {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, err)
		}

		embedding, err := e.doEmbed(ctx, text)
		if err == nil {
			return embedding, nil
		}
		lastErr = err

		// Sleep before retry, unless this was the last attempt.
		if attempt < e.retries {
			trace.SpanFromContext(ctx).AddEvent("openai.embed.retry", trace.WithAttributes(
				attribute.Int("attempt", attempt+1),
				attribute.String("error", err.Error()),
			))
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, ctx.Err())
			case <-time.After(e.retryDelay):
			}
		}
	}

	return nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, lastErr)
}

// doEmbed performs a single embedding API call.
func (e *openaiEmbedding) doEmbed(ctx context.Context, text string) ([]float32, error) {
	reqBody := embeddingRequest{
		Input:      text,
		Model:      e.model,
		Dimensions: e.dimensions,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req) // #nosec G704 -- baseURL is from config, not user input
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(embResp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	return embResp.Data[0].Embedding, nil
}
