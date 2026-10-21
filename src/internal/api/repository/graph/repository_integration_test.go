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
	"github.com/twistingmercury/mnemonic/internal/api/repository/graph"
)

const (
	// testIntegrationPatternPrefix is used to identify test-created patterns for cleanup.
	testIntegrationPatternPrefix = "test-integration-pattern-"

	// testIntegrationConceptPrefix is used to identify test-created concepts for cleanup.
	testIntegrationConceptPrefix = "test-integration-concept-"

	// conceptNameConstraint is the uniqueness constraint the shipped schema puts on Concept.name.
	conceptNameConstraint = "concept_name_unique"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func neo4jDatabase() string { return envOr("NEO4J_DATABASE", "neo4j") }

// setupNeo4j connects to the test Neo4j instance and returns the driver and a
// graph.Repository, skipping the test if Neo4j is unavailable. Test data is
// removed before and after the test.
func setupNeo4j(t *testing.T) (neo4j.DriverWithContext, graph.Repository) {
	t.Helper()

	uri := envOr("NEO4J_URI", "bolt://localhost:17688")
	user := envOr("NEO4J_USER", "neo4j")
	password := envOr("NEO4J_PASSWORD", "mnemonic_dev")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		t.Skipf("skipping integration test: unable to create Neo4j driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		t.Skipf("skipping integration test: Neo4j connectivity check failed: %v", err)
	}

	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() {
		cleanupNeo4jTestData(t, driver)
		_ = driver.Close(context.Background())
	})

	return driver, graph.NewRepository(driver, neo4jDatabase(), nil)
}

// cypher runs a statement directly against the test database.
func cypher(t *testing.T, driver neo4j.DriverWithContext, query string, params map[string]any) []*neo4j.Record {
	t.Helper()
	res, err := neo4j.ExecuteQuery(context.Background(), driver, query, params,
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(neo4jDatabase()))
	require.NoError(t, err)
	return res.Records
}

// cleanupNeo4jTestData removes all test-created nodes and relationships.
func cleanupNeo4jTestData(t *testing.T, driver neo4j.DriverWithContext) {
	t.Helper()
	cypher(t, driver, `MATCH (p:Pattern) WHERE p.name STARTS WITH $prefix DETACH DELETE p`,
		map[string]any{"prefix": testIntegrationPatternPrefix})
	cypher(t, driver, `MATCH (c:Concept) WHERE c.name STARTS WITH $prefix DETACH DELETE c`,
		map[string]any{"prefix": testIntegrationConceptPrefix})
}

// withoutConceptNameConstraint simulates a store with no uniqueness
// constraints, like AWS Neptune, where duplicate Concept nodes can exist. It
// drops the constraint if present; the cleanup registered here deletes the test
// data first, because the constraint cannot be recreated while duplicates
// remain, and then restores the constraint only if it existed beforehand.
func withoutConceptNameConstraint(t *testing.T, driver neo4j.DriverWithContext) {
	t.Helper()
	existed := count(t, driver, `SHOW CONSTRAINTS YIELD name WHERE name = $name RETURN count(*) AS cnt`,
		map[string]any{"name": conceptNameConstraint}) > 0

	t.Cleanup(func() {
		cleanupNeo4jTestData(t, driver)
		if !existed {
			return
		}

		cypher(t, driver, `CREATE CONSTRAINT concept_name_unique IF NOT EXISTS FOR (c:Concept) REQUIRE c.name IS UNIQUE`, nil)
	})

	cypher(t, driver, `DROP CONSTRAINT concept_name_unique IF EXISTS`, nil)
}

// seedPattern creates a Pattern node linked to the named concepts, standing in
// for the enricher. Concept names get the test prefix.
func seedPattern(t *testing.T, driver neo4j.DriverWithContext, suffix string, concepts ...string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	names := make([]string, len(concepts))
	for i, c := range concepts {
		names[i] = testIntegrationConceptPrefix + c
	}
	cypher(t, driver,
		`CREATE (p:Pattern {id: $id, name: $name})
		 WITH p
		 UNWIND $concepts AS cname
		 MERGE (c:Concept {name: cname})
		 ON CREATE SET c.type = 'technology'
		 MERGE (c)-[:MENTIONED_IN]->(p)`,
		map[string]any{"id": id.String(), "name": testIntegrationPatternPrefix + suffix, "concepts": names})
	return id
}

