package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/pflag"
	"github.com/twistingmercury/mnemonic/internal/mcp/config"
	"github.com/twistingmercury/mnemonic/internal/mcp/server"
	"github.com/twistingmercury/mnemonic/internal/mcp/version"
)

var verFlag = pflag.Bool("version", false, "Displays current version information for mnemonic")
var healthFlag = pflag.Bool("health", false, "Get the current health of the service")

// @title Mnemonic API
// @version 1.0
// @description Operations API for the Mnemonic MCP search service
// @host localhost:8080
// @BasePath /v1/api
// @schemes http
func main() {
	pflag.Parse()

	if *verFlag {
		fmt.Println(version.Print())
		os.Exit(0)
	}

	if *healthFlag {
		exitCode := checkHealth()
		os.Exit(exitCode)
	}

	cfg, err := config.Load()

	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	if err := server.ListenAndServe(cfg); err != nil {
		log.Fatalf("exited with err: %s\n", err.Error()) // #nosec G706 -- taint is the process's own argv (--config), set by the operator who launched it
	}
}

// healthCheckTimeout is the HTTP client timeout for the CLI health probe.
// Kept short because Docker healthcheck has its own outer timeout.
const healthCheckTimeout = 3 * time.Second

// checkHealth makes an HTTP GET request to the running server's /health
// endpoint and reports the result. It is designed for use as a Docker
// HEALTHCHECK command in scratch/static containers where curl is unavailable.
func checkHealth() (exitCode int) {
	port, err := resolveServerPort()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		return 1
	}
	url := fmt.Sprintf("http://localhost:%d%s", port, config.DefaultHealthPath)

	client := &http.Client{Timeout: healthCheckTimeout}

	resp, err := client.Get(url) // #nosec G107 G704 -- host is literal localhost and path is constant; only the integer port comes from config
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		fmt.Println(formatHealthStatus(body))
		return 0
	}

	fmt.Fprintf(os.Stderr, "unhealthy: HTTP %d\n", resp.StatusCode)
	if len(body) > 0 {
		fmt.Fprintln(os.Stderr, formatHealthStatus(body))
	}
	return 1
}

// resolveServerPort reads server.port through the same resolver the server
// loads from, so a port set only in the config file is still probed, but
// without full validation: the probe must not need database or OpenAI
// credentials, nor fail on an unrelated bad setting.
func resolveServerPort() (int, error) {
	v, err := config.Resolve()
	if err != nil {
		return 0, err
	}

	// UnmarshalKey applies the same decoding as the server's Unmarshal, so a
	// value the server would reject is rejected here too rather than read as 0.
	var port int
	if err := v.UnmarshalKey("server.port", &port); err != nil {
		return 0, fmt.Errorf("invalid server.port: %w", err)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("server.port %d is outside 1-65535", port)
	}
	return port, nil
}

// formatHealthStatus returns a human-readable one-line summary from the
// heartbeat JSON response. Falls back to the raw body on parse failure.
func formatHealthStatus(body []byte) string {
	var resp struct {
		Status string `json:"status"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Status != "" {
		return fmt.Sprintf("%s: %s", resp.Name, resp.Status)
	}
	return string(body)
}
