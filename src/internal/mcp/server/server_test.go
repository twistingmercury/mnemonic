package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/librarian-mcp/internal/config"
)

// listening returns a server bound to a free local port and its listen call.
func listening(t *testing.T, name string) namedServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Addr: ln.Addr().String(), Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	return namedServer{name, srv, func() error { return srv.Serve(ln) }}
}

var serveCfg = &config.MnemonicConfig{Server: config.ServerConfig{ShutdownTimeout: time.Second}}

// TestServe_ListenerFailureStopsTheOthers verifies that one server failing to
// listen shuts the others down and is reported.
func TestServe_ListenerFailureStopsTheOthers(t *testing.T) {
	broken := namedServer{"broken", &http.Server{ReadHeaderTimeout: time.Second}, func() error { return errors.New("bind failed") }}

	err := serve(context.Background(), serveCfg, zerolog.Nop(), []namedServer{listening(t, "ok"), broken})
	assert.EqualError(t, err, "broken server error: bind failed")
}

// TestServe_CancelShutsDownAll verifies that cancelling the context shuts
// every server down cleanly.
func TestServe_CancelShutsDownAll(t *testing.T) {
	servers := []namedServer{listening(t, "a"), listening(t, "b")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, serveCfg, zerolog.Nop(), servers) }()
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after cancel")
	}
}
