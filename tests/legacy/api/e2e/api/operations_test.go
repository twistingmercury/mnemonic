package api_test

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/twistingmercury/librarian-api/tests/e2e/helpers"
)

// =============================================================================
// Operations Endpoint Tests
// Spec: GET /health, GET /version, GET /metrics
// =============================================================================
//
// All operations endpoints are public -- no authentication is required.

// metricsBaseURL returns the base URL for the metrics endpoint.
// Metrics are served by the otelx library on a separate port (default 9090).
func metricsBaseURL() string {
	if url := os.Getenv("METRICS_URL"); url != "" {
		return url
	}
	return "http://localhost:9090"
}

// TestAPIOnlyNetworkBoundary verifies that mnemonic-api exposes no MCP surface.
// MCP is owned by mnemonic-mcp, so the API listener must not route /mcp and the
// former MCP port must reject TCP connections from another container.
func TestAPIOnlyNetworkBoundary(t *testing.T) {
	client := helpers.NewUnauthenticatedClient(t)

	resp, err := client.Request(http.MethodGet, "/mcp", nil)
	if err != nil {
		t.Fatalf("failed to GET /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected /mcp to be unavailable with status 404, got %d", resp.StatusCode)
	}

	apiURL, err := url.Parse(client.BaseURL)
	if err != nil {
		t.Fatalf("parse API URL %q: %v", client.BaseURL, err)
	}

	mcpAddress := net.JoinHostPort(apiURL.Hostname(), "8081")
	conn, err := net.DialTimeout("tcp", mcpAddress, 2*time.Second)
	if err == nil {
		conn.Close()
		t.Fatalf("expected MCP port %s to reject connections", mcpAddress)
	}

	if !isExpectedConnectionFailure(err) {
		t.Fatalf("expected MCP port %s to be unavailable, got: %v", mcpAddress, err)
	}
}

func isExpectedConnectionFailure(err error) bool {
	var netErr net.Error
	if !strings.Contains(err.Error(), "connection refused") &&
		(!errors.As(err, &netErr) || !netErr.Timeout()) {
		return false
	}
	return true
}

// -----------------------------------------------------------------------------
// Health Check (GET /health)
// -----------------------------------------------------------------------------

// TestHealthCheck_AllHealthyReturns200 verifies that when all components
// (postgres, pgvector, neo4j) are healthy, the endpoint returns 200 OK with
// status "OK" and a dependencies array containing each component.
func TestHealthCheck_AllHealthyReturns200(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	// The heartbeat library returns a Response struct with fields:
	//   status, name, resource, machine, utc_date_time, request_duration_ms, dependencies
	// Use a flexible map to parse the response.
	body := helpers.ParseJSON[map[string]any](t, resp)

	status, ok := body["status"].(string)
	if !ok {
		t.Fatalf("expected 'status' field to be a string, got %T", body["status"])
	}

	// Every dependency, OpenAI included (served by the stub), must be healthy.
	if status != "OK" {
		t.Fatalf("expected status 'OK', got %q\nbody: %v", status, body)
	}

	// Verify name field is "mnemonic-api"
	name, ok := body["name"].(string)
	if !ok {
		t.Fatalf("expected 'name' field to be a string, got %T", body["name"])
	}
	if name != "mnemonic-api" {
		t.Fatalf("expected name 'mnemonic-api', got %q", name)
	}
}

// TestHealthCheck_NoAuthRequired verifies the health endpoint returns a
// successful response without any authentication headers. This confirms the
// endpoint is public as specified.
func TestHealthCheck_NoAuthRequired(t *testing.T) {
	client := helpers.NewUnauthenticatedClient(t)

	resp, err := client.Request(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to GET /health without auth: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
}

// TestHealthCheck_ResponseContainsAllComponents verifies the dependencies array
// in the response contains an entry for every dependency registered via the
// health package.
func TestHealthCheck_ResponseContainsAllComponents(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	body := helpers.ParseJSON[map[string]any](t, resp)

	depsRaw, ok := body["dependencies"]
	if !ok {
		t.Fatal("expected 'dependencies' field in health response")
	}

	deps, ok := depsRaw.([]any)
	if !ok {
		t.Fatalf("expected 'dependencies' to be an array, got %T", depsRaw)
	}

	expectedNames := map[string]bool{
		"PostgreSQL":             false,
		"Neo4j":                  false,
		"OpenAI embedding model": false,
	}

	for _, d := range deps {
		depMap, ok := d.(map[string]any)
		if !ok {
			continue
		}
		name, _ := depMap["name"].(string)
		if _, exists := expectedNames[name]; exists {
			expectedNames[name] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected dependency %q not found in health response", name)
		}
	}
}

// TestHealthCheck_ContentTypeIsJSON verifies the Content-Type header is
// application/json (with optional charset parameter).
func TestHealthCheck_ContentTypeIsJSON(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected Content-Type to start with 'application/json', got %q", ct)
	}
}

