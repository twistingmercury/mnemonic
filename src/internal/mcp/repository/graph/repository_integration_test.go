//go:build integration

package graph_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian-mcp/internal/metrics"
	"github.com/twistingmercury/librarian-mcp/internal/repository/graph"
	"go.opentelemetry.io/otel/metric/noop"
)

const (
	// testIntegrationPatternPrefix is used to identify test-created patterns for cleanup.
	testIntegrationPatternPrefix = "test-integration-pattern-"

	// testIntegrationConceptPrefix is used to identify test-created concepts for cleanup.
	testIntegrationConceptPrefix = "test-integration-concept-"

	// conceptNameConstraint is the uniqueness constraint the shipped schema puts on Concept.name.
	conceptNameConstraint = "concept_name_unique"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// setupNeo4j connects to Neo4j, skipping the test if it is unavailable, and
// removes test data before and after the test. The graph is seeded with
// direct Cypher because writes belong to mnemonic-api and the enricher.
func setupNeo4j(t *testing.T) (*seeder, graph.Repository) {
	t.Helper()

	database := envOr("NEO4J_DATABASE", "neo4j")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	driver, err := neo4j.NewDriverWithContext(
		envOr("NEO4J_URI", "bolt://localhost:17688"),
		neo4j.BasicAuth(envOr("NEO4J_USER", "neo4j"), envOr("NEO4J_PASSWORD", "mnemonic_dev"), ""),
	)
	if err != nil {
		t.Skipf("skipping integration test: unable to create Neo4j driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		t.Skipf("skipping integration test: Neo4j connectivity check failed: %v", err)
	}

	s := &seeder{t: t, driver: driver, database: database}
	s.cleanup()
	t.Cleanup(func() {
		s.cleanup()
		_ = driver.Close(context.Background())
	})

	dbMetrics, err := metrics.NewDatabase(noop.NewMeterProvider().Meter("test"))
	require.NoError(t, err)
	return s, graph.NewRepository(driver, database, dbMetrics)
}

type seeder struct {
	t        *testing.T
	driver   neo4j.DriverWithContext
	database string
}

func (s *seeder) run(cypher string, params map[string]any) {
	s.t.Helper()
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.database})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	require.NoError(s.t, err)
}

func (s *seeder) cleanup() {
	s.run(`MATCH (p:Pattern) WHERE p.name STARTS WITH $prefix DETACH DELETE p`,
		map[string]any{"prefix": testIntegrationPatternPrefix})
	s.run(`MATCH (c:Concept) WHERE c.name STARTS WITH $prefix DETACH DELETE c`,
		map[string]any{"prefix": testIntegrationConceptPrefix})
}

// withoutConceptNameConstraint simulates a store with no uniqueness
// constraints, like AWS Neptune, where duplicate Concept nodes can exist. It
// drops the constraint if present; the cleanup registered here deletes the test
// data first, because the constraint cannot be recreated while duplicates
// remain, and then restores the constraint only if it existed beforehand.
func (s *seeder) withoutConceptNameConstraint() {
	s.t.Helper()
	existed := s.constraintExists(conceptNameConstraint)

	s.t.Cleanup(func() {
		s.cleanup()
		if !existed {
			return
		}

		s.run(`CREATE CONSTRAINT concept_name_unique IF NOT EXISTS FOR (c:Concept) REQUIRE c.name IS UNIQUE`, nil)
	})

	s.run(`DROP CONSTRAINT concept_name_unique IF EXISTS`, nil)
}

func (s *seeder) constraintExists(name string) bool {
	s.t.Helper()
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.database})
	defer func() { _ = session.Close(ctx) }()

	res, err := session.Run(ctx, `SHOW CONSTRAINTS YIELD name WHERE name = $name RETURN count(*) AS cnt`,
		map[string]any{"name": name})
	require.NoError(s.t, err)
	rec, err := res.Single(ctx)
	require.NoError(s.t, err)
	n, _, err := neo4j.GetRecordValue[int64](rec, "cnt")
	require.NoError(s.t, err)
	return n > 0
}

