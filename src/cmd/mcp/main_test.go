package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian-mcp/internal/config"
)

func TestResolveServerPort_Default(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	t.Setenv(config.EnvConfigFile, "")
	t.Setenv("LIBRARIAN_SERVER_PORT", "")

	port, err := resolveServerPort()
	require.NoError(t, err)
	assert.Equal(t, config.DefaultServerPort, port)
}

func TestResolveServerPort_EnvOverride(t *testing.T) {
	t.Setenv(config.EnvConfigFile, "")
	t.Setenv("LIBRARIAN_SERVER_PORT", "9999")

	port, err := resolveServerPort()
	require.NoError(t, err)
	assert.Equal(t, 9999, port)
}

// TestResolveServerPort_ConfigFile verifies that the probe reads a port set
// only in the config file, as the server does.
func TestResolveServerPort_ConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 9123\n"), 0o600))
	t.Setenv(config.EnvConfigFile, path)
	t.Setenv("LIBRARIAN_SERVER_PORT", "")

	port, err := resolveServerPort()
	require.NoError(t, err)
	assert.Equal(t, 9123, port)
}

func TestResolveServerPort_UnreadableExplicitFile(t *testing.T) {
	t.Setenv(config.EnvConfigFile, filepath.Join(t.TempDir(), "missing.yaml"))

	_, err := resolveServerPort()
	assert.ErrorContains(t, err, "failed to read config file")
}

func TestFormatHealthStatus_ValidJSON(t *testing.T) {
	t.Parallel()

	body := `{"status":"OK","name":"mnemonic"}`
	result := formatHealthStatus([]byte(body))
	assert.Equal(t, "mnemonic: OK", result)
}

func TestFormatHealthStatus_InvalidJSON(t *testing.T) {
	t.Parallel()

	body := "not json"
	result := formatHealthStatus([]byte(body))
	assert.Equal(t, "not json", result)
}

func TestFormatHealthStatus_EmptyStatus(t *testing.T) {
	t.Parallel()

	body := `{"name":"mnemonic"}`
	result := formatHealthStatus([]byte(body))
	// Falls back to raw body because status is empty.
	assert.Equal(t, body, result)
}

func TestCheckHealth_Healthy(t *testing.T) {
	body := `{"status":"OK","name":"mnemonic"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, config.DefaultHealthPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	// Extract port from test server address.
	port := extractPort(t, srv.URL)
	t.Setenv("LIBRARIAN_SERVER_PORT", port)

	exitCode := checkHealth()
	assert.Equal(t, 0, exitCode)
}

func TestCheckHealth_Unhealthy(t *testing.T) {
	body := `{"status":"Critical","name":"mnemonic"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	port := extractPort(t, srv.URL)
	t.Setenv("LIBRARIAN_SERVER_PORT", port)

	exitCode := checkHealth()
	assert.Equal(t, 1, exitCode)
}

func TestCheckHealth_Unreachable(t *testing.T) {
	// Use a port where nothing is listening.
	t.Setenv("LIBRARIAN_SERVER_PORT", "1")

	exitCode := checkHealth()
	assert.Equal(t, 1, exitCode)
}

func TestCheckHealth_BadPort(t *testing.T) {
	t.Setenv("LIBRARIAN_SERVER_PORT", "not_a_number")

	// The value does not decode as a port, so the probe fails before any request.
	exitCode := checkHealth()
	assert.Equal(t, 1, exitCode)
}

// extractPort returns the port of a test server URL.
func extractPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u.Port()
}
