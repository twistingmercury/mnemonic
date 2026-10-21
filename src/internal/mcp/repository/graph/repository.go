package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/librarian-mcp/internal/metrics"
)

var tracer = otel.Tracer("mnemonic/neo4j")

// Repository defines the read-only knowledge graph queries the MCP tools need.
type Repository interface {
	// GetPatternConcepts returns all concepts linked to a pattern via MENTIONED_IN relationships.
	GetPatternConcepts(ctx context.Context, patternID uuid.UUID) ([]Concept, error)

	// FindRelatedPatterns finds patterns related to the given pattern using pre-computed RELATED_TO edges.
	// Results include similarity scores and shared concept names.
	FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]RelatedPattern, error)
}

// neo4jRepository is a Neo4j implementation of Repository.
type neo4jRepository struct {
	driver    neo4j.DriverWithContext
	database  string
	dbMetrics *metrics.Database
}

// NewRepository creates a new Neo4j-backed Repository. Each operation gets a
// client span and a latency and error recording.
func NewRepository(driver neo4j.DriverWithContext, database string, dbMetrics *metrics.Database) Repository {
	return &neo4jRepository{driver: driver, database: database, dbMetrics: dbMetrics}
}

// instrument starts a client span for one Neo4j operation. The returned func
// ends the span and records the operation's latency and outcome.
func (r *neo4jRepository) instrument(ctx context.Context, op string) (context.Context, func(error)) {
	start := time.Now()
	ctx, span := tracer.Start(ctx, "neo4j "+op,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "neo4j"),
			attribute.String("db.namespace", r.database),
			attribute.String("db.operation.name", op),
		),
	)
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
		r.dbMetrics.Record(ctx, "neo4j", op, time.Since(start), err)
	}
}

// read runs one retried, read-routed Cypher query and returns its records.
func (r *neo4jRepository) read(ctx context.Context, cypher string, params map[string]any) ([]*neo4j.Record, error) {
	res, err := neo4j.ExecuteQuery(ctx, r.driver, cypher, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(r.database), neo4j.ExecuteQueryWithReadersRouting())
	if err != nil {
		return nil, err
	}
	return res.Records, nil
}

// GetPatternConcepts returns all concepts linked to a pattern via MENTIONED_IN relationships.
func (r *neo4jRepository) GetPatternConcepts(ctx context.Context, patternID uuid.UUID) (_ []Concept, err error) {
	ctx, done := r.instrument(ctx, "GetPatternConcepts")
	defer func() { done(err) }()

	records, err := r.read(ctx,
		`MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $patternId})
		 RETURN c.name AS name, c.type AS type
		 ORDER BY c.name`,
		map[string]any{"patternId": patternID.String()},
	)
	if err != nil {
		return nil, fmt.Errorf("getting concepts for pattern %s: %w", patternID, err)
	}

	concepts := make([]Concept, 0, len(records))
	for _, rec := range records {
		c, err := toConcept(rec)
		if err != nil {
			return nil, fmt.Errorf("getting concepts for pattern %s: %w", patternID, err)
		}
		concepts = append(concepts, c)
	}
	return concepts, nil
}

// relatedCypher matches shared concepts by name, not node identity: without a
// uniqueness constraint two patterns can each have their own Concept node for
// the same name, and a node-identity match would report none shared.
const relatedCypher = `MATCH (p1:Pattern {id: $patternId})-[r:RELATED_TO]-(p2:Pattern)
	 WITH p1, p2, r.similarity AS similarity
	 OPTIONAL MATCH (c1:Concept)-[:MENTIONED_IN]->(p1)
	 WITH p2, similarity, collect(DISTINCT c1.name) AS names
	 OPTIONAL MATCH (c2:Concept)-[:MENTIONED_IN]->(p2)
	 WHERE c2.name IN names
	 WITH p2, similarity, collect(DISTINCT c2.name) AS conceptNames
	 ORDER BY similarity DESC
	 LIMIT $limit
	 RETURN p2.id AS id, p2.name AS name, similarity, conceptNames`

// FindRelatedPatterns finds patterns related to the given pattern using pre-computed RELATED_TO edges.
// Results include similarity scores and shared concept names.
func (r *neo4jRepository) FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) (_ []RelatedPattern, err error) {
	ctx, done := r.instrument(ctx, "FindRelatedPatterns")
	defer func() { done(err) }()

	records, err := r.read(ctx, relatedCypher, map[string]any{
		"patternId": patternID.String(),
		"limit":     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("finding related patterns for %s: %w", patternID, err)
	}

	patterns := make([]RelatedPattern, 0, len(records))
	for _, rec := range records {
		rp, err := toRelatedPattern(rec)
		if err != nil {
			return nil, fmt.Errorf("finding related patterns for %s: %w", patternID, err)
		}
		patterns = append(patterns, rp)
	}
	return patterns, nil
}

func toConcept(rec *neo4j.Record) (Concept, error) {
	name, _, err := neo4j.GetRecordValue[string](rec, "name")
	if err != nil {
		return Concept{}, err
	}
	typ, _, err := neo4j.GetRecordValue[string](rec, "type")
	if err != nil {
		return Concept{}, err
	}
	return Concept{Name: name, Type: typ}, nil
}

func toRelatedPattern(rec *neo4j.Record) (RelatedPattern, error) {
	idStr, _, err := neo4j.GetRecordValue[string](rec, "id")
	if err != nil {
		return RelatedPattern{}, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return RelatedPattern{}, fmt.Errorf("parsing pattern ID %q: %w", idStr, err)
	}
	name, _, err := neo4j.GetRecordValue[string](rec, "name")
	if err != nil {
		return RelatedPattern{}, err
	}
	similarity, _, err := neo4j.GetRecordValue[float64](rec, "similarity")
	if err != nil {
		return RelatedPattern{}, err
	}
	rawNames, _, err := neo4j.GetRecordValue[[]any](rec, "conceptNames")
	if err != nil {
		return RelatedPattern{}, err
	}
	names := make([]string, 0, len(rawNames))
	for _, v := range rawNames {
		s, ok := v.(string)
		if !ok {
			return RelatedPattern{}, fmt.Errorf("unexpected type for concept name element: %T", v)
		}
		names = append(names, s)
	}

	return RelatedPattern{
		ID:           id,
		Name:         name,
		Similarity:   similarity,
		ConceptNames: names,
	}, nil
}
