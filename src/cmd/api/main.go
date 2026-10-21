package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/pflag"
	"github.com/twistingmercury/librarian/internal/api/config"
	"github.com/twistingmercury/librarian/internal/api/server"
	"github.com/twistingmercury/librarian/internal/api/version"
)

// @title Mnemonic API
// @version 1.0
// @description REST API for the Mnemonic agent-pattern-skill management service
// @host localhost:8080
// @BasePath /v1/api
// @schemes http
func main() {
	flags, err := parseFlags(os.Args[1:])
	// pflag has already printed usage for --help, but with ContinueOnError it
	// leaves reporting every other parse error to the caller.
	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if showVersion, _ := flags.GetBool("version"); showVersion {
		fmt.Println(version.Print())
		os.Exit(0)
	}

	if checkOnly, _ := flags.GetBool("health"); checkOnly {
		exitCode := checkHealth(flags)
		os.Exit(exitCode)
	}

	cfg, err := config.LoadWithFlags(flags)

	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	if err := server.ListenAndServe(cfg); err != nil {
		log.Fatalf("exited with err: %s\n", err.Error()) // #nosec G706 -- taint is the process's own argv (--config), set by the operator who launched it
	}
}

// parseFlags builds a fresh flag set instead of using pflag.CommandLine so the
// entry point holds no global flag state and can be parsed repeatedly in tests.
// ContinueOnError returns parse failures to the caller rather than exiting.
func parseFlags(args []string) (*pflag.FlagSet, error) {
	flags := pflag.NewFlagSet("mnemonic-api", pflag.ContinueOnError)
	flags.Bool("version", false, "Displays current version information for mnemonic")
	flags.Bool("health", false, "Get the current health of the service")
	flags.String("config", "", "Path to the configuration file; alternatively set "+config.EnvConfigFile)

	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	return flags, nil
}

// healthCheckTimeout is the HTTP client timeout for the CLI health probe.
// Kept short because Docker healthcheck has its own outer timeout.
const healthCheckTimeout = 3 * time.Second

// checkHealth makes an HTTP GET request to the running server's /health
// endpoint and reports the result. It is designed for use as a Docker
// HEALTHCHECK command in scratch/static containers where curl is unavailable.
// The target is plain HTTP on localhost because TLS terminates at the reverse
// proxy, not in this process.
func checkHealth(flags *pflag.FlagSet) (exitCode int) {
	port, err := resolveServerPort(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		return 1
	}
	url := fmt.Sprintf("http://localhost:%d/health", port)

	client := &http.Client{Timeout: healthCheckTimeout}

	resp, err := client.Get(url) // #nosec G107 G704 -- host is literal localhost, port is range-checked, path is constant
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
// without full validation: the probe must not need database, OpenAI or queue
// credentials, nor fail on an unrelated bad setting.
func resolveServerPort(flags *pflag.FlagSet) (int, error) {
	v, err := config.Resolve(flags)
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
