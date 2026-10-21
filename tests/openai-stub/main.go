// Command openai-stub stands in for the OpenAI API so the integration suite
// is deterministic, free and runs without a key.
//
//   - POST /v1/embeddings hashes the input's words into a normalised vector,
//     so cosine similarity tracks shared words: a query made of a word that
//     only one pattern contains finds that pattern and nothing else.
//   - POST /v1/chat/completions (concept extraction) returns the concepts
//     listed on a "Concepts: a, b" line of the pattern content.
//   - GET /v1/models/{model} answers the services' health checks.
//   - POST /control/fail?match=X&n=N makes the next N embedding requests
//     whose input contains X fail with 500; GET returns how many remain.
package main

import (
	"encoding/json"
	"hash/fnv"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

const defaultDimensions = 2000

// failures maps an input substring to how many more embedding requests
// containing it must fail.
var (
	mu       sync.Mutex
	failures = map[string]int{}
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/embeddings", handleEmbeddings)
	mux.HandleFunc("POST /v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("GET /v1/models/{model}", handleModel)
	mux.HandleFunc("POST /control/fail", handleSetFailures)
	mux.HandleFunc("GET /control/fail", handleGetFailures)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	log.Println("OpenAI stub listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", mux)) // #nosec G114 -- test-only stub
}

func handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	var req struct {
		Input      string `json:"input"`
		Model      string `json:"model"`
		Dimensions int    `json:"dimensions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if shouldFail(req.Input) {
		http.Error(w, `{"error":"forced failure"}`, http.StatusInternalServerError)
		return
	}
	if req.Dimensions <= 0 {
		req.Dimensions = defaultDimensions
	}
	writeJSON(w, map[string]any{
		"object": "list",
		"model":  req.Model,
		"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": embed(req.Input, req.Dimensions)}},
		"usage":  map[string]int{"prompt_tokens": 8, "total_tokens": 8},
	})
}

// embed returns a unit vector with one count per distinct word, each word
// hashed to a dimension.
func embed(text string, dims int) []float32 {
	v := make([]float64, dims)
	seen := map[string]bool{}
	for _, word := range words(text) {
		if seen[word] {
			continue
		}
		seen[word] = true
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		v[h.Sum32()%uint32(dims)]++
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	out := make([]float32, dims)
	for i, x := range v {
		if norm > 0 {
			out[i] = float32(x / norm)
		}
	}
	if norm == 0 {
		out[0] = 1 // pgvector cannot compare a zero vector
	}
	return out
}

func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func shouldFail(input string) bool {
	mu.Lock()
	defer mu.Unlock()
	for match, n := range failures {
		if n > 0 && strings.Contains(input, match) {
			failures[match] = n - 1
			return true
		}
	}
	return false
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	concepts := []string{}
	for _, m := range req.Messages {
		for line := range strings.SplitSeq(m.Content, "\n") {
			list, ok := strings.CutPrefix(strings.TrimSpace(line), "Concepts:")
			if !ok {
				continue
			}
			for c := range strings.SplitSeq(list, ",") {
				if c = strings.TrimSpace(c); c != "" {
					concepts = append(concepts, c)
				}
			}
		}
	}
	content, _ := json.Marshal(map[string][]string{
		"concepts":     concepts,
		"technologies": {},
		"practices":    {},
	})
	writeJSON(w, map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": string(content)}}},
	})
}

func handleModel(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	writeJSON(w, map[string]string{"id": r.PathValue("model"), "object": "model"})
}

func handleSetFailures(w http.ResponseWriter, r *http.Request) {
	match := r.URL.Query().Get("match")
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if match == "" || err != nil || n < 0 {
		http.Error(w, "match and a non-negative n are required", http.StatusBadRequest)
		return
	}
	mu.Lock()
	failures[match] = n
	mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func handleGetFailures(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	remaining := failures[r.URL.Query().Get("match")]
	mu.Unlock()
	writeJSON(w, map[string]int{"remaining": remaining})
}

func authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") == "" {
		http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}
