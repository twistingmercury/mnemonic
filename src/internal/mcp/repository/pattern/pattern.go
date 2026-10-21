package pattern

import (
	"time"

	"github.com/google/uuid"
)

// Pattern holds the pattern columns the MCP tools read. Which fields are set
// depends on the query: Get fills the fields get_pattern shows, GetByIDs fills
// only the metadata used to filter graph-expanded search results.
type Pattern struct {
	// ID is the unique UUID identifier for the pattern.
	ID uuid.UUID

	// Name is the unique human-readable name for the pattern (e.g., "go-error-handling").
	Name string

	// Content is the pattern's markdown text. Set by Get only.
	Content string

	// Tags is a list of categorization tags (e.g., ["golang", "best-practices"]),
	// stored as JSONB.
	Tags []string

	// Language is the primary programming language (e.g., "go", "agnostic"). Set by GetByIDs only.
	Language string

	// Domain is the technical domain (e.g., "backend", "testing"). Set by GetByIDs only.
	Domain string

	// EnrichmentStatus is the processing state: "pending", "enriched", or "failed". Set by Get only.
	EnrichmentStatus string

	// EnrichmentError contains the error message if enrichment failed. Set by Get only.
	EnrichmentError *string

	// EnrichedAt is when enrichment completed successfully. Set by Get only.
	EnrichedAt *time.Time
}
