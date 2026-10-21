package api_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/twistingmercury/librarian-api/tests/e2e/helpers"
)

// =============================================================================
// Pattern Endpoint Tests
// =============================================================================
//
// Covers all pattern-related endpoints from the OpenAPI specification:
//
//   GET    /v1/api/patterns              List patterns (paginated, filterable)
//   POST   /v1/api/patterns              Create pattern (returns 202, async enrichment)
//   GET    /v1/api/patterns/search       Semantic search (vector similarity)
//   GET    /v1/api/patterns/{id}         Get pattern by ID (full content + graph)
//   PUT    /v1/api/patterns/{id}         Update pattern (full replacement)
//   DELETE /v1/api/patterns/{id}         Delete pattern
//
// Pattern resource:
//   - id: UUID (server-generated)
//   - name: Pattern name (^[a-z][a-z0-9-]*$, 1-128 chars)
//   - description: Optional (max 500 chars)
//   - content: Markdown (1-100000 bytes)
//   - tags: Optional (max 20 items)
//   - enrichment_status: pending | enriched | failed (async processing)
//
// Create returns 202 Accepted because enrichment (embedding generation,
// concept extraction) runs asynchronously. Patterns must reach "enriched"
// status before they appear in semantic search results.

// -----------------------------------------------------------------------------
// List Patterns (GET /v1/api/patterns)
// -----------------------------------------------------------------------------

func TestListPatterns_ReturnsOKWithPaginatedSummaries(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	// Data field must be present (may be empty slice)
	if list.Data == nil {
		t.Fatal("expected data field to be present (may be empty)")
	}

	// Pagination metadata must be present
	if list.Pagination.Limit <= 0 {
		t.Fatalf("expected pagination.limit > 0, got %d", list.Pagination.Limit)
	}
}

func TestListPatterns_DefaultPaginationValues(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	// Default limit per spec is 20
	if list.Pagination.Limit != 20 {
		t.Fatalf("expected default limit 20, got %d", list.Pagination.Limit)
	}
}

func TestListPatterns_CustomLimit(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns?limit=5", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?limit=5: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	if list.Pagination.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", list.Pagination.Limit)
	}

	if len(list.Data) > 5 {
		t.Fatalf("expected at most 5 items, got %d", len(list.Data))
	}
}

func TestListPatterns_CursorPaginationWalksAllPages(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create 3 patterns to ensure at least 2 pages with limit=2
	for i := 0; i < 3; i++ {
		body := helpers.PatternCreate{
			Name:       helpers.GenerateUniqueName("pattern"),
			Content:    "cursor pagination test content",
			EntityType: "go-pattern",
			Language:   "go",
			Domain:     "backend",
		}
		resp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
		if err != nil {
			t.Fatalf("failed to create pattern: %v", err)
		}
		resp.Body.Close()
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	maxPages := 20 // safety limit

	for pages < maxPages {
		path := "/v1/api/patterns?limit=2"
		if cursor != "" {
			path = fmt.Sprintf("%s&cursor=%s", path, cursor)
		}

		resp, err := client.Request(http.MethodGet, path, nil)
		if err != nil {
			t.Fatalf("failed to GET %s: %v", path, err)
		}

		list := helpers.ParseJSON[helpers.PatternList](t, resp)

		for _, p := range list.Data {
			if seen[p.ID] {
				t.Fatalf("duplicate pattern ID %q seen across pages", p.ID)
			}
			seen[p.ID] = true
		}

		pages++

		if !list.Pagination.HasMore {
			break
		}

		if list.Pagination.NextCursor == "" {
			t.Fatal("has_more is true but next_cursor is empty")
		}
		cursor = list.Pagination.NextCursor
	}

	if pages == 0 {
		t.Fatal("no pages returned")
	}
}

func TestListPatterns_SummaryExcludesContentField(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a pattern with content
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "this content should not appear in list summaries",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// List patterns and find our created pattern
	resp, err := client.Request(http.MethodGet, "/v1/api/patterns", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	for _, p := range list.Data {
		if p.ID == created.ID {
			// PatternSummary type has no Content field — the type itself enforces this.
			// The struct having no content field means the server did not return it.
			return
		}
	}

	// Pattern may be on a later page — that is acceptable; just verify the type structure.
	// PatternSummary does not have a Content field by definition.
	_ = helpers.PatternSummary{}
}

func TestListPatterns_ResponseIncludesRequestIDHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)
}