// seedPatternOwnConcepts is seedPattern with a CREATE per concept, so the
// pattern gets Concept nodes of its own even when another pattern already has
// concepts of the same name. Without a uniqueness constraint (see
// withoutConceptNameConstraint) that is how duplicate Concept nodes arise.
func seedPatternOwnConcepts(t *testing.T, driver neo4j.DriverWithContext, suffix string, concepts ...string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	names := make([]string, len(concepts))
	for i, c := range concepts {
		names[i] = testIntegrationConceptPrefix + c
	}
	cypher(t, driver,
		`CREATE (p:Pattern {id: $id, name: $name})
		 WITH p
		 UNWIND $concepts AS cname
		 CREATE (:Concept {name: cname, type: 'technology'})-[:MENTIONED_IN]->(p)`,
		map[string]any{"id": id.String(), "name": testIntegrationPatternPrefix + suffix, "concepts": names})
	return id
}

// relate creates a RELATED_TO edge between two patterns, as the enricher would.
func relate(t *testing.T, driver neo4j.DriverWithContext, a, b uuid.UUID, similarity float64) {
	t.Helper()
	cypher(t, driver,
		`MATCH (a:Pattern {id: $a}), (b:Pattern {id: $b})
		 CREATE (a)-[:RELATED_TO {similarity: $sim}]->(b)`,
		map[string]any{"a": a.String(), "b": b.String(), "sim": similarity})
}

func count(t *testing.T, driver neo4j.DriverWithContext, query string, params map[string]any) int64 {
	t.Helper()
	recs := cypher(t, driver, query, params)
	require.Len(t, recs, 1)
	n, _, err := neo4j.GetRecordValue[int64](recs[0], "cnt")
	require.NoError(t, err)
	return n
}

func countConceptsByPrefix(t *testing.T, driver neo4j.DriverWithContext, prefix string) int64 {
	return count(t, driver, `MATCH (c:Concept) WHERE c.name STARTS WITH $prefix RETURN count(c) AS cnt`,
		map[string]any{"prefix": prefix})
}

func countConceptRelationships(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) int64 {
	return count(t, driver, `MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $id}) RETURN count(c) AS cnt`,
		map[string]any{"id": patternID.String()})
}

func patternExists(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) bool {
	return count(t, driver, `MATCH (p:Pattern {id: $id}) RETURN count(p) AS cnt`,
		map[string]any{"id": patternID.String()}) > 0
}

// --- Tests ---

func TestIntegration_DeletePattern(t *testing.T) {
	driver, repo := setupNeo4j(t)
	ctx := context.Background()

	t.Run("removes pattern and its concept relationships", func(t *testing.T) {
		id := seedPattern(t, driver, "delete-with-concepts", "del-c1", "del-c2")
		require.Equal(t, int64(2), countConceptRelationships(t, driver, id))

		require.NoError(t, repo.DeletePattern(ctx, id))

		assert.False(t, patternExists(t, driver, id))
		assert.Equal(t, int64(0), countConceptRelationships(t, driver, id))
	})

	t.Run("nonexistent pattern is not an error", func(t *testing.T) {
		assert.NoError(t, repo.DeletePattern(ctx, uuid.New()))
	})
}

