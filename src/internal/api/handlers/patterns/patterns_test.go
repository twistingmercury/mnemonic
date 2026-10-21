package patterns_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/api/config"
	"github.com/twistingmercury/mnemonic/internal/api/handlers"
	"github.com/twistingmercury/mnemonic/internal/api/handlers/patterns"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/api/repository/chunk"
	patternrepo "github.com/twistingmercury/mnemonic/internal/api/repository/pattern"
	"github.com/twistingmercury/mnemonic/internal/api/service"
	patternsvc "github.com/twistingmercury/mnemonic/internal/api/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/api/service/search"
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

// --- Validation helpers ---

// fieldErr is the field and code pair of one validation error.
type fieldErr struct {
	field string
	code  string
}

// validBody returns a create/update payload that passes validation, with the
// overrides applied and the omitted keys removed.
func validBody(t *testing.T, overrides map[string]any, omit ...string) string {
	t.Helper()

	fields := map[string]any{
		"name":        "go-error-handling",
		"description": "Test pattern description",
		"content":     "# Test Pattern\n\nContent here",
		"tags":        []string{"go", "test"},
		"entity_type": "go-pattern",
		"language":    "go",
		"domain":      "backend",
	}
	maps.Copy(fields, overrides)
	for _, key := range omit {
		delete(fields, key)
	}

	raw, err := json.Marshal(fields)
	require.NoError(t, err)

	return string(raw)
}

// doRequest sends one request through the router and returns the recorded response.
func doRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	return w
}

// problemWant is the RFC 7807 envelope a handler is expected to produce.
type problemWant struct {
	status int
	slug   string
	title  string
	path   string
}

// decodeProblem checks the RFC 7807 envelope and returns the decoded body.
func decodeProblem(t *testing.T, w *httptest.ResponseRecorder, want problemWant) handlers.ProblemDetail {
	t.Helper()

	require.Equal(t, want.status, w.Code)

	var resp handlers.ProblemDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, handlers.ProblemBaseURI+want.slug, resp.Type)
	assert.Equal(t, want.title, resp.Title)
	assert.Equal(t, want.status, resp.Status)
	assert.Equal(t, want.path, resp.Instance)

	return resp
}

// decodeValidationProblem checks for a 400 validation problem on path and
// returns the field/code pairs it reports, in order.
func decodeValidationProblem(t *testing.T, w *httptest.ResponseRecorder, path string) []fieldErr {
	t.Helper()

	resp := decodeProblem(t, w, problemWant{http.StatusBadRequest, "validation-error", "Validation Error", path})
	assert.NotEmpty(t, resp.Detail)

	var got []fieldErr
	for _, e := range resp.Errors {
		got = append(got, fieldErr{e.Field, e.Code})
	}

	return got
}

// TestPatternWrite_ValidationRejects checks that create and update answer 400
// with a problem body naming every violated field, and never reach the service.
func TestPatternWrite_ValidationRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []fieldErr
	}{
		{"name missing", validBody(t, nil, "name"), []fieldErr{{"name", "REQUIRED"}}},
		{"name 129 chars", validBody(t, map[string]any{"name": "a" + strings.Repeat("b", 128)}), []fieldErr{{"name", "MAX_LENGTH"}}},
		{"name uppercase", validBody(t, map[string]any{"name": "Go-Pattern"}), []fieldErr{{"name", "INVALID_FORMAT"}}},
		{"name leading digit", validBody(t, map[string]any{"name": "1go-pattern"}), []fieldErr{{"name", "INVALID_FORMAT"}}},
		{"name underscore", validBody(t, map[string]any{"name": "go_pattern"}), []fieldErr{{"name", "INVALID_FORMAT"}}},
		{"content missing", validBody(t, nil, "content"), []fieldErr{{"content", "REQUIRED"}}},
		{"content 100001 bytes", validBody(t, map[string]any{"content": strings.Repeat("x", 100_001)}), []fieldErr{{"content", "TOO_LARGE"}}},
		{"description 501 chars", validBody(t, map[string]any{"description": strings.Repeat("d", 501)}), []fieldErr{{"description", "MAX_LENGTH"}}},
		{"tags 21 items", validBody(t, map[string]any{"tags": make([]string, 21)}), []fieldErr{{"tags", "MAX_ITEMS"}}},
		{"entity_type missing", validBody(t, nil, "entity_type"), []fieldErr{{"entity_type", "REQUIRED"}}},
		{"language missing", validBody(t, nil, "language"), []fieldErr{{"language", "REQUIRED"}}},
		{"domain missing", validBody(t, nil, "domain"), []fieldErr{{"domain", "REQUIRED"}}},
		{"domain not kebab-case", validBody(t, map[string]any{"domain": "Not A Domain"}), []fieldErr{{"domain", "INVALID_FORMAT"}}},
		{"empty object", `{}`, []fieldErr{
			{"name", "REQUIRED"}, {"content", "REQUIRED"}, {"entity_type", "REQUIRED"},
			{"language", "REQUIRED"}, {"domain", "REQUIRED"},
		}},
		// Malformed JSON fails before field validation, so no field errors are listed.
		{"malformed JSON", `{"name": `, nil},
	}

	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"create", http.MethodPost, "/v1/api/patterns"},
		{"update", http.MethodPut, "/v1/api/patterns/" + uuid.New().String()},
	}

	for _, ep := range endpoints {
		for _, tt := range tests {
			t.Run(ep.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				// No service expectations: an invalid body must never reach it.
				psvc := new(mockPatternService)
				router := newTestRouter(psvc, new(mockSearchService))

				w := doRequest(router, ep.method, ep.path, tt.body)

				got := decodeValidationProblem(t, w, ep.path)
				assert.Equal(t, tt.want, got)
				psvc.AssertExpectations(t)
			})
		}
	}
}