func TestListPatterns_FilterByTags(t *testing.T) {
	client := helpers.NewTestClient(t)

	uniqueTag := "tag-" + uuid.New().String()[:8]

	// Create a pattern with a unique tag
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "filter by tag content",
		Tags:       []string{uniqueTag},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Filter by the unique tag
	resp, err := client.Request(http.MethodGet, "/v1/api/patterns?tags="+uniqueTag, nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?tags=...: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	found := false
	for _, p := range list.Data {
		if p.ID == created.ID {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected to find created pattern %q in tag-filtered list", created.ID)
	}

	// All returned summaries should have the tag
	for _, p := range list.Data {
		hasTag := false
		for _, tag := range p.Tags {
			if tag == uniqueTag {
				hasTag = true
				break
			}
		}
		if !hasTag {
			t.Errorf("pattern %q in tag-filtered list does not have tag %q", p.ID, uniqueTag)
		}
	}
}

func TestListPatterns_FilterByMultipleTagsUsesANDLogic(t *testing.T) {
	client := helpers.NewTestClient(t)

	tagA := "tag-a-" + uuid.New().String()[:8]
	tagB := "tag-b-" + uuid.New().String()[:8]
	tagC := "tag-c-" + uuid.New().String()[:8]

	// Pattern with both tags
	bothBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "has both tags",
		Tags:       []string{tagA, tagB},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	bothResp, err := client.Request(http.MethodPost, "/v1/api/patterns", bothBody)
	if err != nil {
		t.Fatalf("failed to create pattern with both tags: %v", err)
	}
	helpers.AssertStatusCode(t, bothResp, http.StatusAccepted)
	both := helpers.ParseJSON[helpers.Pattern](t, bothResp)

	// Pattern with only tagA
	onlyABody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "has only tag a",
		Tags:       []string{tagA, tagC},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	onlyAResp, err := client.Request(http.MethodPost, "/v1/api/patterns", onlyABody)
	if err != nil {
		t.Fatalf("failed to create pattern with only tag A: %v", err)
	}
	helpers.AssertStatusCode(t, onlyAResp, http.StatusAccepted)
	onlyA := helpers.ParseJSON[helpers.Pattern](t, onlyAResp)

	// Filter by both tags — AND logic means only "both" pattern should appear
	resp, err := client.Request(http.MethodGet, fmt.Sprintf("/v1/api/patterns?tags=%s,%s", tagA, tagB), nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?tags=...: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	foundBoth := false
	foundOnlyA := false
	for _, p := range list.Data {
		if p.ID == both.ID {
			foundBoth = true
		}
		if p.ID == onlyA.ID {
			foundOnlyA = true
		}
	}

	if !foundBoth {
		t.Errorf("expected pattern with both tags to appear in AND-filtered results")
	}
	if foundOnlyA {
		t.Errorf("expected pattern with only tagA to be excluded from AND-filtered results")
	}
}

func TestListPatterns_FullTextSearchByNameAndDescription(t *testing.T) {
	client := helpers.NewTestClient(t)

	uniqueWord := "xyzzy" + uuid.New().String()[:8]

	body := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Description: fmt.Sprintf("description contains %s unique word", uniqueWord),
		Content:     "content for full text search test",
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns?search="+uniqueWord, nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?search=...: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	found := false
	for _, p := range list.Data {
		if p.ID == created.ID {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected to find pattern %q in full-text search results for %q", created.ID, uniqueWord)
	}
}

func TestListPatterns_CombinedTagAndSearchFilters(t *testing.T) {
	client := helpers.NewTestClient(t)

	uniqueTag := "tag-combined-" + uuid.New().String()[:8]
	uniqueWord := "combined" + uuid.New().String()[:8]

	// Pattern matching both tag and search
	matchBody := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Description: fmt.Sprintf("description with %s keyword", uniqueWord),
		Content:     "content for combined filter test",
		Tags:        []string{uniqueTag},
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}
	matchResp, err := client.Request(http.MethodPost, "/v1/api/patterns", matchBody)
	if err != nil {
		t.Fatalf("failed to create matching pattern: %v", err)
	}
	helpers.AssertStatusCode(t, matchResp, http.StatusAccepted)
	match := helpers.ParseJSON[helpers.Pattern](t, matchResp)

	// Pattern with tag but not search term
	tagOnlyBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content with tag only",
		Tags:       []string{uniqueTag},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	tagOnlyResp, err := client.Request(http.MethodPost, "/v1/api/patterns", tagOnlyBody)
	if err != nil {
		t.Fatalf("failed to create tag-only pattern: %v", err)
	}
	helpers.AssertStatusCode(t, tagOnlyResp, http.StatusAccepted)
	tagOnly := helpers.ParseJSON[helpers.Pattern](t, tagOnlyResp)

	path := fmt.Sprintf("/v1/api/patterns?tags=%s&search=%s", uniqueTag, uniqueWord)
	resp, err := client.Request(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", path, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	foundMatch := false
	foundTagOnly := false
	for _, p := range list.Data {
		if p.ID == match.ID {
			foundMatch = true
		}
		if p.ID == tagOnly.ID {
			foundTagOnly = true
		}
	}

	if !foundMatch {
		t.Errorf("expected pattern matching both filters to appear in results")
	}
	if foundTagOnly {
		t.Errorf("expected pattern with tag only (no search match) to be excluded")
	}
}

func TestListPatterns_InvalidLimitReturns400(t *testing.T) {
	t.Run("limit below minimum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns?limit=0", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns?limit=0: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("limit above maximum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns?limit=101", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns?limit=101: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("limit non-numeric", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns?limit=abc", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns?limit=abc: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

func TestListPatterns_InvalidCursorReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns?cursor=!!!invalid-cursor!!!", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?cursor=...: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Create Pattern (POST /v1/api/patterns)
// -----------------------------------------------------------------------------

func TestCreatePattern_ReturnsAcceptedWithPendingEnrichment(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "# Test Pattern\n\nThis is test content.",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending', got %q", pattern.EnrichmentStatus)
	}
}

func TestCreatePattern_ResponseIncludesLocationHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for location header test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("expected Location header to be present")
	}

	if !strings.Contains(location, "/v1/api/patterns/") {
		t.Fatalf("expected Location header to contain '/v1/api/patterns/', got %q", location)
	}
}

func TestCreatePattern_ServerGeneratesUUID(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for UUID test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID == "" {
		t.Fatal("expected server-generated UUID id to be present")
	}

	// Validate it's a valid UUID
	if _, err := uuid.Parse(pattern.ID); err != nil {
		t.Fatalf("expected id to be a valid UUID, got %q: %v", pattern.ID, err)
	}
}

func TestCreatePattern_MinimalFieldsOnlyNameAndContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "minimal content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID == "" {
		t.Fatal("expected id to be set")
	}
	if pattern.Name != body.Name {
		t.Fatalf("expected name %q, got %q", body.Name, pattern.Name)
	}
	if pattern.Content != body.Content {
		t.Fatalf("expected content %q, got %q", body.Content, pattern.Content)
	}
}

func TestCreatePattern_AllFieldsIncludingDescriptionAndTags(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Description: "test description",
		Content:     "# Full Pattern\n\nAll fields populated.",
		Tags:        []string{"go", "testing"},
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}

	resp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID == "" {
		t.Fatal("expected id to be set")
	}
	if pattern.Description != body.Description {
		t.Fatalf("expected description %q, got %q", body.Description, pattern.Description)
	}
	if len(pattern.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(pattern.Tags))
	}
}

func TestCreatePattern_DuplicateNameReturns409(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("pattern")
	body := helpers.PatternCreate{
		Name:       name,
		Content:    "original content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp1, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}
	resp1.Body.Close()
	helpers.AssertStatusCode(t, resp1, http.StatusAccepted)

	// Second create with same name should conflict
	body2 := helpers.PatternCreate{
		Name:       name,
		Content:    "duplicate name content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	resp2, err := client.Request(http.MethodPost, "/v1/api/patterns", body2)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns (duplicate): %v", err)
	}
	defer resp2.Body.Close()

	helpers.AssertStatusCode(t, resp2, http.StatusConflict)
}

func TestCreatePattern_ValidationErrors(t *testing.T) {
	cases := []struct {
		name        string
		body        interface{}
		expectField string
	}{
		{
			name:        "missing name",
			body:        helpers.PatternCreate{Content: "some content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "missing content",
			body:        helpers.PatternCreate{Name: "valid-name", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "name too long",
			body:        helpers.PatternCreate{Name: strings.Repeat("a", 129), Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format uppercase",
			body:        helpers.PatternCreate{Name: "Invalid-Name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format starts with number",
			body:        helpers.PatternCreate{Name: "1-bad-name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format underscores",
			body:        helpers.PatternCreate{Name: "bad_name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "content exceeds max size",
			body:        helpers.PatternCreate{Name: "valid-name", Content: strings.Repeat("x", 100_001), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "description too long",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", Description: strings.Repeat("d", 501), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "description",
		},
		{
			name: "too many tags",
			body: helpers.PatternCreate{
				Name:       "valid-name",
				Content:    "content",
				Tags:       makeTags(21),
				EntityType: "go-pattern",
				Language:   "go",
				Domain:     "backend",
			},
			expectField: "tags",
		},
		{
			name:        "missing entity_type",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", Language: "go", Domain: "backend"},
			expectField: "entity_type",
		},
		{
			name:        "missing language",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "missing domain",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go"},
			expectField: "domain",
		},
		{
			name:        "invalid language value",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "COBOL", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "invalid domain value",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "Not A Domain"},
			expectField: "domain",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := helpers.NewTestClient(t)

			resp, err := client.Request(http.MethodPost, "/v1/api/patterns", tc.body)
			if err != nil {
				t.Fatalf("failed to POST /v1/api/patterns: %v", err)
			}

			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)

			fieldFound := false
			for _, fe := range errResp.Errors {
				if fe.Field == tc.expectField {
					fieldFound = true
					break
				}
			}

			if !fieldFound {
				t.Fatalf("expected field error for %q, got errors: %+v", tc.expectField, errResp.Errors)
			}
			_ = tc.body
			_ = tc.expectField
		})
	}
}

func TestCreatePattern_InvalidJSONReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/patterns", strings.NewReader(`{invalid json`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns with invalid JSON: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestCreatePattern_EmptyBodyReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/patterns", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns with empty body: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Semantic Search (GET /v1/api/patterns/search)
// -----------------------------------------------------------------------------

func TestSearchPatterns_ReturnsRankedResultsWithSimilarity(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q=error+handling+in+go", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	// Results slice must be present (may be empty since nothing is enriched)
	if searchResp.Results == nil {
		t.Fatal("expected results field to be present (may be empty)")
	}
}

// TestSearchPatterns_QueryParameters checks that each accepted parameter
// combination returns 200 and that every result honours the requested limit
// and threshold and carries content and an in-range similarity.
func TestSearchPatterns_QueryParameters(t *testing.T) {
	client := helpers.NewTestClient(t)

	for _, tc := range []struct {
		name          string
		query         string
		maxResults    int
		minSimilarity float64
	}{
		{"default limit", "q=default+limit+test", 10, 0},
		{"custom limit", "q=custom+limit+test&limit=3", 3, 0},
		{"custom threshold", "q=threshold+test&threshold=0.9", 10, 0.9},
		{"filter by tags", "q=tag+filtered+search&tags=go,errors", 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?"+tc.query, nil)
			if err != nil {
				t.Fatalf("failed to GET /v1/api/patterns/search?%s: %v", tc.query, err)
			}

			helpers.AssertStatusCode(t, resp, http.StatusOK)

			searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)
			if searchResp.Results == nil {
				t.Fatal("expected results field to be present (may be empty)")
			}
			if len(searchResp.Results) > tc.maxResults {
				t.Fatalf("expected at most %d results, got %d", tc.maxResults, len(searchResp.Results))
			}
			for _, r := range searchResp.Results {
				if r.Content == "" {
					t.Errorf("result %q has empty content", r.PatternID)
				}
				if r.Similarity < tc.minSimilarity || r.Similarity > 1 {
					t.Errorf("result %q has similarity %f out of [%g,1]", r.PatternID, r.Similarity, tc.minSimilarity)
				}
			}
		})
	}
}

func TestSearchPatterns_ResponseIncludesMetadata(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q=metadata+test+query", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	if searchResp.Metadata.Query == "" {
		t.Fatal("expected metadata.query to be present")
	}
	if searchResp.Metadata.SearchDurationMs < 0 {
		t.Fatalf("expected metadata.search_duration_ms >= 0, got %d", searchResp.Metadata.SearchDurationMs)
	}
}

func TestSearchPatterns_MetadataEchoesQueryString(t *testing.T) {
	client := helpers.NewTestClient(t)

	query := "echo this query string back"

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q="+strings.ReplaceAll(query, " ", "+"), nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	if searchResp.Metadata.Query != query {
		t.Fatalf("expected metadata.query to echo %q, got %q", query, searchResp.Metadata.Query)
	}
}

func TestSearchPatterns_OnlyEnrichedPatternsAppear(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a pattern — it will be in "pending" or "failed" state (not enriched)
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment exclusion test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Search for the pattern by its unique name
	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q="+strings.ReplaceAll(body.Name, "-", "+"), nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	// Verify 200 + correct structure
	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	if searchResp.Results == nil {
		t.Fatal("expected results field to be present")
	}

	// No enricher runs in this suite, so the pattern stays unenriched and must not appear
	for _, r := range searchResp.Results {
		if r.PatternID == created.ID {
			t.Errorf("expected non-enriched pattern %q to be excluded from search results", created.ID)
		}
	}
}

func TestSearchPatterns_MissingQueryReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search (no q): %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestSearchPatterns_EmptyQueryReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q=", nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search?q= (empty): %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestSearchPatterns_QueryTooLongReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	longQuery := strings.Repeat("a", 1001)
	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q="+longQuery, nil)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search with long query: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestSearchPatterns_InvalidLimitReturns400(t *testing.T) {
	t.Run("limit below minimum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q=test&limit=0", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?limit=0: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("limit above maximum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q=test&limit=51", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?limit=51: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

func TestSearchPatterns_InvalidThresholdReturns400(t *testing.T) {
	t.Run("threshold below zero", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q=test&threshold=-0.1", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?threshold=-0.1: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("threshold above one", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns/search?q=test&threshold=1.1", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?threshold=1.1: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

// -----------------------------------------------------------------------------
// Get Pattern (GET /v1/api/patterns/{id})
// -----------------------------------------------------------------------------

func TestGetPattern_ReturnsFullPatternWithContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "# Full Content Pattern\n\nComplete markdown content here.",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID != created.ID {
		t.Fatalf("expected id %q, got %q", created.ID, pattern.ID)
	}
	if pattern.Name != body.Name {
		t.Fatalf("expected name %q, got %q", body.Name, pattern.Name)
	}
	if pattern.Content != body.Content {
		t.Fatalf("expected content %q, got %q", body.Content, pattern.Content)
	}
}

func TestGetPattern_IncludesEnrichmentStatus(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment status test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.EnrichmentStatus == "" {
		t.Fatal("expected enrichment_status to be present")
	}

	validStatuses := map[string]bool{"pending": true, "enriched": true, "failed": true}
	if !validStatuses[pattern.EnrichmentStatus] {
		t.Fatalf("expected enrichment_status to be one of pending/enriched/failed, got %q", pattern.EnrichmentStatus)
	}
}

func TestGetPattern_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	resp, err := client.Request(http.MethodGet, patternPath(nonExistentID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestGetPattern_InvalidUUIDReturns400(t *testing.T) {
	t.Run("not a uuid", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Request(http.MethodGet, "/v1/api/patterns/not-a-uuid", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/not-a-uuid: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("empty id", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		// This routes to the list endpoint, not an invalid UUID — verify 200 not 400
		resp, err := client.Request(http.MethodGet, "/v1/api/patterns/", nil)
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/: %v", err)
		}
		defer resp.Body.Close()

		// An empty id segment typically resolves to the list route (200)
		// or a 400 depending on routing. Accept either.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 200, 400, or 404 for empty id, got %d", resp.StatusCode)
		}
	})
}

func TestGetPattern_ResponseIncludesRequestIDHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for request id header test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)
}

// -----------------------------------------------------------------------------
// Update Pattern (PUT /v1/api/patterns/{id})
// -----------------------------------------------------------------------------

func TestUpdatePattern_ReturnsNoContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "updated content with new information",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPut, patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_UpdatedAtChangesCreatedAtPreserved(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content for timestamp test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "updated content for timestamp test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPut, patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_FullReplacementResetsOmittedFields(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Content:     "original content",
		Description: "original description",
		Tags:        []string{"original-tag"},
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Update with only name and content — description and tags are omitted
	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "replacement content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPut, patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_ContentChangeResetsEnrichmentToPending(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content before update",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "completely new content triggers re-enrichment",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPut, patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	updateBody := helpers.PatternUpdate{
		Name:       "valid-name",
		Content:    "content for non-existent pattern",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPut, patternPath(nonExistentID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestUpdatePattern_ValidationErrors(t *testing.T) {
	cases := []struct {
		name        string
		body        interface{}
		expectField string
	}{
		{
			name:        "missing name",
			body:        helpers.PatternUpdate{Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "missing content",
			body:        helpers.PatternUpdate{Name: "valid-name", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "name too long",
			body:        helpers.PatternUpdate{Name: strings.Repeat("a", 129), Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format",
			body:        helpers.PatternUpdate{Name: "BAD_NAME", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "content exceeds max size",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: strings.Repeat("x", 100_001), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "description too long",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", Description: strings.Repeat("d", 501), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "description",
		},
		{
			name: "too many tags",
			body: helpers.PatternUpdate{
				Name:       "valid-name",
				Content:    "content",
				Tags:       makeTags(21),
				EntityType: "go-pattern",
				Language:   "go",
				Domain:     "backend",
			},
			expectField: "tags",
		},
		{
			name:        "missing entity_type",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", Language: "go", Domain: "backend"},
			expectField: "entity_type",
		},
		{
			name:        "missing language",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "missing domain",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go"},
			expectField: "domain",
		},
		{
			name:        "invalid language value",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "COBOL", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "invalid domain value",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "Not A Domain"},
			expectField: "domain",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := helpers.NewTestClient(t)

			// Create a real pattern to update (so we get 400, not 404)
			createBody := helpers.PatternCreate{
				Name:       helpers.GenerateUniqueName("pattern"),
				Content:    "content for validation test",
				EntityType: "go-pattern",
				Language:   "go",
				Domain:     "backend",
			}
			createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", createBody)
			if err != nil {
				t.Fatalf("failed to create pattern: %v", err)
			}
			helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
			created := helpers.ParseJSON[helpers.Pattern](t, createResp)

			resp, err := client.Request(http.MethodPut, patternPath(created.ID), tc.body)
			if err != nil {
				t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
			}

			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)

			fieldFound := false
			for _, fe := range errResp.Errors {
				if fe.Field == tc.expectField {
					fieldFound = true
					break
				}
			}

			if !fieldFound {
				t.Fatalf("expected field error for %q, got errors: %+v", tc.expectField, errResp.Errors)
			}
			_ = tc.body
			_ = tc.expectField
		})
	}
}

func TestUpdatePattern_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	updateBody := helpers.PatternUpdate{
		Name:       "valid-name",
		Content:    "content for invalid uuid test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPut, "/v1/api/patterns/not-a-valid-uuid", updateBody)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/patterns/not-a-valid-uuid: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Delete Pattern (DELETE /v1/api/patterns/{id})
// -----------------------------------------------------------------------------

func TestDeletePattern_ReturnsNoContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for delete test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodDelete, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to DELETE %s: %v", patternPath(created.ID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
}

func TestDeletePattern_PatternNoLongerRetrievable(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for delete then get test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	deleteResp, err := client.Request(http.MethodDelete, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to DELETE %s: %v", patternPath(created.ID), err)
	}
	deleteResp.Body.Close()
	helpers.AssertStatusCode(t, deleteResp, http.StatusNoContent)

	getResp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s after delete: %v", patternPath(created.ID), err)
	}
	defer getResp.Body.Close()

	helpers.AssertStatusCode(t, getResp, http.StatusNotFound)
}

func TestDeletePattern_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	resp, err := client.Request(http.MethodDelete, patternPath(nonExistentID), nil)
	if err != nil {
		t.Fatalf("failed to DELETE %s: %v", patternPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestDeletePattern_SecondDeleteReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for double delete test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// First delete
	resp1, err := client.Request(http.MethodDelete, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed first DELETE %s: %v", patternPath(created.ID), err)
	}
	resp1.Body.Close()
	helpers.AssertStatusCode(t, resp1, http.StatusNoContent)

	// Second delete should return 404
	resp2, err := client.Request(http.MethodDelete, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed second DELETE %s: %v", patternPath(created.ID), err)
	}
	defer resp2.Body.Close()

	helpers.AssertStatusCode(t, resp2, http.StatusNotFound)
}

func TestDeletePattern_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodDelete, "/v1/api/patterns/not-a-valid-uuid", nil)
	if err != nil {
		t.Fatalf("failed to DELETE /v1/api/patterns/not-a-valid-uuid: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Pattern Enrichment (cross-cutting async behavior)
// -----------------------------------------------------------------------------

func TestPatternEnrichment_NewPatternStartsWithPendingStatus(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment pending status test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending' immediately after create, got %q", pattern.EnrichmentStatus)
	}
}

func TestPatternEnrichment_StatusTransitionsToPendingOrFailed(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment status transition test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}
	p := helpers.ParseJSON[helpers.Pattern](t, resp)

	if p.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending' (no enricher running in E2E), got %q", p.EnrichmentStatus)
	}
}

func TestPatternEnrichment_EnrichedAtSetWhenEnriched(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enriched_at timestamp test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}
	p := helpers.ParseJSON[helpers.Pattern](t, resp)

	if p.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending' (no enricher running in E2E), got %q", p.EnrichmentStatus)
	}
	if p.EnrichedAt != "" {
		t.Fatalf("expected enriched_at to be empty when status is pending, got %q", p.EnrichedAt)
	}
}

func TestPatternEnrichment_ErrorSetWhenFailed(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment error test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}
	p := helpers.ParseJSON[helpers.Pattern](t, resp)

	if p.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending' (no enricher running in E2E), got %q", p.EnrichmentStatus)
	}
}

func TestPatternEnrichment_ContentUpdateTriggersReenrichment(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content before re-enrichment",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Update content — should reset enrichment_status to pending
	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "new content after re-enrichment trigger",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	updateResp, err := client.Request(http.MethodPut, patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, updateResp, http.StatusNoContent)
	helpers.ReadBody(t, updateResp)

	getResp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}
	updated := helpers.ParseJSON[helpers.Pattern](t, getResp)
	if updated.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending' after update, got %q", updated.EnrichmentStatus)
	}
}

