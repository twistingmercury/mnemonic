package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/librarian-enricher/internal/config"
)

// client is the HTTP, auth and retry plumbing the embedding and extraction
// services share.
type client struct {
	http       *http.Client
	baseURL    string
	apiKey     string
	retries    int
	retryDelay time.Duration
}

func newClient(cfg config.OpenAIConfig, timeout time.Duration) client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = config.DefaultOpenAIBaseURL
	}
	return client{
		http:       &http.Client{Timeout: timeout, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     cfg.APIKey,
		retries:    cfg.RetryAttempts,
		retryDelay: cfg.RetryDelay,
	}
}

// withRetry calls fn up to retries+1 times, waiting retryDelay between
// attempts and adding a span event named event before each retry. When every
// attempt fails, or ctx ends, it returns failed wrapping the cause.
func (c *client) withRetry(ctx context.Context, failed error, event string, fn func() error) error {
	var lastErr error
	for attempt := range c.retries + 1 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", failed, err)
		}
		if lastErr = fn(); lastErr == nil {
			return nil
		}
		if attempt < c.retries {
			trace.SpanFromContext(ctx).AddEvent(event, trace.WithAttributes(
				attribute.Int("attempt", attempt+1),
				attribute.String("error", lastErr.Error()),
			))
			select {
			case <-ctx.Done():
				return fmt.Errorf("%w: %w", failed, ctx.Err())
			case <-time.After(c.retryDelay):
			}
		}
	}
	return fmt.Errorf("%w: %w", failed, lastErr)
}

// postJSON POSTs in as JSON to baseURL+path and decodes a 200 response's body
// into out.
func (c *client) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req) // #nosec G704 -- baseURL is from config, not user input
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}
