package openai

import (
	"context"
	"errors"
	"time"

	"github.com/twistingmercury/mnemonic/internal/enricher/config"
)

// ErrEmbeddingFailed is returned when embedding generation fails after all retries.
var ErrEmbeddingFailed = errors.New("embedding generation failed")

// errEmptyEmbeddingResponse is returned when the API returns no embedding data.
var errEmptyEmbeddingResponse = errors.New("empty embedding response")

// EmbeddingService generates vector embeddings from text.
// MVP implementation calls OpenAI text-embedding-3-large.
type EmbeddingService interface {
	// Embed generates a vector embedding for the given text.
	// Returns a float32 slice of length matching the configured dimensions.
	// Returns ErrEmbeddingFailed if the API call fails after retries.
	Embed(ctx context.Context, text string) ([]float32, error)
}

type openaiEmbedding struct {
	client
	model      string
	dimensions int
}

// NewEmbeddingService creates an EmbeddingService backed by the OpenAI
// embeddings API at cfg.BaseURL.
func NewEmbeddingService(cfg config.OpenAIConfig) EmbeddingService {
	return &openaiEmbedding{
		client:     newClient(cfg, 30*time.Second),
		model:      cfg.EmbeddingModel,
		dimensions: cfg.EmbeddingDimensions,
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
	var embedding []float32
	err := e.withRetry(ctx, ErrEmbeddingFailed, "openai.embed.retry", func() error {
		var resp embeddingResponse
		req := embeddingRequest{Input: text, Model: e.model, Dimensions: e.dimensions}
		if err := e.postJSON(ctx, "/embeddings", req, &resp); err != nil {
			return err
		}
		if len(resp.Data) == 0 {
			return errEmptyEmbeddingResponse
		}
		embedding = resp.Data[0].Embedding
		return nil
	})
	if err != nil {
		return nil, err
	}
	return embedding, nil
}
