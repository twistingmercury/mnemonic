//go:build integration

package graph_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/twistingmercury/librarian-enricher/internal/metrics"
	"github.com/twistingmercury/librarian-enricher/internal/repository/graph"
)

const testConceptPrefix = "test-integration-concept-"

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// setupNeo4j connects to the Neo4j named by NEO4J_* env vars, or skips.
func setupNeo4j(t *testing.T) (neo4j.DriverWithContext, graph.Repository) {
	t.Helper()

	driver, err := neo4j.NewDriverWithContext(
		envOr("NEO4J_URI", "bolt://localhost:17688"),
		neo4j.BasicAuth(envOr("NEO4J_USER", "neo4j"), envOr("NEO4J_PASSWORD", "mnemonic_dev"), ""))
	if err != nil {
		t.Skipf("skipping integration test: unable to create Neo4j driver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		t.Skipf("skipping integration test: Neo4j connectivity check failed: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })

	dbMetrics, err := metrics.NewDatabase(noop.NewMeterProvider().Meter("test"))
	require.NoError(t, err)
	return driver, graph.NewRepository(driver, envOr("NEO4J_DATABASE", "neo4j"), dbMetrics)
}

func query(t *testing.T, driver neo4j.DriverWithContext, cypher string, params map[string]any) []*neo4j.Record {
	t.Helper()
	res, err := neo4j.ExecuteQuery(context.Background(), driver, cypher, params, neo4j.EagerResultTransformer)
	require.NoError(t, err)
	return res.Records
}

func TestIntegration_SyncPattern(t *testing.T) {
	driver, repo := setupNeo4j(t)
	ctx := context.Background()

	a, b := uuid.New(), uuid.New()
	t.Cleanup(func() {
		query(t, driver, `MATCH (p:Pattern) WHERE p.id IN $ids DETACH DELETE p`,
			map[string]any{"ids": []string{a.String(), b.String()}})
		query(t, driver, `MATCH (c:Concept) WHERE c.name STARTS WITH $prefix DETACH DELETE c`,
			map[string]any{"prefix": testConceptPrefix})
	})

	shared := graph.Concept{Name: testConceptPrefix + "go", Type: "technology"}
	onlyA := graph.Concept{Name: testConceptPrefix + "retries", Type: "practice"}

	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: b, Name: "b"}, []graph.Concept{shared}, 0.3))
	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: a, Name: "a"}, []graph.Concept{shared, onlyA}, 0.3))

	concepts := query(t, driver, `MATCH (c:Concept)-[:MENTIONED_IN]->(:Pattern {id: $id}) RETURN c.name AS name ORDER BY name`,
		map[string]any{"id": a.String()})
	assert.Len(t, concepts, 2)

	// a shares 1 of max(2, 1) concepts with b: similarity 0.5.
	related := query(t, driver, `MATCH (:Pattern {id: $a})-[r:RELATED_TO]-(:Pattern {id: $b}) RETURN r.similarity AS s`,
		map[string]any{"a": a.String(), "b": b.String()})
	require.Len(t, related, 1)
	s, _ := related[0].Get("s")
	assert.InDelta(t, 0.5, s, 0.0001)

	// Re-syncing with fewer concepts drops the stale MENTIONED_IN and RELATED_TO.
	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: a, Name: "a"}, []graph.Concept{onlyA}, 0.3))
	concepts = query(t, driver, `MATCH (c:Concept)-[:MENTIONED_IN]->(:Pattern {id: $id}) RETURN c.name AS name`,
		map[string]any{"id": a.String()})
	assert.Len(t, concepts, 1)
	related = query(t, driver, `MATCH (:Pattern {id: $a})-[r:RELATED_TO]-() RETURN r`, map[string]any{"a": a.String()})
	assert.Empty(t, related)
}