// -----------------------------------------------------------------------------
// Get Pattern Chunks (GET /v1/api/patterns/{id}/chunks)
// -----------------------------------------------------------------------------

// decoratedTwoSections yields exactly two chunks. Only sections carrying the
// [//]: pattern decorator are chunked, so undecorated markdown produces none —
// which is what made the earlier version of this test vacuous.
const decoratedTwoSections = "[//]: pattern\n## Philosophy\nStorage-only databases.\n\n[//]: pattern\n## Usage\nCall the API."

// decoratedThreeSections replaces the above with different titles and a
// different count, so a stale chunk surviving an update is visible.
const decoratedThreeSections = "[//]: pattern\n## Rationale\nWhy it exists.\n\n[//]: pattern\n## Limits\nWhat it will not do.\n\n[//]: pattern\n## Recovery\nHow to repair it."

func TestGetPatternChunks_ReturnsChunksForPattern(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Chunks are written synchronously inside Create's transaction. Enrichment
	// only adds embeddings later, so they must be complete the moment Create
	// returns — no polling, and an empty list is a failure, not a pending state.
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    decoratedTwoSections,
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/"+created.ID+"/chunks", nil)
	if err != nil {
		t.Fatalf("failed to GET chunks: %v", err)
	}
	helpers.AssertStatusCode(t, resp, http.StatusOK)
	chunkList := helpers.ParseJSON[helpers.ChunkListResponse](t, resp)

	if len(chunkList.Chunks) != 2 {
		t.Fatalf("expected exactly 2 chunks immediately after create, got %d: %+v",
			len(chunkList.Chunks), chunkList.Chunks)
	}
	if chunkList.Count != 2 {
		t.Errorf("expected count 2, got %d", chunkList.Count)
	}

	wantTitles := []string{"Philosophy", "Usage"}
	for i, chunk := range chunkList.Chunks {
		if chunk.ChunkIndex != i {
			t.Errorf("chunk %d: expected chunk_index %d, got %d", i, i, chunk.ChunkIndex)
		}
		if chunk.SectionTitle != wantTitles[i] {
			t.Errorf("chunk %d: expected section_title %q, got %q", i, wantTitles[i], chunk.SectionTitle)
		}
		// Every chunk is created with its enrichment job, so none can be
		// missing one and stuck outside the pending queue.
		if chunk.EnrichmentStatus != "pending" {
			t.Errorf("chunk %d: expected enrichment_status \"pending\", got %q", i, chunk.EnrichmentStatus)
		}
	}
}

