package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/twistingmercury/librarian/internal/enricher/config"
)

// ErrExtractionFailed is returned when concept extraction fails after all retries.
var ErrExtractionFailed = errors.New("concept extraction failed")

// errEmptyExtractionResponse is returned when the API returns no choices.
var errEmptyExtractionResponse = errors.New("empty chat response: no choices returned")

// Concept represents an entity extracted from pattern content.
type Concept struct {
	Name string `json:"name"` // Normalized to lowercase.
	Type string `json:"type"` // "technology", "practice", or "domain".
}

// ExtractionService extracts structured concepts from text using an LLM.
// MVP implementation calls OpenAI gpt-4o-mini chat completions.
type ExtractionService interface {
	// Extract identifies concepts, technologies, and practices in the text.
	// Returns a slice of Concept with normalized lowercase names.
	// Returns ErrExtractionFailed if the API call fails after retries.
	Extract(ctx context.Context, text string) ([]Concept, error)
}

// systemPrompt instructs the LLM to extract entities from pattern content.
const systemPrompt = `Extract key concepts from this pattern document.

Return JSON with:
- concepts: General programming concepts
- technologies: Languages, frameworks, tools
- practices: Best practices, patterns, methodologies

Return ONLY valid JSON with no additional text. Example format:
{"concepts": ["error handling"], "technologies": ["Go"], "practices": ["defensive programming"]}`

type openaiExtraction struct {
	client
	model string
}

// NewExtractionService creates an ExtractionService backed by the OpenAI chat
// completions API at cfg.BaseURL.
func NewExtractionService(cfg config.OpenAIConfig) ExtractionService {
	return &openaiExtraction{client: newClient(cfg, 60*time.Second), model: cfg.ExtractionModel}
}

// chatRequest is the OpenAI chat completions API request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

// chatMessage represents a single message in a chat completion request.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the OpenAI chat completions API response body.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// extractionResult holds the raw LLM extraction output before mapping to Concepts.
type extractionResult struct {
	Concepts     []string `json:"concepts"`
	Technologies []string `json:"technologies"`
	Practices    []string `json:"practices"`
}

// Extract identifies concepts, technologies, and practices in the given text.
func (e *openaiExtraction) Extract(ctx context.Context, text string) ([]Concept, error) {
	var concepts []Concept
	err := e.withRetry(ctx, ErrExtractionFailed, "openai.extract.retry", func() error {
		req := chatRequest{
			Model: e.model,
			Messages: []chatMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: "Pattern content:\n" + text},
			},
		}
		var resp chatResponse
		if err := e.postJSON(ctx, "/chat/completions", req, &resp); err != nil {
			return err
		}
		if len(resp.Choices) == 0 {
			return errEmptyExtractionResponse
		}
		var err error
		concepts, err = parseConcepts(resp.Choices[0].Message.Content)
		return err
	})
	if err != nil {
		return nil, err
	}
	return concepts, nil
}

// parseConcepts parses the LLM JSON output into a normalized slice of Concept.
func parseConcepts(content string) ([]Concept, error) {
	var result extractionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("unmarshal extraction result: %w", err)
	}

	var concepts []Concept

	for _, name := range result.Concepts {
		concepts = append(concepts, Concept{
			Name: strings.ToLower(strings.TrimSpace(name)),
			Type: "domain",
		})
	}
	for _, name := range result.Technologies {
		concepts = append(concepts, Concept{
			Name: strings.ToLower(strings.TrimSpace(name)),
			Type: "technology",
		})
	}
	for _, name := range result.Practices {
		concepts = append(concepts, Concept{
			Name: strings.ToLower(strings.TrimSpace(name)),
			Type: "practice",
		})
	}

	return concepts, nil
}
