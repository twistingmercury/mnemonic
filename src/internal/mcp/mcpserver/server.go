package mcpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/librarian-mcp/internal/config"
	"github.com/twistingmercury/librarian-mcp/internal/metrics"
	patternrepo "github.com/twistingmercury/librarian-mcp/internal/repository/pattern"
	patternsvc "github.com/twistingmercury/librarian-mcp/internal/service/pattern"
	searchsvc "github.com/twistingmercury/librarian-mcp/internal/service/search"
	"github.com/twistingmercury/librarian-mcp/internal/version"
)

// toolListTTL is the cache hint sent on tools/list and server/discover. The
// tool set only changes with a new release, so clients may cache it.
const toolListTTL = time.Hour

// Services are the service calls behind the three tools. Production passes
// the search and pattern services' methods; tests pass closures.
type Services struct {
	SearchPatterns      func(ctx context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error)
	FindRelatedPatterns func(ctx context.Context, patternID uuid.UUID, limit int) (string, []patternsvc.RelatedPatternResult, error)
	GetPatternWithGraph func(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error)
}

// NewMCPServer creates a configured MCP server with all 3 pattern search
// tools registered. The returned server is ready to be wrapped in an HTTP
// handler via NewMCPHTTPHandler. Every tool call is traced, counted and
// timed.
func NewMCPServer(svc Services, logger zerolog.Logger, mcpCfg config.MCPConfig, toolMetrics *metrics.Tools) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mnemonic",
		Version: version.Version(),
	}, &mcp.ServerOptions{
		SchemaCache: mcp.NewSchemaCache(),
		// Tools only: the deprecated logging capability is unused, and the
		// tool list is fixed, so there are no list_changed notifications.
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
		SetCacheable: func(_ context.Context, _ mcp.Request, c *mcp.Cacheable) {
			c.TTLMs = int(toolListTTL.Milliseconds())
			c.CacheScope = "public"
		},
	})

	server.AddReceivingMiddleware(toolTelemetry(toolMetrics))
	mcp.AddTool(server, searchPatternsTool(mcpCfg.DefaultSearchThreshold), handleSearchPatterns(svc.SearchPatterns, logger))
	mcp.AddTool(server, findRelatedPatternsTool(), handleFindRelatedPatterns(svc.FindRelatedPatterns, logger))
	mcp.AddTool(server, getPatternTool, handleGetPattern(svc.GetPatternWithGraph, logger))

	return server
}

// NewMCPHTTPHandler creates a StreamableHTTPHandler wrapping the MCP server.
// The handler implements http.Handler and serves JSON-RPC 2.0 over HTTP POST.
// Stateless mode is enabled because all tools are stateless.
func NewMCPHTTPHandler(server *mcp.Server) *mcp.StreamableHTTPHandler {
	return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
}

// NewMCPHTTPServer creates the net/http.Server for the MCP endpoint. Requests
// get an otelhttp server span (continuing any incoming W3C trace context),
// tagged with the MCP method and protocol version headers, and the standard
// http.server.* metrics. Cross-origin browser requests are rejected with 403,
// as the spec requires servers to validate Origin.
// The caller is responsible for starting and stopping the server
// (typically via errgroup).
func NewMCPHTTPServer(mcpCfg config.MCPConfig, host string, mcpHandler http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/mcp", otelhttp.NewHandler(withMCPHeaders(http.NewCrossOriginProtection().Handler(mcpHandler)), "/mcp"))

	return &http.Server{
		Addr:         mcpCfg.Address(host),
		Handler:      mux,
		ReadTimeout:  mcpCfg.ReadTimeout,
		WriteTimeout: mcpCfg.WriteTimeout,
		IdleTimeout:  mcpCfg.IdleTimeout,
	}
}

// withMCPHeaders copies the Mcp-Method and MCP-Protocol-Version request
// headers onto the active HTTP server span.
func withMCPHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if m := r.Header.Get("Mcp-Method"); m != "" {
			span.SetAttributes(attribute.String("mcp.method.name", m))
		}
		if v := r.Header.Get("MCP-Protocol-Version"); v != "" {
			span.SetAttributes(attribute.String("mcp.protocol.version", v))
		}
		next.ServeHTTP(w, r)
	})
}
