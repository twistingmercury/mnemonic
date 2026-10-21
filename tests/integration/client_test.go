package integration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
)

var (
	apiURL      = env("API_URL", "http://localhost:8080")
	mcpURL      = env("MCP_URL", "http://localhost:8081")
	mcpOpsURL   = env("MCP_OPS_URL", "http://localhost:8080")
	enricherURL = env("ENRICHER_URL", "http://localhost:8082")
	stubURL     = env("OPENAI_STUB_URL", "http://localhost:8090")

	httpClient = &http.Client{Timeout: 10 * time.Second}
)

// searchThreshold is low because the stub's embeddings score a one-word query
// against a whole chunk at about 0.17; unrelated text scores 0.
const searchThreshold = 0.1

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// stubDims is the embedding size the services request from the OpenAI stub
// (openai.embedding_dimensions).
const stubDims = 2000

// tokenDims holds the stub embedding dimension of every word the test
// patterns share, and of every token handed out so far.
var (
	tokenMu   sync.Mutex
	tokenDims map[uint32]bool
)

// stubDim is the dimension the OpenAI stub hashes word to.
func stubDim(word string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(word))
	return h.Sum32() % stubDims
}

// token returns a random word no other pattern contains, so a search for it
// finds only the pattern that holds it. The stub hashes each word to one
// dimension, so a token is re-rolled when its dimension is already taken by a
// shared word or another token; otherwise a search for it would match every
// pattern holding the colliding word.
func token(t *testing.T) string {
	t.Helper()
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if tokenDims == nil {
		tokenDims = map[uint32]bool{}
		shared := patternContent("", "errors", "wrapping", "unrelated") + " integ a b c integration-test"
		for _, w := range strings.FieldsFunc(strings.ToLower(shared), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			tokenDims[stubDim(w)] = true
		}
	}
	b := make([]byte, 6)
	for {
		if _, err := rand.Read(b); err != nil {
			t.Fatalf("random token: %v", err)
		}
		tok := "tok" + hex.EncodeToString(b)
		if d := stubDim(tok); !tokenDims[d] {
			tokenDims[d] = true
			return tok
		}
	}
}

// call sends a JSON request and returns the status and body. A nil body sends
// no payload.
func call(t *testing.T, method, rawURL string, body any) (int, []byte) {
	t.Helper()
	return callWith(t, method, rawURL, body, nil)
}

// callWith is call with extra request headers.
func callWith(t *testing.T, method, rawURL string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s %s: %v", method, rawURL, err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, rawURL, r)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, rawURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// The stub only checks that a key is present; the services send their own.
	req.Header.Set("Authorization", "Bearer sk-test")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, rawURL, err)
	}
	return resp.StatusCode, data
}

// mustCall is call that fails the test unless the status is want, and decodes
// the body into out when out is not nil.
func mustCall(t *testing.T, method, rawURL string, body any, want int, out any) {
	t.Helper()
	status, data := call(t, method, rawURL, body)
	if status != want {
		t.Fatalf("%s %s: status %d, want %d\nbody: %s", method, rawURL, status, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("decode %s %s: %v\nbody: %s", method, rawURL, err, data)
		}
	}
}

// pattern is the subset of the API's pattern resource these tests read.
type pattern struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Content          string `json:"content"`
	EnrichmentStatus string `json:"enrichment_status"`
	EnrichmentError  string `json:"enrichment_error"`
	EnrichedAt       string `json:"enriched_at"`
}

// patternContent returns enrichable content: one decorated section the API
// turns into a chunk, holding words, plus a concept line for the stub's
// extraction.
func patternContent(words string, concepts ...string) string {
	return fmt.Sprintf(`## Overview

Integration test pattern.

Concepts: %s

[//]: pattern

## Core Approach

Wrap errors with fmt.Errorf and the %%w verb, then inspect them with errors.As. %s
`, strings.Join(concepts, ", "), words)
}

// createPattern creates a pattern through the API and deletes it when the
// test ends.
func createPattern(t *testing.T, name, content string) pattern {
	t.Helper()
	var p pattern
	mustCall(t, http.MethodPost, apiURL+"/v1/api/patterns", map[string]any{
		"name":        name,
		"content":     content,
		"tags":        []string{"integration-test"},
		"entity_type": "go-pattern",
		"language":    "go",
		"domain":      "backend",
	}, http.StatusAccepted, &p)
	t.Cleanup(func() {
		// Already gone (404) is fine: some tests delete their own pattern.
		call(t, http.MethodDelete, apiURL+"/v1/api/patterns/"+p.ID, nil)
	})
	return p
}

