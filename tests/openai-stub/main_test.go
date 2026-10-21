package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const authHeader = "Bearer test-key"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newStub().routes())
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body string, authed bool) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	require.NoError(t, err)
	if authed {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func embedRequest(t *testing.T, srv *httptest.Server, input string, dims int) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"input": input, "model": "m", "dimensions": dims})
	require.NoError(t, err)
	return do(t, http.MethodPost, srv.URL+"/v1/embeddings", string(body), true)
}

func embedding(t *testing.T, srv *httptest.Server, input string, dims int) []float64 {
	t.Helper()
	resp := embedRequest(t, srv, input, dims)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Data, 1)
	return out.Data[0].Embedding
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func remaining(t *testing.T, srv *httptest.Server, match string) int {
	t.Helper()
	resp := do(t, http.MethodGet, srv.URL+"/control/fail?match="+match, "", false)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Remaining int `json:"remaining"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out.Remaining
}

func TestEmbeddings_DeterministicAndUnitLength(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	first := embedding(t, srv, "retry with backoff", 64)
	second := embedding(t, srv, "retry with backoff", 64)

	assert.Equal(t, first, second)
	assert.InDelta(t, 1.0, cosine(first, first), 1e-6)
	var norm float64
	for _, x := range first {
		norm += x * x
	}
	assert.InDelta(t, 1.0, math.Sqrt(norm), 1e-6)
}

func TestEmbeddings_Dimensions(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	assert.Len(t, embedding(t, srv, "hello", 32), 32)
	assert.Len(t, embedding(t, srv, "hello", 0), 2000, "default is the service's vector size")
}

func TestEmbeddings_SharedWordsAreCloser(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	base := embedding(t, srv, "circuit breaker pattern", 2000)
	related := embedding(t, srv, "circuit breaker", 2000)
	disjoint := embedding(t, srv, "database migrations", 2000)

	assert.Greater(t, cosine(base, related), cosine(base, disjoint))
}

func TestEmbeddings_InvalidBody(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	resp := do(t, http.MethodPost, srv.URL+"/v1/embeddings", "{not json", true)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestChatCompletions_ConceptsLine(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	body := `{"model":"m","messages":[{"role":"system","content":"x"},{"role":"user","content":"Pattern content:\ntext\nConcepts: alpha, beta\nmore"}]}`

	resp := do(t, http.MethodPost, srv.URL+"/v1/chat/completions", body, true)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []string{"alpha", "beta"}, chatConcepts(t, resp))
}

func TestChatCompletions_NoConceptsLine(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	body := `{"messages":[{"role":"user","content":"nothing to see"}]}`

	resp := do(t, http.MethodPost, srv.URL+"/v1/chat/completions", body, true)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, chatConcepts(t, resp))
}

func chatConcepts(t *testing.T, resp *http.Response) []string {
	t.Helper()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Choices, 1)
	var result struct {
		Concepts     []string `json:"concepts"`
		Technologies []string `json:"technologies"`
		Practices    []string `json:"practices"`
	}
	require.NoError(t, json.Unmarshal([]byte(out.Choices[0].Message.Content), &result))
	require.NotNil(t, result.Concepts)
	return result.Concepts
}

func TestModel(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	resp := do(t, http.MethodGet, srv.URL+"/v1/models/text-embedding-3-large", "", true)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "text-embedding-3-large", out["id"])
}

func TestHealthz_NeedsNoAuthorization(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	resp := do(t, http.MethodGet, srv.URL+"/healthz", "", false)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestV1_RequiresAuthorization(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	routes := []struct{ method, path string }{
		{http.MethodPost, "/v1/embeddings"},
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodGet, "/v1/models/m"},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			resp := do(t, rt.method, srv.URL+rt.path, "{}", false)

			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestControlFail_OnlyMatchingInputsFailNTimes(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	set := do(t, http.MethodPost, srv.URL+"/control/fail?match=boom&n=2", "", false)
	require.Equal(t, http.StatusNoContent, set.StatusCode)
	require.Equal(t, 2, remaining(t, srv, "boom"))

	other := embedRequest(t, srv, "calm input", 8)
	first := embedRequest(t, srv, "a boom input", 8)
	second := embedRequest(t, srv, "boom again", 8)
	third := embedRequest(t, srv, "boom a third time", 8)

	assert.Equal(t, http.StatusOK, other.StatusCode)
	assert.Equal(t, http.StatusInternalServerError, first.StatusCode)
	assert.Equal(t, http.StatusInternalServerError, second.StatusCode)
	assert.Equal(t, http.StatusOK, third.StatusCode)
	assert.Equal(t, 0, remaining(t, srv, "boom"))
}

func TestControlFail_AcceptsGet(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	set := do(t, http.MethodGet, srv.URL+"/control/fail?match=zap&n=1", "", false)

	assert.Equal(t, http.StatusNoContent, set.StatusCode)
	assert.Equal(t, 1, remaining(t, srv, "zap"))
}

func TestControlFail_RejectsBadParameters(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	for _, query := range []string{"n=1", "match=x", "match=x&n=-1", "match=x&n=abc"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			resp := do(t, http.MethodPost, srv.URL+"/control/fail?"+query, "", false)

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestControlReset_ClearsInjectedFailures(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	do(t, http.MethodPost, srv.URL+"/control/fail?match=boom&n=5", "", false)
	do(t, http.MethodPost, srv.URL+"/control/fail?match=bang&n=5", "", false)

	reset := do(t, http.MethodPost, srv.URL+"/control/reset", "", false)

	assert.Equal(t, http.StatusNoContent, reset.StatusCode)
	assert.Equal(t, 0, remaining(t, srv, "boom"))
	assert.Equal(t, 0, remaining(t, srv, "bang"))
	assert.Equal(t, http.StatusOK, embedRequest(t, srv, "boom bang", 8).StatusCode)
}
