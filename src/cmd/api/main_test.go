package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/api/config"
)

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

// healthServer serves a /health endpoint on a local port and counts every
// request it receives, so failure tests can prove the probe never dialled it.
func healthServer(t *testing.T, status int) (string, *atomic.Int32) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"status": http.StatusText(status), "name": "mnemonic"})
	require.NoError(t, err)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, "/health", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return extractPort(t, srv.URL), &hits
}

// probeFlags parses args the way main does for a --health invocation.
func probeFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	flags, err := parseFlags(append([]string{"--health"}, args...))
	require.NoError(t, err)
	return flags
}

func TestResolveServerPort_DefaultWithoutConfigFile(t *testing.T) {
	isolateLoaderSources(t)

	port, err := resolveServerPort(probeFlags(t))
	require.NoError(t, err)
	assert.Equal(t, config.DefaultServerPort, port)
}

func TestCheckHealth_Healthy(t *testing.T) {
	isolateLoaderSources(t)
	port, hits := healthServer(t, http.StatusOK)
	t.Setenv("MNEMONIC_SERVER_PORT", port)

	assert.Equal(t, 0, checkHealth(probeFlags(t)))
	assert.Equal(t, int32(1), hits.Load())
}

func TestCheckHealth_Unhealthy(t *testing.T) {
	isolateLoaderSources(t)
	port, hits := healthServer(t, http.StatusServiceUnavailable)
	t.Setenv("MNEMONIC_SERVER_PORT", port)

	assert.Equal(t, 1, checkHealth(probeFlags(t)))
	assert.Equal(t, int32(1), hits.Load())
}

func TestCheckHealth_Unreachable(t *testing.T) {
	isolateLoaderSources(t)
	// Nothing listens on port 1.
	t.Setenv("MNEMONIC_SERVER_PORT", "1")

	assert.Equal(t, 1, checkHealth(probeFlags(t)))
}

// The Docker HEALTHCHECK must probe the port the server listens on, and the
// server takes it from whichever config file it selected. A port that lives
// only in that file must therefore reach the probe by every selection route.
func TestCheckHealth_PortFromSelectedConfigFile(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		choose func(t *testing.T, path string) []string
	}{
		{"--config flag", "selected.yaml", func(_ *testing.T, path string) []string {
			return []string{"--config", path}
		}},
		{config.EnvConfigFile, "selected.yaml", func(t *testing.T, path string) []string {
			t.Setenv(config.EnvConfigFile, path)
			return nil
		}},
		{"discovered " + config.DevelopmentConfigPath, config.DevelopmentConfigPath, func(*testing.T, string) []string {
			return nil
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := isolateLoaderSources(t)
			port, hits := healthServer(t, http.StatusOK)
			path := writePortConfig(t, dir, tt.file, port)

			assert.Equal(t, 0, checkHealth(probeFlags(t, tt.choose(t, path)...)))
			assert.Equal(t, int32(1), hits.Load())
		})
	}
}

// Proves main hands its parsed flag set to the probe, not just to the loader:
// run end to end, a port known only to the --config file is still reached.
func TestMain_HealthUsesConfigFlagFile(t *testing.T) {
	dir := isolateLoaderSources(t)
	port, hits := healthServer(t, http.StatusOK)
	path := writePortConfig(t, dir, "flag.yaml", port)

	code, stderr := runMain(t, "--health", "--config", path)

	assert.Equal(t, 0, code, "stderr:\n%s", stderr)
	assert.Equal(t, int32(1), hits.Load())
}

// Environment sits above the file for the server, so it must for the probe.
// The file's port points at a second live server so a probe that ignored the
// environment would still succeed, just against the wrong one.
func TestCheckHealth_EnvPortOverridesFilePort(t *testing.T) {
	dir := isolateLoaderSources(t)
	envPort, envHits := healthServer(t, http.StatusOK)
	filePort, fileHits := healthServer(t, http.StatusOK)
	path := writePortConfig(t, dir, "flag.yaml", filePort)
	t.Setenv("MNEMONIC_SERVER_PORT", envPort)

	assert.Equal(t, 0, checkHealth(probeFlags(t, "--config", path)))
	assert.Equal(t, int32(1), envHits.Load())
	assert.Zero(t, fileHits.Load())
}