func TestIntegration_GetPatternGraph(t *testing.T) {
	driver, repo := setupNeo4j(t)
	withoutConceptNameConstraint(t, driver)
	ctx := context.Background()

	t.Run("returns related patterns and concepts ordered by name", func(t *testing.T) {
		pA := seedPattern(t, driver, "graph-a", "gamma", "alpha", "beta")
		pB := seedPattern(t, driver, "graph-b", "alpha")
		pC := seedPattern(t, driver, "graph-c", "beta")
		relate(t, driver, pA, pB, 0.5)
		relate(t, driver, pC, pA, 0.9)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, g.Concepts, 3)
		assert.Equal(t, testIntegrationConceptPrefix+"alpha", g.Concepts[0].Name)
		assert.Equal(t, testIntegrationConceptPrefix+"beta", g.Concepts[1].Name)
		assert.Equal(t, testIntegrationConceptPrefix+"gamma", g.Concepts[2].Name)

		require.Len(t, g.RelatedPatterns, 2)
		assert.Equal(t, pC, g.RelatedPatterns[0].ID, "ordered by similarity")
		assert.Equal(t, []string{testIntegrationConceptPrefix + "beta"}, g.RelatedPatterns[0].ConceptNames)
		assert.Equal(t, pB, g.RelatedPatterns[1].ID)

		g, err = repo.GetPatternGraph(ctx, pA, 1)
		require.NoError(t, err)
		assert.Len(t, g.RelatedPatterns, 1, "respects the related limit")
		assert.Len(t, g.Concepts, 3, "the limit does not apply to concepts")
	})

	t.Run("shared concepts match by name across duplicate Concept nodes", func(t *testing.T) {
		// Earlier subtests leave same-named Concept nodes behind; MERGE would
		// attach to all of them and skew the counts.
		cleanupNeo4jTestData(t, driver)

		pA := seedPatternOwnConcepts(t, driver, "dup-a", "retry", "jitter", "only-a")
		pB := seedPatternOwnConcepts(t, driver, "dup-b", "retry", "jitter", "only-b")
		relate(t, driver, pA, pB, 0.67)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, g.RelatedPatterns, 1)
		assert.Equal(t, 2, g.RelatedPatterns[0].SharedConcepts)
		assert.ElementsMatch(t, []string{
			testIntegrationConceptPrefix + "retry",
			testIntegrationConceptPrefix + "jitter",
		}, g.RelatedPatterns[0].ConceptNames)
		assert.InDelta(t, 0.67, g.RelatedPatterns[0].Similarity, 1e-9)
	})

	t.Run("shared concepts on shared Concept nodes", func(t *testing.T) {
		cleanupNeo4jTestData(t, driver)

		pA := seedPattern(t, driver, "same-a", "retry", "jitter", "only-a")
		pB := seedPattern(t, driver, "same-b", "retry", "jitter")
		relate(t, driver, pA, pB, 0.67)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, g.RelatedPatterns, 1)
		assert.Equal(t, 2, g.RelatedPatterns[0].SharedConcepts)
		assert.ElementsMatch(t, []string{
			testIntegrationConceptPrefix + "retry",
			testIntegrationConceptPrefix + "jitter",
		}, g.RelatedPatterns[0].ConceptNames)
	})

	t.Run("related pattern with no shared concepts", func(t *testing.T) {
		cleanupNeo4jTestData(t, driver)

		pA := seedPatternOwnConcepts(t, driver, "none-a", "alpha")
		pB := seedPatternOwnConcepts(t, driver, "none-b", "beta")
		relate(t, driver, pA, pB, 0.4)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, g.RelatedPatterns, 1)
		assert.Equal(t, 0, g.RelatedPatterns[0].SharedConcepts)
		assert.Empty(t, g.RelatedPatterns[0].ConceptNames)
	})

	t.Run("related pattern with no concepts at all", func(t *testing.T) {
		cleanupNeo4jTestData(t, driver)

		pA := seedPatternOwnConcepts(t, driver, "bare-a", "alpha")
		pB := seedPatternOwnConcepts(t, driver, "bare-b")
		relate(t, driver, pA, pB, 0.4)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, g.RelatedPatterns, 1)
		assert.Equal(t, pB, g.RelatedPatterns[0].ID)
		assert.Equal(t, 0, g.RelatedPatterns[0].SharedConcepts)
		assert.Empty(t, g.RelatedPatterns[0].ConceptNames)
	})

	t.Run("source pattern with no concepts", func(t *testing.T) {
		cleanupNeo4jTestData(t, driver)

		pA := seedPatternOwnConcepts(t, driver, "srcbare-a")
		pB := seedPatternOwnConcepts(t, driver, "srcbare-b", "beta")
		relate(t, driver, pA, pB, 0.4)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, g.RelatedPatterns, 1)
		assert.Equal(t, 0, g.RelatedPatterns[0].SharedConcepts)
		assert.Empty(t, g.RelatedPatterns[0].ConceptNames)
	})

	t.Run("duplicate concept nodes on both sides count each name once", func(t *testing.T) {
		cleanupNeo4jTestData(t, driver)

		pA := seedPatternOwnConcepts(t, driver, "twice-a", "retry", "retry")
		pB := seedPatternOwnConcepts(t, driver, "twice-b", "retry", "retry", "retry")
		relate(t, driver, pA, pB, 0.5)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		require.Len(t, g.RelatedPatterns, 1)
		assert.Equal(t, 1, g.RelatedPatterns[0].SharedConcepts)
		assert.Equal(t, []string{testIntegrationConceptPrefix + "retry"}, g.RelatedPatterns[0].ConceptNames)
	})

	t.Run("parallel RELATED_TO edges", func(t *testing.T) {
		cleanupNeo4jTestData(t, driver)

		pA := seedPatternOwnConcepts(t, driver, "par-a", "retry", "jitter")
		pB := seedPatternOwnConcepts(t, driver, "par-b", "retry", "jitter")
		relate(t, driver, pA, pB, 0.67)
		relate(t, driver, pB, pA, 0.67)

		g, err := repo.GetPatternGraph(ctx, pA, 10)
		require.NoError(t, err)

		for _, rp := range g.RelatedPatterns {
			assert.Equal(t, 2, rp.SharedConcepts, "concepts are not double counted per edge")
			assert.Len(t, rp.ConceptNames, 2)
		}
		t.Logf("parallel edges returned %d rows", len(g.RelatedPatterns))
	})

	t.Run("pattern with no concepts or edges", func(t *testing.T) {
		id := seedPattern(t, driver, "graph-empty")

		g, err := repo.GetPatternGraph(ctx, id, 10)
		require.NoError(t, err)
		assert.Empty(t, g.Concepts)
		assert.Empty(t, g.RelatedPatterns)
	})

	t.Run("nonexistent pattern", func(t *testing.T) {
		g, err := repo.GetPatternGraph(ctx, uuid.New(), 10)
		require.NoError(t, err)
		assert.Empty(t, g.Concepts)
		assert.Empty(t, g.RelatedPatterns)
	})
}

