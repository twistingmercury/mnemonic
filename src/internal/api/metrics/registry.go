package metrics

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Registry holds all metric instruments for the application.
// It provides centralized access to domain-specific metrics.
type Registry struct {
	Patterns *Pattern
	Database *Database
}

// NewRegistry creates all metric instruments using the provided meter.
// It initializes pattern and database metrics.
func NewRegistry(meter metric.Meter) (*Registry, error) {
	patterns, err := NewPattern(meter)
	if err != nil {
		return nil, fmt.Errorf("pattern metrics: %w", err)
	}

	database, err := NewDatabase(meter)
	if err != nil {
		return nil, fmt.Errorf("database metrics: %w", err)
	}

	return &Registry{
		Patterns: patterns,
		Database: database,
	}, nil
}