// The probe needs only server.port. Missing credentials and an invalid,
// unrelated setting make full loading fail, but must not stop the probe.
func TestCheckHealth_SkipsUnrelatedValidation(t *testing.T) {
	dir := isolateLoaderSources(t)
	port, hits := healthServer(t, http.StatusOK)
	path := filepath.Join(dir, "flag.yaml")
	content := "server:\n  port: " + port + "\nlogging:\n  level: nonsense\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	flags := probeFlags(t, "--config", path)

	_, err := config.LoadWithFlags(flags)
	require.Error(t, err, "precondition: this config must fail full validation")

	assert.Equal(t, 0, checkHealth(flags))
	assert.Equal(t, int32(1), hits.Load())
}

// A config file the server could not read means the probe cannot know the
// real port; it must fail rather than fall back to defaults or environment.
// The environment names a live server so a fallback would be observable.
func TestMain_HealthUnreadableConfigFileFailsWithoutRequest(t *testing.T) {
	dir := isolateLoaderSources(t)
	port, hits := healthServer(t, http.StatusOK)
	t.Setenv("MNEMONIC_SERVER_PORT", port)
	missing := filepath.Join(dir, "missing.yaml")

	code, stderr := runMain(t, "--health", "--config", missing)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "failed to read config file "+missing)
	assert.Zero(t, hits.Load())
}

// An unusable port is a configuration error reported as such, not a request
// to localhost:0 or to a port the kernel would refuse.
func TestMain_HealthInvalidPortFailsWithoutRequest(t *testing.T) {
	tests := []struct {
		port    string
		wantErr string
	}{
		{"not_a_number", "invalid server.port"},
		{"0", "server.port 0 is outside 1-65535"},
		{"-1", "server.port -1 is outside 1-65535"},
		{"65536", "server.port 65536 is outside 1-65535"},
	}

	for _, tt := range tests {
		t.Run(tt.port, func(t *testing.T) {
			dir := isolateLoaderSources(t)
			filePort, hits := healthServer(t, http.StatusOK)
			path := writePortConfig(t, dir, "flag.yaml", filePort)
			t.Setenv("MNEMONIC_SERVER_PORT", tt.port)

			code, stderr := runMain(t, "--health", "--config", path)

			assert.Equal(t, 1, code)
			assert.Contains(t, stderr, tt.wantErr)
			assert.Zero(t, hits.Load())
		})
	}
}

// extractPort returns the port of an httptest server URL.
func extractPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u.Port()
}