func TestIntegration_DeletePatternRemovesOnlyItsOrphanedConcepts(t *testing.T) {
	driver, repo := setupNeo4j(t)
	ctx := context.Background()

	source := seedPattern(t, driver, "orphan-source", "orphan-a", "orphan-b", "orphan-shared")
	seedPattern(t, driver, "orphan-neighbour", "orphan-shared")
	// An orphan that was never attached to source must survive: the cleanup is
	// scoped to the deleted pattern's concepts, not a graph-wide sweep.
	cypher(t, driver, `CREATE (:Concept {name: $name, type: 'technology'})`,
		map[string]any{"name": testIntegrationConceptPrefix + "orphan-unrelated"})

	require.NoError(t, repo.DeletePattern(ctx, source))

	assert.Equal(t, int64(0), countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"orphan-a"))
	assert.Equal(t, int64(0), countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"orphan-b"))
	assert.Equal(t, int64(1), countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"orphan-shared"),
		"a concept another pattern still mentions is kept")
	assert.Equal(t, int64(1), countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"orphan-unrelated"),
		"orphans not attached to the deleted pattern are left alone")
}

func TestIntegration_ContextCancellation(t *testing.T) {
	_, repo := setupNeo4j(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.Error(t, repo.DeletePattern(ctx, uuid.New()))

	_, err := repo.GetPatternGraph(ctx, uuid.New(), 10)
	assert.Error(t, err)
}
