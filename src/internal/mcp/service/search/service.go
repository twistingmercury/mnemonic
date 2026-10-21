// Package search provides the semantic similarity search behind the MCP
// search_patterns tool. It coordinates the embedding service (query
// vectorization), the chunk repository (pgvector similarity search) and the
// graph repository (expansion through RELATED_TO edges).
package search

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/twistingmercury/mnemonic/internal/mcp/metrics"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/chunk"
	graphrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/graph"
	patternrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/pattern"
	openaisvc "github.com/twistingmercury/mnemonic/internal/mcp/service/openai"
)

// SearchOptions defines the parameters for a semantic search.
type SearchOptions struct {
	Query     string   // Natural language query text
	Limit     int      // Max results (default 10, max 50)
	Threshold float64  // Min similarity (default 0.5)
	Tags      []string // Conjunctive tag filter
	Language  string   // Optional: filter by pattern language
	Domain    string   // Optional: filter by pattern domain
}

// ChunkMatch is a single semantic search hit from a pattern chunk.
type ChunkMatch = chunkrepo.Match

// GraphMatch is a pattern discovered via Neo4j graph traversal from a vector search seed.
type GraphMatch struct {
	PatternID       uuid.UUID
	PatternName     string
	Similarity      float64  // concept-overlap similarity from RELATED_TO edge
	ConceptNames    []string // shared concept names from graph relationship
	SeedPatternName string
}

// SearchResult wraps similarity search matches with the original query.
type SearchResult struct {
	Matches      []*ChunkMatch
	GraphMatches []*GraphMatch // graph-expanded results (nil if unavailable)
	Query        string        // Echo of the original query text
}

const (
	// graphSeedCount is the number of top vector-match patterns used as graph expansion seeds.
	graphSeedCount = 3

	// graphResultsPerSeed is the max related patterns fetched per seed from Neo4j.
	graphResultsPerSeed = 5

	// graphMatchCap is the maximum graph matches returned in a single search response.
	graphMatchCap = 5
)

// Service performs semantic search over pattern chunks with graph expansion.
type Service struct {
	embeddingSvc openaisvc.EmbeddingService
	patternRepo  patternrepo.Repository
	chunkRepo    chunkrepo.Repository
	graphRepo    graphrepo.Repository
	metrics      *metrics.Pattern
	logger       zerolog.Logger
}

// New creates a new search Service backed by the given dependencies.
func New(
	embeddingSvc openaisvc.EmbeddingService,
	patternRepo patternrepo.Repository,
	chunkRepo chunkrepo.Repository,
	graphRepo graphrepo.Repository,
	patternMetrics *metrics.Pattern,
	logger zerolog.Logger,
) *Service {
	return &Service{
		embeddingSvc: embeddingSvc,
		patternRepo:  patternRepo,
		chunkRepo:    chunkRepo,
		graphRepo:    graphRepo,
		metrics:      patternMetrics,
		logger:       logger,
	}
}

