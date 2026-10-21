package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/twistingmercury/heartbeat/v2"
	"github.com/twistingmercury/librarian/internal/enricher/config"
)

// checkTimeout is the per-check timeout used when probing dependencies.
const checkTimeout = 3 * time.Second

// Pinger is satisfied by *pgxpool.Pool and any type that supports Ping.
type Pinger interface {
	Ping(ctx context.Context) error
}

// ConnectivityVerifier is satisfied by neo4j.DriverWithContext and any type
// that supports VerifyConnectivity.
type ConnectivityVerifier interface {
	VerifyConnectivity(ctx context.Context) error
}

// Dependencies holds the external connections that health checks probe.
type Dependencies struct {
	PGPool      Pinger
	Neo4jDriver ConnectivityVerifier
	OpenAI      config.OpenAIConfig
}

// Descriptors returns the dependency descriptors for the heartbeat Handler.
func Descriptors(deps Dependencies) []heartbeat.DependencyDescriptor {
	openAIBaseURL := strings.TrimRight(deps.OpenAI.BaseURL, "/")
	if openAIBaseURL == "" {
		openAIBaseURL = config.DefaultOpenAIBaseURL
	}

	return []heartbeat.DependencyDescriptor{
		{
			Name:      "PostgreSQL",
			Type:      "database",
			Timeout:   checkTimeout,
			CheckFunc: criticalCheck(deps.PGPool.Ping, "ping failed"),
		},
		{
			Name:      "Neo4j",
			Type:      "database",
			Timeout:   checkTimeout,
			CheckFunc: criticalCheck(deps.Neo4jDriver.VerifyConnectivity, "connectivity check failed"),
		},
		{
			Name:      "OpenAI embedding model",
			Type:      "external_api",
			Timeout:   checkTimeout,
			CheckFunc: openAIModelCheck(openAIBaseURL, deps.OpenAI.APIKey, deps.OpenAI.EmbeddingModel),
		},
		{
			Name:      "OpenAI extraction model",
			Type:      "external_api",
			Timeout:   checkTimeout,
			CheckFunc: openAIModelCheck(openAIBaseURL, deps.OpenAI.APIKey, deps.OpenAI.ExtractionModel),
		},
	}
}

// criticalCheck returns a check that reports Critical, prefixed with
// failMsg, when probe fails. heartbeat cancels ctx when the check's timeout
// expires and records the duration.
func criticalCheck(probe func(context.Context) error, failMsg string) heartbeat.CheckFunc {
	return func(ctx context.Context) (status heartbeat.StatusResult) {
		if err := probe(ctx); err != nil {
			status.Status = heartbeat.StatusCritical
			status.Message = fmt.Sprintf("%s: %v", failMsg, err)
			return status
		}
		status.Status = heartbeat.StatusOK
		status.Message = "ok"
		return status
	}
}

// openAIModelCheck returns a check that retrieves model from the OpenAI models
// endpoint. The call is free and confirms that the API is reachable, the key
// is valid and the key can use the model. Failures are reported as Warning so
// an OpenAI outage shows in /health without failing the probe.
func openAIModelCheck(baseURL, apiKey, model string) heartbeat.CheckFunc {
	modelURL := baseURL + "/models/" + url.PathEscape(model)

	return func(ctx context.Context) (status heartbeat.StatusResult) {
		status.Resource = model

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
		if err != nil {
			status.Status = heartbeat.StatusWarning
			status.Message = "invalid OpenAI base URL"
			return status
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// The error can carry internal addresses, so it is not echoed.
			status.Status = heartbeat.StatusWarning
			status.Message = "request failed"
			if ctx.Err() != nil {
				status.Message = "request timed out"
			}
			return status
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

		status.StatusCode = resp.StatusCode
		if resp.StatusCode != http.StatusOK {
			status.Status = heartbeat.StatusWarning
			status.Message = fmt.Sprintf("models endpoint returned %s", resp.Status)
			return status
		}

		status.Status = heartbeat.StatusOK
		status.Message = "ok"
		return status
	}
}
