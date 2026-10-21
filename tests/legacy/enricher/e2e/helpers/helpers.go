package helpers

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// envOr returns the env var key, or def when it is unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestClient wraps an HTTP client with helper methods for E2E tests.
type TestClient struct {
	*http.Client
	BaseURL string
}

// NewTestClient creates a new test client pointed at the enricher API.
func NewTestClient(t *testing.T) *TestClient {
	t.Helper()
	return &TestClient{
		Client:  &http.Client{Timeout: 10 * time.Second},
		BaseURL: envOr("ENRICHER_URL", "http://localhost:8080"),
	}
}

// Get performs a GET request to the specified path, accepting JSON.
func (c *TestClient) Get(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return c.Client.Do(req)
}

// ReadBody reads and closes the response body.
func ReadBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return body
}

// ParseJSON unmarshals JSON response body into the provided struct.
func ParseJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var result T
	body := ReadBody(t, resp)
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nBody: %s", err, string(body))
	}
	return result
}

// AssertStatusCode verifies the response status code matches expected.
func AssertStatusCode(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d\nBody: %s", expected, resp.StatusCode, string(body))
	}
}
