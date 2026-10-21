package helpers

import (
	"context"
	"testing"

	"github.com/google/uuid"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// NewNeo4jDriver creates a neo4j.DriverWithContext from env vars.
// Reads NEO4J_URI (default: "bolt://localhost:7687"), NEO4J_USER (default: "neo4j"),
// and NEO4J_PASSWORD (default: "mnemonic_dev").
// Calls t.Fatal on error. The caller is responsible for closing the driver.
func NewNeo4jDriver(t *testing.T) neo4j.DriverWithContext {
	t.Helper()

	driver, err := neo4j.NewDriverWithContext(
		envOr("NEO4J_URI", "bolt://localhost:7687"),
		neo4j.BasicAuth(envOr("NEO4J_USER", "neo4j"), envOr("NEO4J_PASSWORD", "mnemonic_dev"), ""))
	if err != nil {
		t.Fatalf("failed to create neo4j driver: %v", err)
	}

	if err := driver.VerifyConnectivity(context.Background()); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("failed to verify neo4j connectivity: %v", err)
	}

	return driver
}

// countForPattern runs a read query, bound to the pattern's ID as $id, and
// returns how many records it produced. Calls t.Fatal on error.
func countForPattern(t *testing.T, driver neo4j.DriverWithContext, cypher string, patternID uuid.UUID) int {
	t.Helper()

	result, err := neo4j.ExecuteQuery(
		context.Background(),
		driver,
		cypher,
		map[string]any{"id": patternID.String()},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithReadersRouting(),
	)
	if err != nil {
		t.Fatalf("failed to query graph for pattern %s: %v", patternID, err)
	}
	return len(result.Records)
}

const patternNodeCypher = "MATCH (p:Pattern {id: $id}) RETURN p"

// AssertPatternNodeExists asserts that a Pattern node with the given ID exists in Neo4j.
// Calls t.Fatal if no node is found.
func AssertPatternNodeExists(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) {
	t.Helper()
	if countForPattern(t, driver, patternNodeCypher, patternID) == 0 {
		t.Fatalf("expected Pattern node with id %s to exist in Neo4j, but none found", patternID)
	}
}

// AssertConceptNodesExist asserts that at least one Concept node with a MENTIONED_IN
// edge to the Pattern node exists.
// Calls t.Fatal if no concept nodes are found.
func AssertConceptNodesExist(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) {
	t.Helper()
	if countForPattern(t, driver, "MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $id}) RETURN c", patternID) == 0 {
		t.Fatalf("expected at least one Concept node MENTIONED_IN Pattern %s, but none found", patternID)
	}
}

// AssertPatternNodeNotExists asserts that NO Pattern node with the given ID exists in Neo4j.
// Calls t.Fatal if a node IS found.
func AssertPatternNodeNotExists(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) {
	t.Helper()
	if countForPattern(t, driver, patternNodeCypher, patternID) != 0 {
		t.Fatalf("expected no Pattern node with id %s in Neo4j, but one was found", patternID)
	}
}

// CleanupPatternGraph deletes the pattern node and all connected concept nodes/edges.
func CleanupPatternGraph(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) {
	t.Helper()

	_, err := neo4j.ExecuteQuery(
		context.Background(),
		driver,
		"MATCH (p:Pattern {id: $id}) OPTIONAL MATCH (c:Concept)-[:MENTIONED_IN]->(p) DETACH DELETE p, c",
		map[string]any{"id": patternID.String()},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithWritersRouting(),
	)
	if err != nil {
		t.Fatalf("failed to cleanup pattern graph for %s: %v", patternID, err)
	}
}