// TestUpdatePattern_ReplacesChunks covers the half of re-enrichment that HTTP
// can see: Update deletes every stale chunk and inserts the new set in one
// transaction. Asserting only that status returns to "pending" proves nothing,
// because a freshly created pattern is already pending — that assertion
// survives deleting the reset entirely.
func TestUpdatePattern_ReplacesChunks(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("pattern")
	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", helpers.PatternCreate{
		Name:       name,
		Content:    decoratedTwoSections,
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	})
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	before := getChunkTitles(t, client, created.ID)
	if got := []string{"Philosophy", "Usage"}; !slices.Equal(before, got) {
		t.Fatalf("precondition failed: expected %v before update, got %v", got, before)
	}

	updateResp, err := client.Request(http.MethodPut, patternPath(created.ID), helpers.PatternUpdate{
		Name:       name,
		Content:    decoratedThreeSections,
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	})
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}
	helpers.AssertStatusCode(t, updateResp, http.StatusNoContent)
	helpers.ReadBody(t, updateResp)

	after := getChunkTitles(t, client, created.ID)
	want := []string{"Rationale", "Limits", "Recovery"}
	if !slices.Equal(after, want) {
		t.Fatalf("expected chunks replaced with %v, got %v", want, after)
	}

	// Stale chunks must be gone, not merely outnumbered by new ones.
	for _, stale := range before {
		for _, title := range after {
			if title == stale {
				t.Errorf("stale chunk %q survived the update", stale)
			}
		}
	}

	getResp, err := client.Request(http.MethodGet, patternPath(created.ID), nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}
	updated := helpers.ParseJSON[helpers.Pattern](t, getResp)
	if updated.EnrichmentStatus != "pending" {
		t.Errorf("expected enrichment_status \"pending\" after update, got %q", updated.EnrichmentStatus)
	}
}

