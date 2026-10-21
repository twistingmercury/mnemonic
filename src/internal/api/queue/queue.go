// Package queue defines the Publisher interface for enqueuing enrichment jobs.
package queue

import (
	"context"

	"github.com/google/uuid"
)

// Publisher enqueues enrichment job identifiers for asynchronous processing.
type Publisher interface {
	// Publish enqueues a job identified by jobID. It uses ctx for cancellation
	// and deadline propagation.
	Publish(ctx context.Context, jobID uuid.UUID) error

	// Close releases the underlying connection and channel resources.
	Close() error
}