// Whichever pattern of a pair syncs last, and however often, the pair keeps one
// edge directed from the lower id to the higher.
func TestIntegration_SyncPattern_OneCanonicalEdgePerPair(t *testing.T) {
	driver, repo := setupNeo4j(t)
	ctx := context.Background()

	low, high := uuid.New(), uuid.New()
	if high.String() < low.String() {
		low, high = high, low
	}

	prefix := testConceptPrefix + "canonical-" + low.String() + "-"
	t.Cleanup(func() {
		query(t, driver, `MATCH (p:Pattern) WHERE p.id IN $ids DETACH DELETE p`,
			map[string]any{"ids": []string{low.String(), high.String()}})
		query(t, driver, `MATCH (c:Concept) WHERE c.name STARTS WITH $prefix DETACH DELETE c`,
			map[string]any{"prefix": prefix})
	})
	shared := []graph.Concept{{Name: prefix + "go", Type: "technology"}}

	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: low, Name: "low"}, shared, 0.3))
	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: high, Name: "high"}, shared, 0.3))
	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: low, Name: "low"}, shared, 0.3))
	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: high, Name: "high"}, shared, 0.3))

	related := query(t, driver, `MATCH (x:Pattern)-[r:RELATED_TO]->(y:Pattern)
		WHERE x.id IN $ids AND y.id IN $ids RETURN x.id AS from, r.similarity AS s`,
		map[string]any{"ids": []string{low.String(), high.String()}})
	require.Len(t, related, 1)
	from, _ := related[0].Get("from")
	assert.Equal(t, low.String(), from)
	s, _ := related[0].Get("s")
	assert.InDelta(t, 1.0, s, 0.0001)
}

// The enricher runs several workers, so two related patterns can sync at the
// same moment. Each pair must still end up with exactly one RELATED_TO edge.
// The race is harshest without concept_name_unique, as on Neptune, where
// overlapping syncs each create their own copy of a new shared concept.
func TestIntegration_SyncPattern_ConcurrentRelatedPatterns(t *testing.T) {
	driver, repo := setupNeo4j(t)

	const pairs = 20
	ids := make([]string, 0, 2*pairs)
	t.Cleanup(func() {
		query(t, driver, `MATCH (p:Pattern) WHERE p.id IN $ids DETACH DELETE p`, map[string]any{"ids": ids})
		query(t, driver, `MATCH (c:Concept) WHERE c.name STARTS WITH $prefix DETACH DELETE c`,
			map[string]any{"prefix": testConceptPrefix})
	})

	edgeCounts := map[int]int{}
	for i := range pairs {
		a, b := uuid.New(), uuid.New()
		ids = append(ids, a.String(), b.String())
		syncPairConcurrently(t, repo, i, a, b)

		related := query(t, driver, `MATCH (:Pattern {id: $a})-[r:RELATED_TO]-(:Pattern {id: $b}) RETURN r`,
			map[string]any{"a": a.String(), "b": b.String()})
		edgeCounts[len(related)]++
		assert.Len(t, related, 1, "pair %d: expected one RELATED_TO edge", i)
	}
	t.Logf("pairs by RELATED_TO edge count: %v", edgeCounts)
}

// syncPairConcurrently syncs two patterns sharing two of three concepts
// (similarity 0.67), releasing both calls at once so their transactions overlap.
func syncPairConcurrently(t *testing.T, repo graph.Repository, pair int, a, b uuid.UUID) {
	t.Helper()

	prefix := fmt.Sprintf("%spair-%d-%s-", testConceptPrefix, pair, a)
	shared := []graph.Concept{{Name: prefix + "go", Type: "technology"}, {Name: prefix + "retries", Type: "practice"}}
	conceptsA := slices.Concat(shared, []graph.Concept{{Name: prefix + "only-a", Type: "domain"}})
	conceptsB := slices.Concat(shared, []graph.Concept{{Name: prefix + "only-b", Type: "domain"}})

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(syncAfter(start, errs, repo, graph.Pattern{ID: a, Name: "a"}, conceptsA))
	wg.Go(syncAfter(start, errs, repo, graph.Pattern{ID: b, Name: "b"}, conceptsB))
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "pair %d", pair)
	}
}

func syncAfter(start <-chan struct{}, errs chan<- error, repo graph.Repository, p graph.Pattern, concepts []graph.Concept) func() {
	return func() {
		<-start
		errs <- repo.SyncPattern(context.Background(), p, concepts, 0.3)
	}
}
