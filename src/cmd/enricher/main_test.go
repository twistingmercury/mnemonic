package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/enricher/config"
)

// mustParseFlags parses args with the production flag set.
func mustParseFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	flags, err := parseFlags(args)
	require.NoError(t, err)
	return flags
}

func TestResolveServerPort_Default(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	t.Setenv("LIBRARIAN_SERVER_PORT", "")

	port, err := resolveServerPort(mustParseFlags(t))
	require.NoError(t, err)
	assert.Equal(t, config.DefaultServerPort, port)
}

func TestResolveServerPort_EnvOverride(t *testing.T) {
	t.Setenv("LIBRARIAN_SERVER_PORT", "9999")

	port, err := resolveServerPort(mustParseFlags(t))
	require.NoError(t, err)
	assert.Equal(t, 9999, port)
}

// TestResolveServerPort_ConfigFile verifies the probe reads the port from the
// same config file the server would load.
func TestResolveServerPort_ConfigFile(t *testing.T) {
	t.Setenv("LIBRARIAN_SERVER_PORT", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8181\n"), 0o600))

	port, err := resolveServerPort(mustParseFlags(t, "--config", path))
	require.NoError(t, err)
	assert.Equal(t, 8181, port)
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
	resp := map[string]interface{}{
		"status": "OK",
		"name":   "mnemonic",
	}
	body, err := json.Marshal(resp)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, config.DefaultHealthPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	t.Setenv("LIBRARIAN_SERVER_PORT", serverPort(t, srv))

	exitCode := checkHealth(mustParseFlags(t))
	assert.Equal(t, 0, exitCode)
}

func TestCheckHealth_Unhealthy(t *testing.T) {
	resp := map[string]interface{}{
		"status": "Critical",
		"name":   "mnemonic",
	}
	body, err := json.Marshal(resp)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	t.Setenv("LIBRARIAN_SERVER_PORT", serverPort(t, srv))

	exitCode := checkHealth(mustParseFlags(t))
	assert.Equal(t, 1, exitCode)
}

func TestCheckHealth_Unreachable(t *testing.T) {
	// Use a port where nothing is listening.
	t.Setenv("LIBRARIAN_SERVER_PORT", "1")

	exitCode := checkHealth(mustParseFlags(t))
	assert.Equal(t, 1, exitCode)
}

func TestCheckHealth_BadPort(t *testing.T) {
	t.Setenv("LIBRARIAN_SERVER_PORT", "not_a_number")

	// A non-numeric port is rejected when it is decoded, producing exit code 1.
	exitCode := checkHealth(mustParseFlags(t))
	assert.Equal(t, 1, exitCode)
}

// serverPort returns the port of a test server.
func serverPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u.Port()
}
