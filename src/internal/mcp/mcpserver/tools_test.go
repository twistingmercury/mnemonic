package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/twistingmercury/librarian/internal/mcp/config"
	"github.com/twistingmercury/librarian/internal/mcp/metrics"
	patternsvc "github.com/twistingmercury/librarian/internal/mcp/service/pattern"
	searchsvc "github.com/twistingmercury/librarian/internal/mcp/service/search"
)

// connectTools serves svc over in-memory transports, with a configured search
// threshold distinct from the 0.5 in the tool description, and returns the
// client session.
func connectTools(t *testing.T, svc Services, toolMetrics *metrics.Tools) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := NewMCPServer(svc, zerolog.Nop(), config.MCPConfig{DefaultSearchThreshold: 0.6}, toolMetrics)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestToolSchemaDefaults verifies that the SDK fills omitted limits and the
// threshold from the input schema, the threshold from config, and keeps an
// explicit zero threshold.
func TestToolSchemaDefaults(t *testing.T) {
	var gotSearch []searchsvc.SearchOptions
	var gotLimit int
	session := connectTools(t, Services{
		SearchPatterns: func(_ context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
			gotSearch = append(gotSearch, opts)
			return &searchsvc.SearchResult{}, nil
		},
		FindRelatedPatterns: func(_ context.Context, _ uuid.UUID, limit int) (string, []patternsvc.RelatedPatternResult, error) {
			gotLimit = limit
			return "source", nil, nil
		},
	}, noopToolMetrics(t))
	ctx := context.Background()

	for _, args := range []map[string]any{
		{"query": "q"},
		{"query": "q", "limit": 50, "threshold": 0},
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_patterns", Arguments: args})
		require.NoError(t, err)
		require.False(t, res.IsError, "%v", res.Content)
	}
	assert.Equal(t, []searchsvc.SearchOptions{
		{Query: "q", Limit: defaultSearchLimit, Threshold: 0.6},
		{Query: "q", Limit: maxSearchLimit, Threshold: 0},
	}, gotSearch)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "find_related_patterns", Arguments: map[string]any{"pattern_id": uuid.NewString()}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	assert.Equal(t, defaultRelatedLimit, gotLimit)
}

// TestToolSchemaBounds verifies that the SDK rejects out-of-range limits and
// thresholds before the service is called, and that the calls are counted as
// invalid input.
func TestToolSchemaBounds(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	toolMetrics, err := metrics.NewTools(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	session := connectTools(t, Services{
		SearchPatterns: func(context.Context, searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
			t.Error("search_patterns reached the service")
			return nil, nil
		},
		FindRelatedPatterns: func(context.Context, uuid.UUID, int) (string, []patternsvc.RelatedPatternResult, error) {
			t.Error("find_related_patterns reached the service")
			return "", nil, nil
		},
	}, toolMetrics)

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"search_patterns", map[string]any{"query": "q", "limit": 0}},
		{"search_patterns", map[string]any{"query": "q", "limit": 51}},
		{"search_patterns", map[string]any{"query": "q", "threshold": -0.1}},
		{"search_patterns", map[string]any{"query": "q", "threshold": 1.5}},
		{"find_related_patterns", map[string]any{"pattern_id": uuid.NewString(), "limit": 0}},
		{"find_related_patterns", map[string]any{"pattern_id": uuid.NewString(), "limit": 21}},
	} {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		require.NoError(t, err)
		require.True(t, res.IsError, "%s %v", tc.tool, tc.args)
		require.NotEmpty(t, res.Content)
		text := res.Content[0].(*mcp.TextContent).Text
		assert.Contains(t, text, sdkArgumentsErrorPrefix, "%s %v", tc.tool, tc.args)
	}

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "mnemonic.mcp.tool_calls" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				tool, _ := dp.Attributes.Value("tool")
				outcome, _ := dp.Attributes.Value("outcome")
				got[tool.AsString()+"/"+outcome.AsString()] = dp.Value
			}
		}
	}
	assert.Equal(t, map[string]int64{
		"search_patterns/" + outcomeInvalid:       4,
		"find_related_patterns/" + outcomeInvalid: 2,
	}, got)
}

// TestAdvertisedToolsHaveDescriptions verifies that every tool in tools/list
// carries a description, since agents pick tools by it. It asserts on whatever
// the server advertises, so a new tool without one fails here.
func TestAdvertisedToolsHaveDescriptions(t *testing.T) {
	session := connectTools(t, Services{}, noopToolMetrics(t))

	res, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, res.Tools, "server advertises no tools")

	for _, tool := range res.Tools {
		assert.NotEmpty(t, strings.TrimSpace(tool.Description), "tool %q has no description", tool.Name)
	}
}
