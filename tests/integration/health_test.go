package integration

import (
	"net/http"
	"slices"
	"testing"
)

// TestServicesHealthy checks each service in this stack reports every
// dependency it is wired to, and that all of them, the OpenAI stub included,
// are OK.
func TestServicesHealthy(t *testing.T) {
	t.Parallel()

	services := []struct {
		name string
		url  string
		deps []string
	}{
		{"api", apiURL, []string{"PostgreSQL", "Neo4j", "OpenAI embedding model", "RabbitMQ"}},
		{"mcp", mcpOpsURL, []string{"PostgreSQL", "Neo4j", "OpenAI embedding model"}},
		{"enricher", enricherURL, []string{"PostgreSQL", "Neo4j", "OpenAI embedding model", "OpenAI extraction model"}},
	}

	for _, svc := range services {
		t.Run(svc.name, func(t *testing.T) {
			t.Parallel()

			var health struct {
				Status       string `json:"status"`
				Dependencies []struct {
					Name    string `json:"name"`
					Status  string `json:"status"`
					Message string `json:"message"`
				} `json:"dependencies"`
			}
			mustCall(t, http.MethodGet, svc.url+"/health", nil, http.StatusOK, &health)

			var got []string
			for _, d := range health.Dependencies {
				got = append(got, d.Name)
				if d.Status != "OK" {
					t.Errorf("dependency %q is %s: %s", d.Name, d.Status, d.Message)
				}
			}
			slices.Sort(got)
			want := slices.Sorted(slices.Values(svc.deps))
			if !slices.Equal(got, want) {
				t.Errorf("dependencies = %v, want %v", got, want)
			}
			if health.Status != "OK" {
				t.Errorf("status = %q, want OK", health.Status)
			}

			var version struct {
				Version string `json:"version"`
			}
			mustCall(t, http.MethodGet, svc.url+"/version", nil, http.StatusOK, &version)
			if version.Version == "" {
				t.Error("version is empty")
			}
		})
	}
}