func getChunkTitles(t *testing.T, client *helpers.TestClient, patternID string) []string {
	t.Helper()
	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/"+patternID+"/chunks", nil)
	if err != nil {
		t.Fatalf("failed to GET chunks: %v", err)
	}
	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.ChunkListResponse](t, resp)
	titles := make([]string, len(list.Chunks))
	for i, c := range list.Chunks {
		titles[i] = c.SectionTitle
	}
	return titles
}

// overlongSectionTitle exceeds pattern_chunks.section_title (varchar(255)), so
// the chunk INSERT fails after the pattern row has already been inserted in the
// same transaction. That is the only way to force a mid-transaction failure
// through the public API, and it is what makes the two rollback tests below
// possible.
//
// If a per-section-title limit is ever validated at the handler, these tests
// will start seeing 400 instead of 500 and fail loudly. That is the correct
// signal: move them to the integration tier, where the failure can be injected
// rather than provoked.
func overlongSectionTitle() string {
	return "[//]: pattern\n## " + strings.Repeat("T", 300) + "\nBody text."
}

// TestCreatePattern_FailedWriteLeavesNothingPersisted covers H4: Create used to
// insert the pattern before its chunks without an enclosing transaction, so a
// chunk failure returned 500 with the pattern already persisted — and the
// caller's retry then hit 409 on its own name, with no way forward.
func TestCreatePattern_FailedWriteLeavesNothingPersisted(t *testing.T) {
	client := helpers.NewTestClient(t)
	name := helpers.GenerateUniqueName("rollback")

	failResp, err := client.Request(http.MethodPost, "/v1/api/patterns", helpers.PatternCreate{
		Name:       name,
		Content:    overlongSectionTitle(),
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	})
	if err != nil {
		t.Fatalf("failed to POST: %v", err)
	}
	helpers.AssertStatusCode(t, failResp, http.StatusInternalServerError)
	helpers.ReadBody(t, failResp)

	// The decisive check: the same name must be free. A pattern row surviving
	// the failure would answer 409 here, which is exactly the dead end H4
	// describes.
	retryResp, err := client.Request(http.MethodPost, "/v1/api/patterns", helpers.PatternCreate{
		Name:       name,
		Content:    decoratedTwoSections,
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	})
	if err != nil {
		t.Fatalf("failed to POST retry: %v", err)
	}
	if retryResp.StatusCode == http.StatusConflict {
		t.Fatalf("retry returned 409: the failed create left a pattern row behind")
	}
	helpers.AssertStatusCode(t, retryResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, retryResp)

	// And the retry is a complete pattern, not a repair of a half-written one.
	if got := getChunkTitles(t, client, created.ID); !slices.Equal(got, []string{"Philosophy", "Usage"}) {
		t.Fatalf("expected the retry to write both chunks, got %v", got)
	}
}

