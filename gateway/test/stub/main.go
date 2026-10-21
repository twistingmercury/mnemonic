// Command stub stands in for mnemonic-api and mnemonic-mcp when testing the
// gateway alone. It answers every request with 200 and echoes what it
// received, so tests can check both the gateway's decision and what the
// gateway forwarded.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

type echo struct {
	Stub    bool              `json:"stub"`
	Service string            `json:"service"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
}

func main() {
	name := os.Getenv("STUB_NAME")
	addrs := strings.Split(os.Getenv("STUB_LISTEN"), ",")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := make(map[string]string, len(r.Header))
		for k := range r.Header {
			headers[strings.ToLower(k)] = r.Header.Get(k)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(echo{Stub: true, Service: name, Method: r.Method, Path: r.URL.RequestURI(), Headers: headers})
	})

	errs := make(chan error, len(addrs))
	for _, addr := range addrs {
		go func() {
			log.Printf("%s stub listening on %s", name, addr)
			errs <- http.ListenAndServe(addr, handler) // #nosec G114 -- test stub
		}()
	}
	log.Fatal(<-errs)
}