// pattern creates a Pattern node mentioned by the given concepts.
func (s *seeder) pattern(suffix string, concepts ...string) uuid.UUID {
	s.t.Helper()
	id := uuid.New()
	names := make([]string, len(concepts))
	for i, c := range concepts {
		names[i] = testIntegrationConceptPrefix + c
	}
	s.run(`MERGE (p:Pattern {id: $id}) SET p.name = $name
	       WITH p UNWIND $concepts AS conceptName
	       MERGE (c:Concept {name: conceptName}) ON CREATE SET c.type = 'technology'
	       MERGE (c)-[:MENTIONED_IN]->(p)`,
		map[string]any{"id": id.String(), "name": testIntegrationPatternPrefix + suffix, "concepts": names})
	return id
}

// patternOwnConcepts is pattern with a CREATE per concept, so the pattern gets
// Concept nodes of its own even when another pattern already has concepts of
// the same name. Without a uniqueness constraint (see
// withoutConceptNameConstraint) that is how duplicate Concept nodes arise.
func (s *seeder) patternOwnConcepts(suffix string, concepts ...string) uuid.UUID {
	s.t.Helper()
	id := uuid.New()
	names := make([]string, len(concepts))
	for i, c := range concepts {
		names[i] = testIntegrationConceptPrefix + c
	}
	s.run(`CREATE (p:Pattern {id: $id, name: $name})
	       WITH p UNWIND $concepts AS conceptName
	       CREATE (:Concept {name: conceptName, type: 'technology'})-[:MENTIONED_IN]->(p)`,
		map[string]any{"id": id.String(), "name": testIntegrationPatternPrefix + suffix, "concepts": names})
	return id
}

// relate creates a RELATED_TO edge between two patterns.
func (s *seeder) relate(a, b uuid.UUID, similarity float64) {
	s.t.Helper()
	s.run(`MATCH (a:Pattern {id: $a}), (b:Pattern {id: $b})
	       MERGE (a)-[r:RELATED_TO]-(b) SET r.similarity = $similarity`,
		map[string]any{"a": a.String(), "b": b.String(), "similarity": similarity})
}

