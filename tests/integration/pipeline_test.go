package integration

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const enrichTimeout = 30 * time.Second

// TestPatternFlowsFromAPIToMCP follows one pattern through the whole stack:
// written through the API, enriched by the enricher via RabbitMQ, then found
// by both the API's and the MCP server's search and read back through MCP
// with the concepts the enricher extracted.
func TestPatternFlowsFromAPIToMCP(t *testing.T) {
	t.Parallel()

	tok := token(t)
	concepts := []string{tok + "-errors", tok + "-wrapping"}
	p := createPattern(t, "integ-"+tok, patternContent("", concepts...))
	if p.EnrichmentStatus != "pending" {
		t.Fatalf("new pattern enrichment_status = %q, want pending", p.EnrichmentStatus)
	}

	enriched := waitEnriched(t, p.ID, enrichTimeout, false)
	if enriched.EnrichedAt == "" {
		t.Error("enriched_at is empty after enrichment")
	}

	var chunks struct {
		Chunks []struct {
			EnrichmentStatus string `json:"enrichment_status"`
		} `json:"chunks"`
	}
	mustCall(t, http.MethodGet, apiURL+"/v1/api/patterns/"+p.ID+"/chunks", nil, http.StatusOK, &chunks)
	if len(chunks.Chunks) != 1 || chunks.Chunks[0].EnrichmentStatus != "enriched" {
		t.Errorf("chunks = %+v, want one enriched chunk", chunks.Chunks)
	}

	if ids := apiSearch(t, tok); !slices.Contains(ids, p.ID) {
		t.Errorf("API search for %q = %v, want it to include %s", tok, ids, p.ID)
	}
	if text := mcpSearch(t, tok); !strings.Contains(text, p.Name) {
		t.Errorf("MCP search for %q does not list %s:\n%s", tok, p.Name, text)
	}

	text := mcpOK(t, "get_pattern", map[string]any{"id": p.ID})
	for _, want := range append([]string{p.Name, "**Enrichment:** enriched", "### Extracted Concepts"}, concepts...) {
		if !strings.Contains(text, want) {
			t.Errorf("MCP get_pattern is missing %q:\n%s", want, text)
		}
	}
}

// TestUpdateReenrichesPattern checks that changing a pattern's content makes
// the enricher re-embed it, so search finds the new text and not the old.
func TestUpdateReenrichesPattern(t *testing.T) {
	t.Parallel()

	oldWord, newWord := token(t), token(t)
	p := createPattern(t, "integ-"+token(t), patternContent(oldWord))
	waitEnriched(t, p.ID, enrichTimeout, false)

	updatePattern(t, p, patternContent(newWord))
	waitEnriched(t, p.ID, enrichTimeout, false)

	if ids := apiSearch(t, newWord); !slices.Contains(ids, p.ID) {
		t.Errorf("API search for the new content = %v, want it to include %s", ids, p.ID)
	}
	if ids := apiSearch(t, oldWord); slices.Contains(ids, p.ID) {
		t.Errorf("API search for the replaced content still returns %s", p.ID)
	}
	if text := mcpSearch(t, newWord); !strings.Contains(text, p.Name) {
		t.Errorf("MCP search for the new content does not list %s:\n%s", p.Name, text)
	}
	if text := mcpSearch(t, oldWord); strings.Contains(text, p.Name) {
		t.Errorf("MCP search for the replaced content still lists %s:\n%s", p.Name, text)
	}
}

// TestRelatedPatternsAndDelete checks the enricher links patterns that share
// concepts in Neo4j, that MCP reports the link, and that deleting a pattern
// through the API removes it from every MCP read path.
func TestRelatedPatternsAndDelete(t *testing.T) {
	t.Parallel()

	tok := token(t)
	shared := []string{tok + "-retry", tok + "-backoff", tok + "-jitter"}

	// Enriched one after the other: the enricher links a pattern to those
	// already in the graph when it syncs.
	a := createPattern(t, "integ-a-"+tok, patternContent("", shared...))
	waitEnriched(t, a.ID, enrichTimeout, false)
	b := createPattern(t, "integ-b-"+tok, patternContent("", shared...))
	unrelated := createPattern(t, "integ-c-"+tok, patternContent("", tok+"-unrelated"))
	waitEnriched(t, b.ID, enrichTimeout, false)
	waitEnriched(t, unrelated.ID, enrichTimeout, false)

	text := mcpOK(t, "find_related_patterns", map[string]any{"pattern_id": a.ID})
	if !strings.Contains(text, b.Name) {
		t.Errorf("find_related_patterns(a) does not list b:\n%s", text)
	}
	if strings.Contains(text, unrelated.Name) {
		t.Errorf("find_related_patterns(a) lists a pattern with no shared concepts:\n%s", text)
	}
	for _, c := range shared {
		if !strings.Contains(text, c) {
			t.Errorf("find_related_patterns(a) is missing shared concept %q:\n%s", c, text)
		}
	}

	mustCall(t, http.MethodDelete, apiURL+"/v1/api/patterns/"+a.ID, nil, http.StatusNoContent, nil)
	mustCall(t, http.MethodGet, apiURL+"/v1/api/patterns/"+a.ID, nil, http.StatusNotFound, nil)

	if text, isError := mcpTool(t, "get_pattern", map[string]any{"id": a.ID}); !isError {
		t.Errorf("MCP get_pattern still returns the deleted pattern:\n%s", text)
	}
	if text := mcpSearch(t, tok); strings.Contains(text, a.Name) {
		t.Errorf("MCP search still lists the deleted pattern:\n%s", text)
	}
	if text := mcpOK(t, "find_related_patterns", map[string]any{"pattern_id": b.ID}); strings.Contains(text, a.Name) {
		t.Errorf("find_related_patterns(b) still lists the deleted pattern:\n%s", text)
	}
}

// TestEnricherRetriesFailedJob makes the first embedding call for a pattern
// fail and checks the enricher's retry sweep recovers it. Compose runs the
// enricher with no in-call OpenAI retries, so the failure reaches the job.
func TestEnricherRetriesFailedJob(t *testing.T) {
	t.Parallel()

	tok := token(t)
	mustCall(t, http.MethodPost, fmt.Sprintf("%s/control/fail?match=%s&n=1", stubURL, tok), nil, http.StatusNoContent, nil)

	p := createPattern(t, "integ-"+tok, patternContent(""))

	// The sweep runs every 15s, so allow for one full interval plus enrichment.
	deadline := time.Now().Add(45 * time.Second)
	sawFailed := false
	for {
		var got pattern
		mustCall(t, http.MethodGet, apiURL+"/v1/api/patterns/"+p.ID, nil, http.StatusOK, &got)
		if got.EnrichmentStatus == "failed" {
			sawFailed = true
		}
		if got.EnrichmentStatus == "enriched" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pattern still %q (%s) after the retry window", got.EnrichmentStatus, got.EnrichmentError)
		}
		time.Sleep(250 * time.Millisecond)
	}

	var stub struct {
		Remaining int `json:"remaining"`
	}
	mustCall(t, http.MethodGet, stubURL+"/control/fail?match="+tok, nil, http.StatusOK, &stub)
	if stub.Remaining != 0 {
		t.Errorf("the forced failure was never served (remaining = %d)", stub.Remaining)
	}
	if !sawFailed {
		t.Error("never saw the pattern fail, so the retry path was not exercised")
	}
}
