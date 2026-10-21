package patterns_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian-api/internal/config"
	"github.com/twistingmercury/librarian-api/internal/handlers"
	"github.com/twistingmercury/librarian-api/internal/handlers/patterns"
	chunkrepo "github.com/twistingmercury/librarian-api/internal/repository/chunk"
	patternrepo "github.com/twistingmercury/librarian-api/internal/repository/pattern"
	"github.com/twistingmercury/librarian-api/internal/service"
	patternsvc "github.com/twistingmercury/librarian-api/internal/service/pattern"
	searchsvc "github.com/twistingmercury/librarian-api/internal/service/search"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- Mock PatternService ---

type mockPatternService struct {
	mock.Mock
}

func (m *mockPatternService) Create(ctx context.Context, input patternsvc.Input) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternService) Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternService) GetWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error) {
	args := m.Called(ctx, id)
	var p *patternrepo.Pattern
	var g *patternsvc.GraphContext
	if args.Get(0) != nil {
		p = args.Get(0).(*patternrepo.Pattern)
	}
	if args.Get(1) != nil {
		g = args.Get(1).(*patternsvc.GraphContext)
	}
	return p, g, args.Error(2)
}

func (m *mockPatternService) Update(ctx context.Context, id uuid.UUID, input patternsvc.Input) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternService) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockPatternService) List(ctx context.Context, filter patternrepo.Filter, opts patternsvc.ListOptions) ([]*patternrepo.Pattern, error) {
	args := m.Called(ctx, filter, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternService) ListChunks(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*chunkrepo.Chunk), args.Error(1)
}

// --- Mock SearchService ---

type mockSearchService struct {
	mock.Mock
}

func (m *mockSearchService) SearchPatterns(ctx context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*searchsvc.SearchResult), args.Error(1)
}

// --- Helpers ---

// testVocab contains the canonical vocabulary used across tests.
var testVocab = config.VocabularyConfig{
	Languages: []string{"agnostic", "go", "python", "csharp", "shell", "typescript", "javascript", "sql", "cypher"},
	Domains:   []string{"api-design", "backend", "frontend", "testing", "devops", "cli", "data-design", "documentation"},
}

func newTestRouter(psvc patternsvc.Service, ssvc searchsvc.Service) *gin.Engine {
	router := gin.New()
	h := patterns.New(psvc, ssvc, testVocab)
	v1 := router.Group("/v1/api")
	h.RegisterRoutes(v1)
	return router
}

func makePattern(name string) *patternrepo.Pattern {
	desc := "Test pattern description"
	return &patternrepo.Pattern{
		ID:               uuid.New(),
		Name:             name,
		Description:      &desc,
		Content:          "# Test Pattern\n\nContent here",
		Tags:             []string{"go", "test"},
		EntityType:       "go-pattern",
		Language:         "go",
		Domain:           "backend",
		RelatedPatterns:  []string{},
		EnrichmentStatus: "pending",
		CreatedAt:        time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC),
	}
}

// --- Tests ---