func TestIntegration_FindRelatedPatterns(t *testing.T) {
	s, repo := setupNeo4j(t)
	ctx := context.Background()

	// pA shares 3 concepts with pB and 1 with pC; pD shares none.
	pA := s.pattern("related-a", "go", "concurrency", "channels", "error-handling")
	pB := s.pattern("related-b", "go", "concurrency", "channels")
	pC := s.pattern("related-c", "go", "testing")
	pD := s.pattern("related-unique", "python", "django")
	s.relate(pA, pB, 0.75)
	s.relate(pA, pC, 0.25)
	s.relate(pB, pC, 1.0/3)

	t.Run("finds patterns ordered by similarity with shared concepts", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)
		require.Len(t, results, 2)

		assert.Equal(t, pB, results[0].ID)
		assert.Equal(t, testIntegrationPatternPrefix+"related-b", results[0].Name)
		assert.InDelta(t, 0.75, results[0].Similarity, 1e-9)
		assert.ElementsMatch(t, []string{
			testIntegrationConceptPrefix + "go",
			testIntegrationConceptPrefix + "concurrency",
			testIntegrationConceptPrefix + "channels",
		}, results[0].ConceptNames)

		assert.Equal(t, pC, results[1].ID)
		assert.Equal(t, []string{testIntegrationConceptPrefix + "go"}, results[1].ConceptNames)
	})

	t.Run("does not include the source pattern itself", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)
		for _, r := range results {
			assert.NotEqual(t, pA, r.ID)
		}
	})

	t.Run("returns no results for pattern without edges", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pD, 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pA, 1)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, pB, results[0].ID)
	})

	t.Run("returns empty for nonexistent pattern", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, uuid.New(), 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestIntegration_FindRelatedPatternsSharedConceptNames(t *testing.T) {
	s, repo := setupNeo4j(t)
	s.withoutConceptNameConstraint()
	ctx := context.Background()

	t.Run("matches by name across duplicate Concept nodes", func(t *testing.T) {
		// Earlier subtests leave same-named Concept nodes behind; MERGE would
		// attach to all of them and skew the results.
		s.cleanup()

		pA := s.patternOwnConcepts("dup-a", "retry", "jitter", "only-a")
		pB := s.patternOwnConcepts("dup-b", "retry", "jitter", "only-b")
		s.relate(pA, pB, 0.67)

		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, results, 1)
		assert.InDelta(t, 0.67, results[0].Similarity, 1e-9)
		assert.ElementsMatch(t, []string{
			testIntegrationConceptPrefix + "retry",
			testIntegrationConceptPrefix + "jitter",
		}, results[0].ConceptNames)
	})

	t.Run("shared Concept nodes", func(t *testing.T) {
		s.cleanup()

		pA := s.pattern("same-a", "retry", "jitter", "only-a")
		pB := s.pattern("same-b", "retry", "jitter")
		s.relate(pA, pB, 0.67)

		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, results, 1)
		assert.ElementsMatch(t, []string{
			testIntegrationConceptPrefix + "retry",
			testIntegrationConceptPrefix + "jitter",
		}, results[0].ConceptNames)
	})

	t.Run("no shared concepts", func(t *testing.T) {
		s.cleanup()

		pA := s.patternOwnConcepts("none-a", "alpha")
		pB := s.patternOwnConcepts("none-b", "beta")
		s.relate(pA, pB, 0.4)

		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, results, 1)
		assert.Empty(t, results[0].ConceptNames)
	})

	t.Run("related pattern with no concepts at all", func(t *testing.T) {
		s.cleanup()

		pA := s.patternOwnConcepts("bare-a", "alpha")
		pB := s.patternOwnConcepts("bare-b")
		s.relate(pA, pB, 0.4)

		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, results, 1)
		assert.Equal(t, pB, results[0].ID)
		assert.Empty(t, results[0].ConceptNames)
	})

	t.Run("source pattern with no concepts", func(t *testing.T) {
		s.cleanup()

		pA := s.patternOwnConcepts("srcbare-a")
		pB := s.patternOwnConcepts("srcbare-b", "beta")
		s.relate(pA, pB, 0.4)

		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, results, 1)
		assert.Empty(t, results[0].ConceptNames)
	})

	t.Run("duplicate concept nodes on both sides list each name once", func(t *testing.T) {
		s.cleanup()

		pA := s.patternOwnConcepts("twice-a", "retry", "retry")
		pB := s.patternOwnConcepts("twice-b", "retry", "retry", "retry")
		s.relate(pA, pB, 0.5)

		results, err := repo.FindRelatedPatterns(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, results, 1)
		assert.Equal(t, []string{testIntegrationConceptPrefix + "retry"}, results[0].ConceptNames)
	})
}

func TestIntegration_GetPatternConcepts(t *testing.T) {
	s, repo := setupNeo4j(t)
	ctx := context.Background()

	t.Run("returns concepts ordered by name", func(t *testing.T) {
		id := s.pattern("get-concepts", "gamma", "alpha", "beta")

		concepts, err := repo.GetPatternConcepts(ctx, id)
		require.NoError(t, err)
		require.Len(t, concepts, 3)
		assert.Equal(t, testIntegrationConceptPrefix+"alpha", concepts[0].Name)
		assert.Equal(t, testIntegrationConceptPrefix+"beta", concepts[1].Name)
		assert.Equal(t, testIntegrationConceptPrefix+"gamma", concepts[2].Name)
		for _, c := range concepts {
			assert.Equal(t, "technology", c.Type)
		}
	})

	t.Run("returns empty for pattern with no concepts", func(t *testing.T) {
		id := s.pattern("get-concepts-empty")

		concepts, err := repo.GetPatternConcepts(ctx, id)
		require.NoError(t, err)
		assert.Empty(t, concepts)
	})

	t.Run("returns empty for nonexistent pattern", func(t *testing.T) {
		concepts, err := repo.GetPatternConcepts(ctx, uuid.New())
		require.NoError(t, err)
		assert.Empty(t, concepts)
	})
}

func TestIntegration_ContextCancellation(t *testing.T) {
	_, repo := setupNeo4j(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.FindRelatedPatterns(ctx, uuid.New(), 10)
	assert.Error(t, err)
	_, err = repo.GetPatternConcepts(ctx, uuid.New())
	assert.Error(t, err)
}