// isolateLoaderSources clears every source LoadWithFlags consults besides the
// flag set, so only the test's own file and variables can influence it.
// /etc/mnemonic/config.yaml cannot be redirected, so its presence skips the
// test rather than letting a host file decide the result.
func isolateLoaderSources(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(config.ProductionConfigPath); err == nil {
		t.Skipf("%s exists and would be discovered", config.ProductionConfigPath)
	}
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, config.EnvPrefix+"_") {
			t.Setenv(name, "")
		}
	}
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func writePortConfig(t *testing.T, dir, name, port string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "server:\n  port: " + port + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// The --config flag must reach the loader; without it the file source with
// the highest precedence is unreachable from the command line.
func TestParseFlags_ConfigFlagSelectsFile(t *testing.T) {
	dir := isolateLoaderSources(t)
	path := writePortConfig(t, dir, "flag.yaml", "18181")

	flags, err := parseFlags([]string{"--config", path})
	require.NoError(t, err)

	configFlag := flags.Lookup("config")
	require.NotNil(t, configFlag)
	assert.True(t, configFlag.Changed)
	assert.Equal(t, path, configFlag.Value.String())

	cfg, err := config.LoadWithFlags(flags)
	require.NoError(t, err)
	assert.Equal(t, 18181, cfg.Server.Port)
}

// A parse failure must come back as an error so main controls the exit code;
// ExitOnError would terminate this test binary instead.
func TestParseFlags_UnknownFlagReturnsError(t *testing.T) {
	t.Parallel()

	_, err := parseFlags([]string{"--no-such-flag"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-flag")
}

func TestParseFlags_BoolFlags(t *testing.T) {
	t.Parallel()

	flags, err := parseFlags([]string{"--version", "--health"})
	require.NoError(t, err)

	showVersion, err := flags.GetBool("version")
	require.NoError(t, err)
	assert.True(t, showVersion)

	checkOnly, err := flags.GetBool("health")
	require.NoError(t, err)
	assert.True(t, checkOnly)
}

func TestParseFlags_NoArgsLeavesFlagsUnset(t *testing.T) {
	t.Parallel()

	flags, err := parseFlags(nil)
	require.NoError(t, err)

	for _, name := range []string{"version", "health", "config"} {
		f := flags.Lookup(name)
		require.NotNil(t, f, "flag %q must be defined", name)
		assert.False(t, f.Changed, "flag %q", name)
		assert.Equal(t, f.DefValue, f.Value.String(), "flag %q", name)
	}
}

// Registering on pflag.CommandLine would make the entry point stateful: a
// second parse panics on redefinition and flag values leak between callers.
func TestParseFlags_LeavesCommandLineUntouched(t *testing.T) {
	_, err := parseFlags([]string{"--config", "x.yaml", "--version", "--health"})
	require.NoError(t, err)
	_, err = parseFlags(nil)
	require.NoError(t, err)

	for _, name := range []string{"version", "health", "config"} {
		assert.Nil(t, pflag.CommandLine.Lookup(name), "flag %q registered on pflag.CommandLine", name)
	}
	assert.False(t, pflag.CommandLine.Parsed())
}

// mainArgsEnv carries the child's arguments; it deliberately avoids the
// MNEMONIC_ prefix so it can never be mistaken for configuration.
const mainArgsEnv = "GO_TEST_MAIN_ARGS"

// TestMainProcess is not a test: runMain re-executes the test binary into it
// so main, which calls os.Exit, runs in a child process.
func TestMainProcess(t *testing.T) {
	raw, ok := os.LookupEnv(mainArgsEnv)
	if !ok {
		t.Skip("helper process for runMain")
	}
	os.Args = append([]string{"mnemonic-api"}, strings.Split(raw, "\x1f")...)
	main()
}

// runMain runs main with args in a child process that inherits the test's
// environment and working directory, and returns its exit code and stderr.
func runMain(t *testing.T, args ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainProcess$") // #nosec G204 -- re-executes this test binary
	cmd.Env = append(os.Environ(), mainArgsEnv+"="+strings.Join(args, "\x1f"))
	var stderr strings.Builder
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, ctx.Err(), "main did not exit; stderr:\n%s", stderr.String())
	if err == nil {
		return 0, stderr.String()
	}
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "running main: %v", err)
	return exitErr.ExitCode(), stderr.String()
}

// Proves main hands its parsed flag set to the loader: an explicit --config
// path must be read, so a missing one fails configuration loading. If main
// ignored the flag, loading would succeed from defaults and fail later (or
// not at all) with a different error.
func TestMain_ConfigFlagReachesLoader(t *testing.T) {
	dir := isolateLoaderSources(t)
	missing := filepath.Join(dir, "missing.yaml")

	code, stderr := runMain(t, "--config", missing)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "failed to load configuration")
	assert.Contains(t, stderr, "failed to read config file "+missing)
}

func TestMain_UnknownFlagExitsTwo(t *testing.T) {
	isolateLoaderSources(t)

	code, stderr := runMain(t, "--no-such-flag")

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "no-such-flag")
}

func TestMain_HelpExitsZeroAndDocumentsConfig(t *testing.T) {
	isolateLoaderSources(t)

	code, stderr := runMain(t, "--help")

	assert.Equal(t, 0, code)
	assert.Contains(t, stderr, "--config")
	assert.Contains(t, stderr, config.EnvConfigFile)
}
