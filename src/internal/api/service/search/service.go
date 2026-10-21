// Package search provides semantic similarity search over pattern chunks.
// The REST search endpoint uses this service.
// It coordinates between the embedding service (for query vectorization) and the
// chunk repository (for pgvector similarity search).
package search

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/twistingmercury/librarian/internal/api/metrics"
	chunkrepo "github.com/twistingmercury/librarian/internal/api/repository/chunk"
	"github.com/twistingmercury/librarian/internal/api/service"
	openaisvc "github.com/twistingmercury/librarian/internal/api/service/openai"
)

// Service handles semantic search over patterns.
type Service interface {
	// SearchPatterns generates a query embedding and performs vector similarity search.
	SearchPatterns(ctx context.Context, opts SearchOptions) (*SearchResult, error)
}

// SearchOptions defines the parameters for a semantic search.
type SearchOptions struct {
	Query     string   // Natural language query text
	Limit     int      // Max results (default 10, max 50)
	Threshold float64  // Min similarity (default 0.7)
	Tags      []string // Conjunctive tag filter
	Language  string   // Optional: filter by pattern language
	Domain    string   // Optional: filter by pattern domain
}

// ChunkMatch is a single semantic search hit from a pattern chunk.
type ChunkMatch struct {
	PatternID    uuid.UUID
	PatternName  string
	EntityType   string
	Language     string
	Domain       string
	Tags         []string
	SectionTitle string
	ChunkIndex   int
	Content      string
	Similarity   float64
}

// SearchResult wraps similarity search matches with metadata required by
// the OpenAPI PatternSearchResponse schema.
type SearchResult struct {
	Matches          []*ChunkMatch
	Query            string // Echo of the original query text
	TotalCandidates  int    // Total chunk matches returned (after threshold filtering)
	SearchDurationMs int64  // Wall-clock search time in milliseconds
}

// searchService implements the Service interface.
type searchService struct {
	embeddingSvc openaisvc.EmbeddingService
	chunkRepo    chunkrepo.Repository
	metrics      *metrics.Pattern
	logger       zerolog.Logger
}

// New creates a new search Service backed by the given dependencies.
// patternMetrics may be nil, in which case vector search is not recorded.
func New(
	embeddingSvc openaisvc.EmbeddingService,
	chunkRepo chunkrepo.Repository,
	patternMetrics *metrics.Pattern,
	logger zerolog.Logger,
) Service {
	return &searchService{
		embeddingSvc: embeddingSvc,
		chunkRepo:    chunkRepo,
		metrics:      patternMetrics,
		logger:       logger,
	}
}

// SearchPatterns generates a query embedding and performs vector similarity search.
func (s *searchService) SearchPatterns(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	start := time.Now()

	// 1. Generate query embedding.
	embedding, err := s.embeddingSvc.Embed(ctx, opts.Query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", service.ErrServiceUnavailable, err)
	}

	// 2. Perform chunk-based similarity search.
	simOpts := chunkrepo.SimilarityOptions{
		MinSimilarity: opts.Threshold,
		MaxResults:    opts.Limit,
		Language:      opts.Language,
		Domain:        opts.Domain,
		Tags:          opts.Tags,
	}

	searchStart := time.Now()
	rawMatches, err := s.chunkRepo.FindSimilar(ctx, embedding, simOpts)
	if err != nil {
		return nil, fmt.Errorf("find similar chunks: %w", err)
	}
	if s.metrics != nil {
		s.metrics.RecordQuery(ctx, "pgvector", time.Since(searchStart), len(rawMatches))
	}

	matches := make([]*ChunkMatch, len(rawMatches))
	for i, m := range rawMatches {
		matches[i] = &ChunkMatch{
			PatternID:    m.PatternID,
			PatternName:  m.PatternName,
			EntityType:   m.EntityType,
			Language:     m.Language,
			Domain:       m.Domain,
			Tags:         m.Tags,
			SectionTitle: m.SectionTitle,
			ChunkIndex:   m.ChunkIndex,
			Content:      m.Content,
			Similarity:   m.Similarity,
		}
	}

	return &SearchResult{
		Matches:          matches,
		Query:            opts.Query,
		TotalCandidates:  len(matches),
		SearchDurationMs: time.Since(start).Milliseconds(),
	}, nil
}
