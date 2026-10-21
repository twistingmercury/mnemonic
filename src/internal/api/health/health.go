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
	"github.com/twistingmercury/librarian/internal/api/config"
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

// QueueConnection is satisfied by *rabbitmq.Publisher.
type QueueConnection interface {
	Connected() bool
}

// Dependencies holds the external connections that health checks probe.
// Queue is optional; without it no RabbitMQ check is registered.
type Dependencies struct {
	PGPool      Pinger
	Neo4jDriver ConnectivityVerifier
	OpenAI      config.OpenAIConfig
	Queue       QueueConnection
}

// Descriptors returns the dependency checks for the heartbeat handler.
// PostgreSQL and Neo4j are Critical when unreachable; OpenAI and the queue
// only warn.
func Descriptors(d Dependencies) []heartbeat.DependencyDescriptor {
	openAIBaseURL := config.DefaultOpenAIBaseURL
	if d.OpenAI.BaseURL != "" {
		openAIBaseURL = strings.TrimRight(d.OpenAI.BaseURL, "/")
	}

	descriptors := []heartbeat.DependencyDescriptor{
		{
			Name:      "PostgreSQL",
			Type:      "database",
			Timeout:   checkTimeout,
			CheckFunc: probe("ping failed", d.PGPool.Ping),
		},
		{
			Name:      "Neo4j",
			Type:      "database",
			Timeout:   checkTimeout,
			CheckFunc: probe("connectivity check failed", d.Neo4jDriver.VerifyConnectivity),
		},
		{
			Name:      "OpenAI embedding model",
			Type:      "external_api",
			Timeout:   checkTimeout,
			CheckFunc: openAIModelCheck(openAIBaseURL, d.OpenAI.APIKey, d.OpenAI.EmbeddingModel),
		},
	}

	if d.Queue != nil {
		descriptors = append(descriptors, heartbeat.DependencyDescriptor{
			Name:      "RabbitMQ",
			Type:      "queue",
			Timeout:   checkTimeout,
			CheckFunc: queueCheck(d.Queue),
		})
	}

	return descriptors
}

// probe returns a check that is Critical, naming the failure, when fn errors.
// heartbeat cancels ctx when the check's timeout expires and records the
// duration.
func probe(failure string, fn func(context.Context) error) heartbeat.CheckFunc {
	return func(ctx context.Context) (status heartbeat.StatusResult) {
		if err := fn(ctx); err != nil {
			status.Status = heartbeat.StatusCritical
			status.Message = fmt.Sprintf("%s: %v", failure, err)
			return status
		}
		status.Status = heartbeat.StatusOK
		status.Message = "ok"
		return status
	}
}

// queueCheck returns a check that reports whether the enrichment job
// publisher is connected. A disconnected queue is a Warning: patterns are
// still written and their job rows stay pending until they are published.
func queueCheck(q QueueConnection) heartbeat.CheckFunc {
	return func(_ context.Context) (status heartbeat.StatusResult) {
		if !q.Connected() {
			status.Status = heartbeat.StatusWarning
			status.Message = "publisher disconnected; reconnects on the next publish"
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