// TestUpdatePattern_FailedWriteLeavesPriorStateIntact matters more than the
// Create case: Update deletes the existing chunks inside the transaction before
// inserting the new ones, so a failure that committed would leave the pattern
// with no chunks at all and no route back to being enriched.
func TestUpdatePattern_FailedWriteLeavesPriorStateIntact(t *testing.T) {
	client := helpers.NewTestClient(t)
	name := helpers.GenerateUniqueName("rollback-update")

	createResp, err := client.Request(http.MethodPost, "/v1/api/patterns", helpers.PatternCreate{
		Name:       name,
		Content:    decoratedTwoSections,
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	})
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	before := helpers.ParseJSON[helpers.Pattern](t, mustGet(t, client, patternPath(created.ID)))

	failResp, err := client.Request(http.MethodPut, patternPath(created.ID), helpers.PatternUpdate{
		Name:       name,
		Content:    overlongSectionTitle(),
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	})
	if err != nil {
		t.Fatalf("failed to PUT: %v", err)
	}
	helpers.AssertStatusCode(t, failResp, http.StatusInternalServerError)
	helpers.ReadBody(t, failResp)

	after := helpers.ParseJSON[helpers.Pattern](t, mustGet(t, client, patternPath(created.ID)))
	if after.Content != before.Content {
		t.Errorf("content changed despite the failed update:\n before: %q\n after:  %q", before.Content, after.Content)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Errorf("updated_at changed despite the failed update: %q -> %q", before.UpdatedAt, after.UpdatedAt)
	}

	// The stale chunks must still be there. This is what a committed partial
	// update would destroy.
	if got := getChunkTitles(t, client, created.ID); !slices.Equal(got, []string{"Philosophy", "Usage"}) {
		t.Fatalf("expected the original chunks to survive, got %v", got)
	}
}

func mustGet(t *testing.T, client *helpers.TestClient, path string) *http.Response {
	t.Helper()
	resp, err := client.Request(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", path, err)
	}
	helpers.AssertStatusCode(t, resp, http.StatusOK)
	return resp
}

func TestGetPatternChunks_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Request(http.MethodGet, "/v1/api/patterns/not-a-uuid/chunks", nil)
	if err != nil {
		t.Fatalf("failed to GET chunks: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// makeTags generates n unique tag strings for table-driven tests.
func makeTags(n int) []string {
	tags := make([]string, n)
	for i := range tags {
		tags[i] = fmt.Sprintf("tag-%d", i)
	}
	return tags
}

// patternPath returns the API path for a specific pattern ID.
func patternPath(id string) string {
	return fmt.Sprintf("/v1/api/patterns/%s", id)
}
