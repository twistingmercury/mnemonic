package pattern

import (
	"time"

	"github.com/google/uuid"
)

// Pattern represents a context pattern stored in the database.
type Pattern struct {
	// ID is the unique UUID identifier for the pattern.
	ID uuid.UUID `db:"id"`

	// Name is the unique human-readable name for the pattern (e.g., "go-error-handling").
	Name string `db:"name"`

	// Description is an optional description of when/how to use this pattern.
	Description *string `db:"description"`

	// Content is the actual context text that will be injected into prompts (up to 100,000 bytes, enforced by the API handler).
	Content string `db:"content"`

	// Tags is a list of categorization tags (e.g., ["golang", "best-practices"]).
	// Stored as JSONB in the database and unmarshaled during retrieval.
	Tags []string `db:"-"`

	// EntityType is the pattern category (e.g., "go-pattern", "e2e-testing").
	EntityType string `db:"entity_type"`

	// Language is the primary programming language (e.g., "go", "agnostic", "shell").
	Language string `db:"language"`

	// Domain is the technical domain (e.g., "backend", "api-design", "testing").
	Domain string `db:"domain"`

	// Version is the optional target version string (e.g., "Go 1.21+").
	Version *string `db:"version"`

	// RelatedPatterns is a list of related pattern entity names, stored as JSONB.
	RelatedPatterns []string `db:"-"`

	// EnrichmentStatus is the processing state: "pending", "enriched", or "failed".
	EnrichmentStatus string `db:"enrichment_status"`

	// EnrichmentError contains the error message if enrichment failed (nil otherwise).
	EnrichmentError *string `db:"enrichment_error"`

	// EnrichedAt is the timestamp when enrichment completed successfully (nil if pending or failed).
	EnrichedAt *time.Time `db:"enriched_at"`

	// CreatedAt is the timestamp when the pattern was created.
	CreatedAt time.Time `db:"created_at"`

	// UpdatedAt is the timestamp when the pattern was last modified.
	UpdatedAt time.Time `db:"updated_at"`
}

// Filter defines filtering options for pattern queries.
type Filter struct {
	// Tags filters patterns that have any of these tags.
	Tags []string

	// EnrichmentStatus filters patterns by enrichment status.
	EnrichmentStatus string

	// SearchQuery performs full-text search in name/description.
	SearchQuery string

	// Language filters patterns by programming language.
	Language string

	// Domain filters patterns by technical domain.
	Domain string

	// EntityType filters patterns by category.
	EntityType string
}