// updatePattern replaces a pattern's content through the API.
func updatePattern(t *testing.T, p pattern, content string) {
	t.Helper()
	mustCall(t, http.MethodPut, apiURL+"/v1/api/patterns/"+p.ID, map[string]any{
		"name":        p.Name,
		"content":     content,
		"tags":        []string{"integration-test"},
		"entity_type": "go-pattern",
		"language":    "go",
		"domain":      "backend",
	}, http.StatusNoContent, nil)
}

// waitEnriched polls the API until the pattern is enriched. A failed status
// ends the wait only when allowFailed is false; the enricher's retry sweep can
// still recover it.
func waitEnriched(t *testing.T, id string, timeout time.Duration, allowFailed bool) pattern {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var p pattern
	for {
		mustCall(t, http.MethodGet, apiURL+"/v1/api/patterns/"+id, nil, http.StatusOK, &p)
		switch {
		case p.EnrichmentStatus == "enriched":
			return p
		case p.EnrichmentStatus == "failed" && !allowFailed:
			t.Fatalf("pattern %s enrichment failed: %s", id, p.EnrichmentError)
		case time.Now().After(deadline):
			t.Fatalf("pattern %s still %q after %s", id, p.EnrichmentStatus, timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// apiSearch runs the API's semantic search and returns the matching pattern IDs.
func apiSearch(t *testing.T, query string) []string {
	t.Helper()
	var resp struct {
		Results []struct {
			PatternID string `json:"pattern_id"`
		} `json:"results"`
	}
	q := url.Values{"q": {query}, "threshold": {fmt.Sprint(searchThreshold)}}
	mustCall(t, http.MethodGet, apiURL+"/v1/api/patterns/search?"+q.Encode(), nil, http.StatusOK, &resp)
	ids := make([]string, len(resp.Results))
	for i, r := range resp.Results {
		ids[i] = r.PatternID
	}
	return ids
}

// mcpProtocolVersion is the MCP protocol version the tests speak.
const mcpProtocolVersion = "2026-07-28"

// mcpTool calls an MCP tool and returns its text and whether the tool or the
// JSON-RPC layer reported an error. It sends a 2026-07-28 request: no
// initialize handshake, the protocol version and client capabilities in
// _meta, and the method and tool name mirrored into headers.
func mcpTool(t *testing.T, name string, args map[string]any) (text string, isError bool) {
	t.Helper()
	status, data := callWith(t, http.MethodPost, mcpURL+"/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
			"_meta": map[string]any{
				"io.modelcontextprotocol/protocolVersion":    mcpProtocolVersion,
				"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "mnemonic-integration-tests", "version": "0"},
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			},
		},
	}, map[string]string{
		"MCP-Protocol-Version": mcpProtocolVersion,
		"Mcp-Method":           "tools/call",
		"Mcp-Name":             name,
	})
	if status != http.StatusOK {
		t.Fatalf("MCP %s: status %d\nbody: %s", name, status, data)
	}
	// Streamable HTTP may frame the response as an SSE event.
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if payload, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			data = payload
			break
		}
	}
	var resp struct {
		Error  *struct{ Message string } `json:"error"`
		Result *struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode MCP %s: %v\nbody: %s", name, err, data)
	}
	if resp.Error != nil {
		return resp.Error.Message, true
	}
	if resp.Result == nil || len(resp.Result.Content) == 0 {
		t.Fatalf("MCP %s: empty result\nbody: %s", name, data)
	}
	return resp.Result.Content[0].Text, resp.Result.IsError
}

// mcpOK calls an MCP tool and fails the test if it reports an error.
func mcpOK(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	text, isError := mcpTool(t, name, args)
	if isError {
		t.Fatalf("MCP %s(%v) returned an error: %s", name, args, text)
	}
	return text
}

func mcpSearch(t *testing.T, query string) string {
	t.Helper()
	return mcpOK(t, "search_patterns", map[string]any{"query": query, "threshold": searchThreshold})
}