// TestPatternWrite_AcceptsLimits pins the inclusive upper bounds so the
// rejection cases above cannot pass because a limit is off by one.
func TestPatternWrite_AcceptsLimits(t *testing.T) {
	t.Parallel()

	body := validBody(t, map[string]any{
		"name":        "a" + strings.Repeat("b", 127),
		"content":     strings.Repeat("x", 100_000),
		"description": strings.Repeat("d", 500),
		"tags":        make([]string, 20),
	})
	updatePath := "/v1/api/patterns/" + uuid.New().String()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"create", http.MethodPost, "/v1/api/patterns", http.StatusAccepted},
		{"update", http.MethodPut, updatePath, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			psvc := new(mockPatternService)
			router := newTestRouter(psvc, new(mockSearchService))
			pattern := makePattern("go-error-handling")
			psvc.On("Create", mock.Anything, mock.Anything).Return(pattern, nil)
			psvc.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(pattern, nil)

			w := doRequest(router, tt.method, tt.path, body)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestPatternWrite_InvalidID checks that a non-UUID id is rejected on every
// route that takes one, before the service is called.
func TestPatternWrite_InvalidID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"update", http.MethodPut, "/v1/api/patterns/not-a-uuid", validBody(t, nil)},
		{"delete", http.MethodDelete, "/v1/api/patterns/not-a-uuid", ""},
		{"chunks", http.MethodGet, "/v1/api/patterns/not-a-uuid/chunks", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			psvc := new(mockPatternService)
			router := newTestRouter(psvc, new(mockSearchService))

			w := doRequest(router, tt.method, tt.path, tt.body)

			got := decodeValidationProblem(t, w, tt.path)
			assert.Equal(t, []fieldErr{{"id", "INVALID_FORMAT"}}, got)
			psvc.AssertExpectations(t)
		})
	}
}

func TestPatternUpdate_NotFound(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	router := newTestRouter(psvc, new(mockSearchService))

	id := uuid.New()
	path := "/v1/api/patterns/" + id.String()
	psvc.On("Update", mock.Anything, id, mock.Anything).
		Return(nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id))

	w := doRequest(router, http.MethodPut, path, validBody(t, nil))

	resp := decodeProblem(t, w, problemWant{http.StatusNotFound, "not-found", "Not Found", path})
	assert.Contains(t, resp.Detail, id.String())
	psvc.AssertExpectations(t)
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

// --- List and search query validation ---

// recordListOptions returns a mock callback that stores the ListOptions the
// handler passes to the service.
func recordListOptions(dst *patternsvc.ListOptions) func(mock.Arguments) {
	return func(args mock.Arguments) {
		*dst = args.Get(2).(patternsvc.ListOptions)
	}
}

// recordSearchOptions returns a mock callback that stores the SearchOptions
// the handler passes to the service.
func recordSearchOptions(dst *searchsvc.SearchOptions) func(mock.Arguments) {
	return func(args mock.Arguments) {
		*dst = args.Get(1).(searchsvc.SearchOptions)
	}
}

func TestPatternList_InvalidQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		wantField string
	}{
		{"limit zero", "limit=0", "limit"},
		{"limit above max", "limit=101", "limit"},
		{"limit negative", "limit=-1", "limit"},
		{"limit not a number", "limit=abc", "limit"},
		{"cursor not base64", "cursor=!!!", "cursor"},
		// "eA==" is valid base64 for "x", which is not a JSON cursor payload.
		{"cursor not JSON", "cursor=eA==", "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			psvc := new(mockPatternService)
			router := newTestRouter(psvc, new(mockSearchService))
			path := "/v1/api/patterns"

			w := doRequest(router, http.MethodGet, path+"?"+tt.query, "")

			got := decodeValidationProblem(t, w, path)
			assert.Equal(t, []fieldErr{{tt.wantField, "INVALID_VALUE"}}, got)
			psvc.AssertExpectations(t)
		})
	}
}

