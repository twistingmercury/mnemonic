// Command openai-stub answers the two OpenAI endpoints the API calls, so the
// E2E suite never reaches api.openai.com: POST /v1/embeddings returns a
// constant vector of the requested size, and GET /v1/models/{model} satisfies
// the health check.
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

const defaultDimensions = 2000

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

func handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	var req struct {
		Model      string `json:"model"`
		Dimensions int    `json:"dimensions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Dimensions <= 0 {
		req.Dimensions = defaultDimensions
	}
	embedding := make([]float32, req.Dimensions)
	for i := range embedding {
		embedding[i] = 0.1
	}
	writeJSON(w, map[string]any{
		"object": "list",
		"model":  req.Model,
		"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": embedding}},
		"usage":  map[string]int{"prompt_tokens": 8, "total_tokens": 8},
	})
}

func handleModel(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	writeJSON(w, map[string]string{"id": r.PathValue("model"), "object": "model"})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/embeddings", handleEmbeddings)
	mux.HandleFunc("GET /v1/models/{model}", handleModel)

	log.Println("OpenAI stub listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", mux)) // #nosec G114 -- test-only stub
}