// -----------------------------------------------------------------------------
// Version (GET /version)
// -----------------------------------------------------------------------------

// TestVersion_ReturnsOKWithVersionInfo verifies the version endpoint returns
// 200 OK with service, version, build_date, and commit fields populated.
func TestVersion_ReturnsOKWithVersionInfo(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/version", nil)
	if err != nil {
		t.Fatalf("failed to GET /version: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	ver := helpers.ParseJSON[helpers.VersionResponse](t, resp)

	if ver.Service != "mnemonic-api" {
		t.Fatalf("expected service 'mnemonic-api', got %q", ver.Service)
	}

	if ver.Version == "" {
		t.Fatal("expected 'version' field to be non-empty")
	}
}

// TestVersion_NoAuthRequired verifies the version endpoint returns a
// successful response without any authentication headers.
func TestVersion_NoAuthRequired(t *testing.T) {
	client := helpers.NewUnauthenticatedClient(t)

	resp, err := client.Request(http.MethodGet, "/version", nil)
	if err != nil {
		t.Fatalf("failed to GET /version without auth: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
}

// TestVersion_ContentTypeIsJSON verifies the Content-Type header is
// application/json (with optional charset parameter).
func TestVersion_ContentTypeIsJSON(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/version", nil)
	if err != nil {
		t.Fatalf("failed to GET /version: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected Content-Type to start with 'application/json', got %q", ct)
	}
}

// TestVersion_FieldsArePresent verifies that all documented fields (service,
// version, build_date, commit) are present in the response. Fields default to
// "n/a" when the binary is built without ldflags.
func TestVersion_FieldsArePresent(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/version", nil)
	if err != nil {
		t.Fatalf("failed to GET /version: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	// Parse into a generic map to verify all expected keys exist.
	body := helpers.ParseJSON[map[string]any](t, resp)

	expectedFields := []string{"service", "version", "build_date", "commit"}
	for _, field := range expectedFields {
		val, ok := body[field]
		if !ok {
			t.Errorf("expected field %q to be present in version response", field)
			continue
		}
		str, ok := val.(string)
		if !ok {
			t.Errorf("expected field %q to be a string, got %T", field, val)
			continue
		}
		if str == "" {
			t.Errorf("expected field %q to be non-empty", field)
		}
	}
}

// -----------------------------------------------------------------------------
// Metrics (GET /metrics)
// -----------------------------------------------------------------------------

// TestMetrics_ReturnsOKWithPrometheusFormat verifies the metrics endpoint
// returns 200 OK with Prometheus exposition format content type.
func TestMetrics_ReturnsOKWithPrometheusFormat(t *testing.T) {
	metricsURL := metricsBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Get(metricsURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET %s/metrics: %v", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	// Prometheus exposition format uses text/plain or the OpenMetrics content type.
	if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
		t.Fatalf("expected Content-Type to contain 'text/plain' or 'openmetrics', got %q", ct)
	}
}

// TestMetrics_NoAuthRequired verifies the metrics endpoint returns a
// successful response without any authentication headers.
func TestMetrics_NoAuthRequired(t *testing.T) {
	metricsURL := metricsBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Get(metricsURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET %s/metrics: %v", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// Swagger UI (GET /swagger/index.html)
// -----------------------------------------------------------------------------

// TestSwaggerUI_ReturnsOK verifies the Swagger UI index page is served and
// returns HTTP 200. The endpoint is public and requires no authentication.
func TestSwaggerUI_ReturnsOK(t *testing.T) {
	client := helpers.NewUnauthenticatedClient(t)

	resp, err := client.Request(http.MethodGet, "/swagger/index.html", nil)
	if err != nil {
		t.Fatalf("failed to GET /swagger/index.html: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
}

// TestMetrics_ContainsStandardGoMetrics verifies the response body contains
// standard Go runtime metrics (e.g., go_goroutines, go_memstats_alloc_bytes)
// as evidence that the Prometheus handler is wired correctly.
func TestMetrics_ContainsStandardGoMetrics(t *testing.T) {
	metricsURL := metricsBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Get(metricsURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET %s/metrics: %v", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body := helpers.ReadBody(t, resp)
	bodyStr := string(body)

	// Standard Go runtime metrics exposed by the default Prometheus registry.
	expectedMetrics := []string{
		"go_goroutines",
		"go_memstats_alloc_bytes",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(bodyStr, metric) {
			t.Errorf("expected metrics body to contain %q", metric)
		}
	}
}