// TestPatternList_Pagination checks the default page size and that the
// handler asks for one extra row to learn whether another page exists.
func TestPatternList_Pagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		query        string
		returned     int
		wantLimit    int
		wantRequest  int
		wantData     int
		wantHasMore  bool
		wantNextPage bool
	}{
		{"default limit", "", 1, 20, 21, 1, false, false},
		{"more rows than limit", "?limit=2", 3, 2, 3, 2, true, true},
		{"exactly limit rows", "?limit=2", 2, 2, 3, 2, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			psvc := new(mockPatternService)
			router := newTestRouter(psvc, new(mockSearchService))
			rows := make([]*patternrepo.Pattern, tt.returned)
			for i := range rows {
				rows[i] = makePattern(fmt.Sprintf("pattern-%d", i))
			}
			var got patternsvc.ListOptions
			psvc.On("List", mock.Anything, mock.Anything, mock.Anything).
				Run(recordListOptions(&got)).Return(rows, nil)

			w := doRequest(router, http.MethodGet, "/v1/api/patterns"+tt.query, "")

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, patternsvc.ListOptions{Offset: 0, Limit: tt.wantRequest}, got)

			var resp struct {
				Data       []map[string]any    `json:"data"`
				Pagination handlers.Pagination `json:"pagination"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Len(t, resp.Data, tt.wantData)
			assert.Equal(t, tt.wantLimit, resp.Pagination.Limit)
			assert.Equal(t, tt.wantHasMore, resp.Pagination.HasMore)
			if !tt.wantNextPage {
				assert.Nil(t, resp.Pagination.NextCursor)
				return
			}

			require.NotNil(t, resp.Pagination.NextCursor)
			assert.Equal(t, handlers.EncodeCursor(tt.wantLimit), *resp.Pagination.NextCursor)
		})
	}
}

func TestSearch_InvalidQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		wantField string
		wantCode  string
	}{
		{"q 1001 chars", "q=" + strings.Repeat("a", 1001), "q", "MAX_LENGTH"},
		{"limit zero", "q=x&limit=0", "limit", "INVALID_VALUE"},
		{"limit 51", "q=x&limit=51", "limit", "INVALID_VALUE"},
		{"limit not a number", "q=x&limit=abc", "limit", "INVALID_VALUE"},
		{"threshold below zero", "q=x&threshold=-0.1", "threshold", "INVALID_VALUE"},
		{"threshold above one", "q=x&threshold=1.1", "threshold", "INVALID_VALUE"},
		{"threshold not a number", "q=x&threshold=abc", "threshold", "INVALID_VALUE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// No search expectations: an invalid query must never reach the service.
			ssvc := new(mockSearchService)
			router := newTestRouter(new(mockPatternService), ssvc)
			path := "/v1/api/patterns/search"

			w := doRequest(router, http.MethodGet, path+"?"+tt.query, "")

			got := decodeValidationProblem(t, w, path)
			assert.Equal(t, []fieldErr{{tt.wantField, tt.wantCode}}, got)
			ssvc.AssertExpectations(t)
		})
	}
}

// TestSearch_ForwardsOptions checks the defaults, the inclusive bounds and
// the comma-split tag filter as the service sees them.
func TestSearch_ForwardsOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  searchsvc.SearchOptions
	}{
		{"defaults", "q=x", searchsvc.SearchOptions{Query: "x", Limit: 10, Threshold: 0.7}},
		{"lower bounds", "q=x&limit=1&threshold=0", searchsvc.SearchOptions{Query: "x", Limit: 1, Threshold: 0}},
		{"upper bounds", "q=x&limit=50&threshold=1", searchsvc.SearchOptions{Query: "x", Limit: 50, Threshold: 1}},
		{"q 1000 chars", "q=" + strings.Repeat("a", 1000), searchsvc.SearchOptions{Query: strings.Repeat("a", 1000), Limit: 10, Threshold: 0.7}},
		{"tags split on comma", "q=x&tags=a,b", searchsvc.SearchOptions{Query: "x", Limit: 10, Threshold: 0.7, Tags: []string{"a", "b"}}},
		{"language and domain", "q=x&language=go&domain=backend", searchsvc.SearchOptions{Query: "x", Limit: 10, Threshold: 0.7, Language: "go", Domain: "backend"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssvc := new(mockSearchService)
			router := newTestRouter(new(mockPatternService), ssvc)
			var got searchsvc.SearchOptions
			ssvc.On("SearchPatterns", mock.Anything, mock.Anything).
				Run(recordSearchOptions(&got)).Return(&searchsvc.SearchResult{}, nil)

			w := doRequest(router, http.MethodGet, "/v1/api/patterns/search?"+tt.query, "")

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tt.want, got)
		})
	}
}
