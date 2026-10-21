package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
)

// failNext is how many more embeddings requests should fail. It may go
// negative; only a positive count before a request fails it.
var failNext atomic.Int32

func requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") == "" {
		http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(w, r) {
		return
	}
	if failNext.Add(-1) >= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"forced failure"}`))
		return
	}

	embedding := make([]float32, 2000)
	for i := range embedding {
		embedding[i] = 0.1
	}

	type embeddingItem struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	type usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
	type response struct {
		Object string          `json:"object"`
		Data   []embeddingItem `json:"data"`
		Model  string          `json:"model"`
		Usage  usage           `json:"usage"`
	}

	resp := response{
		Object: "list",
		Data: []embeddingItem{
			{Object: "embedding", Index: 0, Embedding: embedding},
		},
		Model: "text-embedding-3-small",
		Usage: usage{PromptTokens: 8, TotalTokens: 8},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("embeddings encode error: %v", err)
	}
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(w, r) {
		return
	}

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type choice struct {
		Message message `json:"message"`
	}
	type response struct {
		Choices []choice `json:"choices"`
	}

	resp := response{
		Choices: []choice{
			{
				Message: message{
					Role:    "assistant",
					Content: `{"concepts":["pattern","design"],"technologies":["go"],"practices":["testing"]}`,
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("chat completions encode error: %v", err)
	}
}

// handleModel answers the health check's model lookup (GET /v1/models/{model}).
func handleModel(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"id":     r.PathValue("model"),
		"object": "model",
	}); err != nil {
		log.Printf("model encode error: %v", err)
	}
}

func handleFailNext(w http.ResponseWriter, r *http.Request) {
	failNext.Store(10)
	w.WriteHeader(http.StatusOK)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/embeddings", handleEmbeddings)
	mux.HandleFunc("POST /v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("GET /v1/models/{model}", handleModel)
	mux.HandleFunc("POST /control/fail-next", handleFailNext)

	log.Println("OpenAI stub listening on :8090")
	if err := http.ListenAndServe(":8090", mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
