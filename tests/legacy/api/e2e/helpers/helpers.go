package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// apiBaseURL returns the base URL for the API from environment or default.
func apiBaseURL() string {
	if url := os.Getenv("API_URL"); url != "" {
		return url
	}
	return "http://localhost:8080"
}

// TestClient wraps an HTTP client with authentication headers and helper methods.
type TestClient struct {
	*http.Client
	BaseURL   string
	UserID    string
	TeamID    string
	UserRoles string
	RequestID string
}

// NewTestClient creates a new test client with default authentication headers.
// By default, creates a client with admin role for full access.
func NewTestClient(t *testing.T) *TestClient {
	t.Helper()
	return &TestClient{
		Client:    &http.Client{Timeout: 10 * time.Second},
		BaseURL:   apiBaseURL(),
		UserID:    uuid.New().String(),
		TeamID:    uuid.New().String(),
		UserRoles: "admin,developer",
		RequestID: uuid.New().String(),
	}
}

// NewUnauthenticatedClient creates a client without authentication headers.
func NewUnauthenticatedClient(t *testing.T) *TestClient {
	t.Helper()
	return &TestClient{
		Client:  &http.Client{Timeout: 10 * time.Second},
		BaseURL: apiBaseURL(),
	}
}

// Do executes an HTTP request with authentication headers.
func (c *TestClient) Do(req *http.Request) (*http.Response, error) {
	if c.UserID != "" {
		req.Header.Set("X-User-ID", c.UserID)
	}
	if c.TeamID != "" {
		req.Header.Set("X-Team-ID", c.TeamID)
	}
	if c.UserRoles != "" {
		req.Header.Set("X-User-Roles", c.UserRoles)
	}
	if c.RequestID != "" {
		req.Header.Set("X-Request-ID", c.RequestID)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return c.Client.Do(req)
}

// Request sends method to path with authentication headers. A non-nil body
// is sent as JSON.
func (c *TestClient) Request(method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(jsonBody)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
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

// AssertRequestIDHeader verifies X-Request-ID header is present.
func AssertRequestIDHeader(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header to be present")
	}
}

// GenerateUniqueName creates a unique name for test resources.
func GenerateUniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.New().String()[:8])
}
