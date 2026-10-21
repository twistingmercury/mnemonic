package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/mnemonic/internal/api/metrics"
)

var tracer = otel.Tracer("mnemonic/neo4j")

// Repository defines the knowledge-graph operations the API needs. Writing
// concepts and RELATED_TO edges is the enricher's job.
type Repository interface {
	// DeletePattern removes a Pattern node and all its relationships from the
	// graph, along with any of its concepts that no other pattern mentions.
	DeletePattern(ctx context.Context, patternID uuid.UUID) error

	// GetPatternGraph returns up to relatedLimit related patterns and all
	// concepts for a pattern, using a single session and read transaction.
	GetPatternGraph(ctx context.Context, patternID uuid.UUID, relatedLimit int) (*PatternGraph, error)
}

// neo4jRepository is a Neo4j implementation of Repository.
type neo4jRepository struct {
	driver    neo4j.DriverWithContext
	database  string
	dbMetrics *metrics.Database
}

// NewRepository creates a new Neo4j-backed Repository. Each operation gets a
// client span and, when dbMetrics is non-nil, a latency and error recording.
func NewRepository(driver neo4j.DriverWithContext, database string, dbMetrics *metrics.Database) Repository {
	return &neo4jRepository{driver: driver, database: database, dbMetrics: dbMetrics}
}

// instrument starts a client span for one Neo4j operation. The returned func
// ends the span and records the operation's latency and outcome; defer it
// before any session close so close errors are counted too.
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
		if r.dbMetrics != nil {
			r.dbMetrics.Record(ctx, "neo4j", op, time.Since(start), err)
		}
	}
}

var errNilPatternID = errors.New("patternID must not be nil UUID")

// write runs one auto-committed, retried Cypher write statement.
func (r *neo4jRepository) write(ctx context.Context, cypher string, params map[string]any) error {
	_, err := neo4j.ExecuteQuery(ctx, r.driver, cypher, params, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(r.database), neo4j.ExecuteQueryWithWritersRouting())
	return err
}

// DeletePattern removes a Pattern node and all its relationships from the
// graph, along with any of its concepts that no other pattern mentions.
func (r *neo4jRepository) DeletePattern(ctx context.Context, patternID uuid.UUID) (err error) {
	if patternID == uuid.Nil {
		return errNilPatternID
	}
	ctx, done := r.instrument(ctx, "DeletePattern")
	defer func() { done(err) }()

	err = r.write(ctx,
		// Collect the pattern's concepts before detaching it, then drop only
		// those no other pattern mentions. This replaces a graph-wide orphan
		// sweep with work proportional to one pattern.
		`MATCH (p:Pattern {id: $patternId})
		 OPTIONAL MATCH (c:Concept)-[:MENTIONED_IN]->(p)
		 WITH p, collect(c) AS concepts
		 DETACH DELETE p
		 WITH concepts
		 UNWIND concepts AS c
		 WITH c WHERE NOT (c)-[:MENTIONED_IN]->()
		 DELETE c`,
		map[string]any{"patternId": patternID.String()},
	)
	if err != nil {
		return fmt.Errorf("deleting pattern %s: %w", patternID, err)
	}
	return nil
}

const conceptsCypher = `MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $patternId})
	 RETURN c.name AS name
	 ORDER BY c.name`

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
	 RETURN p2.id AS id, p2.name AS name, size(conceptNames) AS sharedConcepts, similarity, conceptNames`

// GetPatternGraph runs the related-patterns and concepts queries in one
// session and read transaction, instead of a session per query.
func (r *neo4jRepository) GetPatternGraph(ctx context.Context, patternID uuid.UUID, relatedLimit int) (_ *PatternGraph, err error) {
	if patternID == uuid.Nil {
		return nil, errNilPatternID
	}
	ctx, done := r.instrument(ctx, "GetPatternGraph")
	defer func() { done(err) }()

	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: r.database, AccessMode: neo4j.AccessModeRead})
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	graph, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) (*PatternGraph, error) {
		params := map[string]any{"patternId": patternID.String(), "limit": relatedLimit}

		res, err := tx.Run(ctx, relatedCypher, params)
		if err != nil {
			return nil, err
		}
		relatedRecs, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		res, err = tx.Run(ctx, conceptsCypher, params)
		if err != nil {
			return nil, err
		}
		conceptRecs, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		g := &PatternGraph{
			RelatedPatterns: make([]RelatedPattern, 0, len(relatedRecs)),
			Concepts:        make([]Concept, 0, len(conceptRecs)),
		}
		for _, rec := range relatedRecs {
			rp, err := toRelatedPattern(rec)
			if err != nil {
				return nil, err
			}
			g.RelatedPatterns = append(g.RelatedPatterns, rp)
		}
		for _, rec := range conceptRecs {
			c, err := toConcept(rec)
			if err != nil {
				return nil, err
			}
			g.Concepts = append(g.Concepts, c)
		}
		return g, nil
	})
	if err != nil {
		return nil, fmt.Errorf("getting graph for pattern %s: %w", patternID, err)
	}
	return graph, nil
}

func toConcept(rec *neo4j.Record) (Concept, error) {
	name, _, err := neo4j.GetRecordValue[string](rec, "name")
	if err != nil {
		return Concept{}, err
	}
	return Concept{Name: name}, nil
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
	shared, _, err := neo4j.GetRecordValue[int64](rec, "sharedConcepts")
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
		ID:             id,
		Name:           name,
		SharedConcepts: int(shared),
		Similarity:     similarity,
		ConceptNames:   names,
	}, nil
}