func TestPatternCreate_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	psvc.On("Create", mock.Anything, mock.AnythingOfType("pattern.Input")).Return(pattern, nil)

	body := `{
		"name": "go-error-handling",
		"description": "Test pattern description",
		"content": "# Test Pattern\n\nContent here",
		"tags": ["go", "test"],
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "/v1/api/patterns/")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "go-error-handling", resp["name"])
	assert.Equal(t, "pending", resp["enrichment_status"])
}

func TestPatternCreate_Conflict(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	psvc.On("Create", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("%w: pattern %q", service.ErrConflict, "go-error-handling"))

	body := `{
		"name": "go-error-handling",
		"content": "content",
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestPatternCreate_BadRequest(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	// Missing required content field.
	body := `{"name": "test"}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPatternWrite_InvalidFields checks that create and update reject an
// invalid field with a 400 naming the field and the failed rule.
func TestPatternWrite_InvalidFields(t *testing.T) {
	t.Parallel()

	const createPath = "/v1/api/patterns"
	updatePath := "/v1/api/patterns/" + uuid.New().String()
	body := func(entityType, language, domain string) string {
		return fmt.Sprintf(`{
			"name": "go-error-handling",
			"content": "# Test Pattern\n\nContent here",
			"entity_type": %q,
			"language": %q,
			"domain": %q
		}`, entityType, language, domain)
	}

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		wantField string
		wantCode  string
	}{
		{"create entity_type not kebab-case", http.MethodPost, createPath, body("Invalid Type", "go", "backend"), "entity_type", "INVALID_FORMAT"},
		{"create language not kebab-case", http.MethodPost, createPath, body("go-pattern", "INVALID_LANG", "backend"), "language", "INVALID_FORMAT"},
		{"create language not allowed", http.MethodPost, createPath, body("go-pattern", "brainfuck", "backend"), "language", "INVALID_VALUE"},
		{"create domain not allowed", http.MethodPost, createPath, body("go-pattern", "go", "not-a-valid-domain"), "domain", "INVALID_VALUE"},
		{"update language not allowed", http.MethodPut, updatePath, body("go-pattern", "brainfuck", "backend"), "language", "INVALID_VALUE"},
		{"update domain not allowed", http.MethodPut, updatePath, body("go-pattern", "go", "not-a-valid-domain"), "domain", "INVALID_VALUE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// No service expectations: an invalid body must never reach it.
			router := newTestRouter(new(mockPatternService), new(mockSearchService))

			w := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			var resp handlers.ProblemDetail
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			require.NotEmpty(t, resp.Errors)
			assert.Equal(t, tt.wantField, resp.Errors[0].Field)
			assert.Equal(t, tt.wantCode, resp.Errors[0].Code)
		})
	}
}

func TestPatternGet_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	psvc.On("GetWithGraph", mock.Anything, pattern.ID).Return(pattern, (*patternsvc.GraphContext)(nil), nil)
	psvc.On("ListChunks", mock.Anything, pattern.ID).
		Return([]*chunkrepo.Chunk{}, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+pattern.ID.String(), nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "go-error-handling", resp["name"])
}

func TestPatternGet_InvalidUUID(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/not-a-uuid", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPatternGet_NotFound(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("GetWithGraph", mock.Anything, id).
		Return(nil, nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+id.String(), nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPatternList_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	p1 := makePattern("pattern-a")
	p2 := makePattern("pattern-b")
	psvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Return([]*patternrepo.Pattern{p1, p2}, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns?limit=20", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]any)
	assert.Len(t, data, 2)
}

func TestPatternUpdate_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	psvc.On("Update", mock.Anything, pattern.ID, mock.Anything).Return(pattern, nil)

	body := `{
		"name": "go-error-handling",
		"content": "Updated content",
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/api/patterns/"+pattern.ID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.Bytes())

	psvc.AssertExpectations(t)
}

func TestPatternDelete_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("Delete", mock.Anything, id).Return(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/patterns/"+id.String(), nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestPatternDelete_NotFound(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("Delete", mock.Anything, id).
		Return(fmt.Errorf("%w: pattern %s", service.ErrNotFound, id))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/patterns/"+id.String(), nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSearch_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	ssvc.On("SearchPatterns", mock.Anything, mock.AnythingOfType("search.SearchOptions")).
		Return(&searchsvc.SearchResult{
			Matches: []*searchsvc.ChunkMatch{
				{
					PatternID:    uuid.New(),
					PatternName:  "go-error-handling",
					EntityType:   "go-pattern",
					Language:     "go",
					Domain:       "backend",
					Content:      "# Test Pattern\n\nContent here",
					Tags:         []string{"go", "test"},
					SectionTitle: "Philosophy",
					ChunkIndex:   0,
					Similarity:   0.92,
				},
			},
			Query:            "error handling",
			TotalCandidates:  47,
			SearchDurationMs: 23,
		}, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search?q=error+handling", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	results := resp["results"].([]any)
	assert.Len(t, results, 1)

	first := results[0].(map[string]any)
	assert.Equal(t, "go-error-handling", first["pattern_name"])
	assert.Equal(t, "go-pattern", first["entity_type"])
	assert.Equal(t, "go", first["language"])
	assert.Equal(t, "backend", first["domain"])

	metadata := resp["metadata"].(map[string]any)
	assert.Equal(t, "error handling", metadata["query"])
}

func TestSearch_MissingQuery(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSearch_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	ssvc.On("SearchPatterns", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("%w: embedding service down", service.ErrServiceUnavailable))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search?q=test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestGetChunks_NotFoundPattern(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("Get", mock.Anything, id).
		Return(nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+id.String()+"/chunks", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetChunks_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	chunks := []*chunkrepo.Chunk{
		{ChunkIndex: 0, SectionTitle: "Overview", EnrichmentStatus: "pending"},
		{ChunkIndex: 1, SectionTitle: "Details", EnrichmentStatus: "enriched"},
	}
	psvc.On("Get", mock.Anything, pattern.ID).Return(pattern, nil)
	psvc.On("ListChunks", mock.Anything, pattern.ID).Return(chunks, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+pattern.ID.String()+"/chunks", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.InDelta(t, float64(2), resp["count"], 0.001)
	chunkList := resp["chunks"].([]any)
	require.Len(t, chunkList, 2)
	first := chunkList[0].(map[string]any)
	assert.Equal(t, "Overview", first["section_title"])
}

func TestPatternGet_ChunksPopulated(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	chunks := []*chunkrepo.Chunk{
		{ChunkIndex: 0, SectionTitle: "Overview", EnrichmentStatus: "pending"},
		{ChunkIndex: 1, SectionTitle: "Philosophy", EnrichmentStatus: "enriched"},
	}
	psvc.On("GetWithGraph", mock.Anything, pattern.ID).Return(pattern, (*patternsvc.GraphContext)(nil), nil)
	psvc.On("ListChunks", mock.Anything, pattern.ID).Return(chunks, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+pattern.ID.String(), nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "go-error-handling", resp["name"])

	chunkList, ok := resp["chunks"].([]any)
	require.True(t, ok, "expected chunks to be an array")
	require.Len(t, chunkList, 2)

	first := chunkList[0].(map[string]any)
	assert.Equal(t, "Overview", first["section_title"])
	assert.InDelta(t, float64(0), first["chunk_index"], 0.001)
	assert.Equal(t, "pending", first["enrichment_status"])

	second := chunkList[1].(map[string]any)
	assert.Equal(t, "Philosophy", second["section_title"])
	assert.Equal(t, "enriched", second["enrichment_status"])

	psvc.AssertExpectations(t)
}