// SearchPatterns generates a query embedding, performs vector similarity
// search and expands the top results through the knowledge graph.
func (s *Service) SearchPatterns(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	embedding, err := s.embeddingSvc.Embed(ctx, opts.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	searchStart := time.Now()
	matches, err := s.chunkRepo.FindSimilar(ctx, embedding, chunkrepo.SimilarityOptions{
		MinSimilarity: opts.Threshold,
		MaxResults:    opts.Limit,
		Language:      opts.Language,
		Domain:        opts.Domain,
		Tags:          opts.Tags,
	})
	if err != nil {
		return nil, fmt.Errorf("find similar chunks: %w", err)
	}
	s.metrics.RecordQuery(ctx, "pgvector", time.Since(searchStart), len(matches))

	return &SearchResult{
		Matches:      matches,
		GraphMatches: s.expandViaGraph(ctx, matches, opts),
		Query:        opts.Query,
	}, nil
}

// seedPattern holds the identifying fields for a top vector-result pattern.
type seedPattern struct {
	id         uuid.UUID
	name       string
	similarity float64
}

// expandViaGraph performs graph-based expansion from the top vector seeds.
// Returns nil (no error surfaced to caller) when graph is unavailable or fails.
func (s *Service) expandViaGraph(ctx context.Context, matches []*ChunkMatch, opts SearchOptions) []*GraphMatch {
	if len(matches) == 0 {
		return nil
	}

	// Build per-pattern best similarity from vector results.
	bestSim := make(map[uuid.UUID]seedPattern)
	for _, m := range matches {
		if existing, ok := bestSim[m.PatternID]; !ok || m.Similarity > existing.similarity {
			bestSim[m.PatternID] = seedPattern{
				id:         m.PatternID,
				name:       m.PatternName,
				similarity: m.Similarity,
			}
		}
	}

	// Sort seeds by similarity descending; take the top graphSeedCount.
	seeds := make([]seedPattern, 0, len(bestSim))
	for _, sp := range bestSim {
		seeds = append(seeds, sp)
	}
	slices.SortFunc(seeds, func(a, b seedPattern) int {
		return cmp.Compare(b.similarity, a.similarity)
	})
	if len(seeds) > graphSeedCount {
		seeds = seeds[:graphSeedCount]
	}

	// Query the graph for every seed concurrently; a failed seed is logged and
	// skipped.
	perSeed := make([][]graphrepo.RelatedPattern, len(seeds))
	var wg sync.WaitGroup
	for i, seed := range seeds {
		wg.Go(func() {
			related, err := s.graphRepo.FindRelatedPatterns(ctx, seed.id, graphResultsPerSeed)
			if err != nil {
				s.logger.Warn().Err(err).Str("seed_pattern_id", seed.id.String()).Msg("graph expansion failed for seed; continuing with remaining seeds")
				return
			}
			perSeed[i] = related
		})
	}
	wg.Wait()

	// Collect across all seeds with cross-seed dedup, keeping the entry with
	// the highest Similarity when the same pattern appears from multiple seeds.
	type graphEntry struct {
		related graphrepo.RelatedPattern
		seed    seedPattern
	}
	best := make(map[uuid.UUID]graphEntry)

	for i, seed := range seeds {
		for _, rp := range perSeed[i] {
			if existing, ok := best[rp.ID]; !ok || rp.Similarity > existing.related.Similarity {
				best[rp.ID] = graphEntry{related: rp, seed: seed}
			}
		}
	}

	// Exclude patterns already present in the vector result set.
	filtered := make([]graphEntry, 0, len(best))
	for id, entry := range best {
		if _, inVector := bestSim[id]; !inVector {
			filtered = append(filtered, entry)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	// Batch-fetch metadata for remaining graph candidates.
	candidateIDs := make([]uuid.UUID, len(filtered))
	for i, entry := range filtered {
		candidateIDs[i] = entry.related.ID
	}

	patterns, err := s.patternRepo.GetByIDs(ctx, candidateIDs)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to fetch graph-expanded pattern metadata; returning vector-only results")
		return nil
	}

	// Index metadata by ID for O(1) lookup.
	meta := make(map[uuid.UUID]*patternrepo.Pattern, len(patterns))
	for _, p := range patterns {
		meta[p.ID] = p
	}

	// Post-filter by language / domain / tags and build GraphMatch slice.
	graphMatches := make([]*GraphMatch, 0, len(filtered))
	for _, entry := range filtered {
		p, ok := meta[entry.related.ID]
		if !ok {
			continue
		}
		if opts.Language != "" && p.Language != opts.Language && p.Language != "agnostic" {
			continue
		}
		if opts.Domain != "" && p.Domain != opts.Domain {
			continue
		}
		if !hasAllTags(p.Tags, opts.Tags) {
			continue
		}
		graphMatches = append(graphMatches, &GraphMatch{
			PatternID:       p.ID,
			PatternName:     p.Name,
			Similarity:      entry.related.Similarity,
			ConceptNames:    entry.related.ConceptNames,
			SeedPatternName: entry.seed.name,
		})
	}

	if len(graphMatches) == 0 {
		return nil
	}

	// Sort by similarity descending, then cap.
	slices.SortFunc(graphMatches, func(a, b *GraphMatch) int {
		return cmp.Compare(b.Similarity, a.Similarity)
	})
	if len(graphMatches) > graphMatchCap {
		graphMatches = graphMatches[:graphMatchCap]
	}

	return graphMatches
}

// hasAllTags reports whether tags contains every element of required (conjunctive filter).
func hasAllTags(tags []string, required []string) bool {
	for _, r := range required {
		if !slices.Contains(tags, r) {
			return false
		}
	}
	return true
}
