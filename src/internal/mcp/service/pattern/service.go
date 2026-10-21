// Package pattern provides the read-side business logic behind the
// get_pattern and find_related_patterns MCP tools. It combines pattern rows
// from PostgreSQL with knowledge graph context from Neo4j.
package pattern

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	graphrepo "github.com/twistingmercury/librarian/internal/mcp/repository/graph"
	patternrepo "github.com/twistingmercury/librarian/internal/mcp/repository/pattern"
)

// graphContextRelatedLimit caps the related patterns get_pattern shows.
const graphContextRelatedLimit = 10

// GraphContext holds the knowledge graph context for a pattern.
type GraphContext struct {
	RelatedPatterns []RelatedPatternResult
	Concepts        []ConceptResult
}

// RelatedPatternResult represents a pattern discovered through shared concepts.
type RelatedPatternResult struct {
	ID             uuid.UUID
	Name           string
	Relationship   string
	Similarity     float64
	SharedConcepts []string
}

// ConceptResult represents a concept linked to a pattern.
type ConceptResult struct {
	Name string
	Type string
}

// Service reads patterns and their graph context.
type Service struct {
	patternRepo patternrepo.Repository
	graphRepo   graphrepo.Repository
	logger      zerolog.Logger
}

// New creates a pattern Service backed by the given repositories.
func New(patternRepo patternrepo.Repository, graphRepo graphrepo.Repository, logger zerolog.Logger) *Service {
	return &Service{
		patternRepo: patternRepo,
		graphRepo:   graphRepo,
		logger:      logger,
	}
}

// GetPatternWithGraph retrieves a pattern and, if it is enriched, its graph
// context (related patterns and concepts, fetched concurrently). Neo4j
// failures degrade gracefully, returning (pattern, nil, nil).
// Returns an error wrapping patternrepo.ErrNotFound if the pattern does not
// exist.
func (s *Service) GetPatternWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *GraphContext, error) {
	pattern, err := s.patternRepo.Get(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get pattern with graph: %w", err)
	}

	if pattern.EnrichmentStatus != "enriched" {
		return pattern, nil, nil
	}

	graphCtx, err := s.fetchGraphContext(ctx, id)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("pattern_id", id.String()).
			Msg("failed to fetch graph context, returning without graph")
		return pattern, nil, nil
	}

	return pattern, graphCtx, nil
}

// FindRelatedPatterns returns the source pattern's name and the patterns
// related to it via the knowledge graph. The name lookup and the graph query
// run concurrently. Returns an error wrapping patternrepo.ErrNotFound if the
// pattern does not exist.
func (s *Service) FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) (string, []RelatedPatternResult, error) {
	var (
		name    string
		related []graphrepo.RelatedPattern
	)

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		name, err = s.patternRepo.GetName(gCtx, patternID)
		return err
	})
	g.Go(func() error {
		var err error
		related, err = s.graphRepo.FindRelatedPatterns(gCtx, patternID, limit)
		return err
	})
	if err := g.Wait(); err != nil {
		return "", nil, fmt.Errorf("find related: %w", err)
	}

	return name, toRelatedResults(related), nil
}

// fetchGraphContext retrieves related patterns and concepts from Neo4j
// concurrently.
func (s *Service) fetchGraphContext(ctx context.Context, patternID uuid.UUID) (*GraphContext, error) {
	var (
		related  []graphrepo.RelatedPattern
		concepts []graphrepo.Concept
	)

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		related, err = s.graphRepo.FindRelatedPatterns(gCtx, patternID, graphContextRelatedLimit)
		if err != nil {
			return fmt.Errorf("fetching related patterns: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		var err error
		concepts, err = s.graphRepo.GetPatternConcepts(gCtx, patternID)
		if err != nil {
			return fmt.Errorf("fetching concepts: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	conceptResults := make([]ConceptResult, len(concepts))
	for i, c := range concepts {
		conceptResults[i] = ConceptResult{Name: c.Name, Type: c.Type}
	}

	return &GraphContext{
		RelatedPatterns: toRelatedResults(related),
		Concepts:        conceptResults,
	}, nil
}

// toRelatedResults converts graph RELATED_TO matches to service results.
func toRelatedResults(related []graphrepo.RelatedPattern) []RelatedPatternResult {
	results := make([]RelatedPatternResult, len(related))
	for i, r := range related {
		results[i] = RelatedPatternResult{
			ID:             r.ID,
			Name:           r.Name,
			Relationship:   "RELATED_TO",
			Similarity:     r.Similarity,
			SharedConcepts: r.ConceptNames,
		}
	}
	return results
}
