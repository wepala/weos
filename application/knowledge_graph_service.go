package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"go.uber.org/fx"
)

// ErrKGUnavailable is returned when a knowledge-graph operation is requested
// but the underlying store is not configured. Callers (notably MCP tool
// handlers) translate this to a "knowledge graph not configured" message
// instead of an empty result so the LLM doesn't reason on a false negative.
var ErrKGUnavailable = errors.New("knowledge graph not configured")

// KnowledgeGraphService is the application-layer entry point for the
// `knowledge-graph` MCP tool group. It composes SPARQL on top of the
// storage-agnostic KnowledgeGraphStore so MCP tool handlers stay declarative
// — they describe intent ("expand this entity"), the service translates that
// to a SPARQL query the store can answer.
type KnowledgeGraphService interface {
	// Active reports whether the underlying store is connected. MCP tools
	// short-circuit when inactive so the LLM gets a clear "not configured"
	// signal instead of empty results.
	Active() bool

	// Query runs an arbitrary SPARQL query (SELECT/ASK/CONSTRUCT/DESCRIBE).
	// Use for the LLM-facing kg_sparql_query tool when something complex is
	// needed; the other methods cover most reasoning patterns more cheaply.
	Query(ctx context.Context, sparql string) (repositories.KGQueryResult, error)

	// ExpandEntity returns the one-hop neighborhood of an IRI as a list of
	// triples. depth defaults to 1; depths >1 walk reference chains via
	// SPARQL property paths.
	ExpandEntity(ctx context.Context, iri string, depth int) ([]repositories.Triple, error)

	// SearchEntities returns IRIs whose label-like properties (rdfs:label,
	// schema:name, foaf:name, dcterms:title) match q (case-insensitive
	// substring). limit defaults to 20, capped at 100.
	SearchEntities(ctx context.Context, q string, limit int) ([]repositories.KGTerm, error)

	// DescribeClass returns the predicates and (optionally) example instances
	// of a class IRI. Useful for LLM introspection — "what does foaf:Person
	// look like in this graph?".
	DescribeClass(ctx context.Context, classIRI string, sampleInstances int) (ClassDescription, error)

	// FindPath returns a best-effort path of triples connecting two IRIs
	// within maxHops. Returns nil triples when no path is found within the
	// hop budget; nil error in that case (path-not-found is not an error).
	FindPath(ctx context.Context, from, to string, maxHops int) ([]repositories.Triple, error)
}

// ClassDescription captures the metadata returned by DescribeClass.
type ClassDescription struct {
	ClassIRI   string             `json:"class_iri"`
	Predicates []string           `json:"predicates"`
	Instances  []repositories.KGTerm `json:"instances,omitempty"`
}

// ProvideKnowledgeGraphService wires the service. When the store is inactive
// the service still works (returns errors) so MCP tool handlers can report a
// clear "knowledge graph not configured" message instead of being absent.
func ProvideKnowledgeGraphService(params struct {
	fx.In
	Store  repositories.KnowledgeGraphStore
	Logger entities.Logger
}) KnowledgeGraphService {
	return &knowledgeGraphService{store: params.Store, logger: params.Logger}
}

type knowledgeGraphService struct {
	store  repositories.KnowledgeGraphStore
	logger entities.Logger
}

func (s *knowledgeGraphService) Active() bool { return s.store.Active() }

func (s *knowledgeGraphService) Query(
	ctx context.Context, sparql string,
) (repositories.KGQueryResult, error) {
	if !s.store.Active() {
		return repositories.KGQueryResult{}, ErrKGUnavailable
	}
	if sparql == "" {
		return repositories.KGQueryResult{}, fmt.Errorf("sparql query is required")
	}
	s.logger.Debug(ctx, "kg query", "form", detectQuerySummary(sparql))
	return s.store.Query(ctx, sparql)
}

func (s *knowledgeGraphService) ExpandEntity(
	ctx context.Context, iri string, depth int,
) ([]repositories.Triple, error) {
	if !s.store.Active() {
		return nil, ErrKGUnavailable
	}
	if iri == "" {
		return nil, fmt.Errorf("iri is required")
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		// Beyond 3 hops the result set explodes for typical graphs; the LLM
		// should chain ExpandEntity calls instead of asking for the full
		// closure.
		depth = 3
	}
	q := buildExpandQuery(iri, depth)
	s.logger.Debug(ctx, "kg expand entity", "iri", iri, "depth", depth)
	res, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return res.Triples, nil
}

func (s *knowledgeGraphService) SearchEntities(
	ctx context.Context, q string, limit int,
) ([]repositories.KGTerm, error) {
	if !s.store.Active() {
		return nil, ErrKGUnavailable
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query string is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	sparql := buildSearchQuery(q, limit)
	s.logger.Debug(ctx, "kg search entities", "q", q, "limit", limit)
	res, err := s.store.Query(ctx, sparql)
	if err != nil {
		return nil, err
	}
	out := make([]repositories.KGTerm, 0, len(res.Bindings))
	for _, row := range res.Bindings {
		if t, ok := row["s"]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *knowledgeGraphService) DescribeClass(
	ctx context.Context, classIRI string, sampleInstances int,
) (ClassDescription, error) {
	if !s.store.Active() {
		return ClassDescription{}, ErrKGUnavailable
	}
	if classIRI == "" {
		return ClassDescription{}, fmt.Errorf("class iri is required")
	}
	if sampleInstances < 0 {
		sampleInstances = 0
	}
	if sampleInstances > 50 {
		sampleInstances = 50
	}

	// Distinct predicates used by instances of the class. Property paths
	// (`rdf:type/rdfs:subClassOf*`) walk the subclass chain so subclasses
	// are included automatically.
	predQuery := fmt.Sprintf(`
SELECT DISTINCT ?p WHERE {
  ?s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type>/<http://www.w3.org/2000/01/rdf-schema#subClassOf>* %s .
  ?s ?p ?o .
}`, formatIRI(classIRI))
	predRes, err := s.store.Query(ctx, predQuery)
	if err != nil {
		return ClassDescription{}, err
	}
	out := ClassDescription{ClassIRI: classIRI}
	for _, row := range predRes.Bindings {
		if t, ok := row["p"]; ok {
			out.Predicates = append(out.Predicates, t.Value)
		}
	}

	if sampleInstances > 0 {
		instQuery := fmt.Sprintf(`
SELECT ?s WHERE {
  ?s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type>/<http://www.w3.org/2000/01/rdf-schema#subClassOf>* %s .
} LIMIT %d`, formatIRI(classIRI), sampleInstances)
		instRes, err := s.store.Query(ctx, instQuery)
		if err != nil {
			return out, err
		}
		for _, row := range instRes.Bindings {
			if t, ok := row["s"]; ok {
				out.Instances = append(out.Instances, t)
			}
		}
	}
	s.logger.Debug(ctx, "kg describe class",
		"class", classIRI, "predicates", len(out.Predicates), "instances", len(out.Instances))
	return out, nil
}

func (s *knowledgeGraphService) FindPath(
	ctx context.Context, from, to string, maxHops int,
) ([]repositories.Triple, error) {
	if !s.store.Active() {
		return nil, ErrKGUnavailable
	}
	if from == "" || to == "" {
		return nil, fmt.Errorf("both from and to iris are required")
	}
	if maxHops <= 0 {
		maxHops = 4
	}
	if maxHops > 6 {
		// Property-path queries with high length quickly become expensive;
		// 6 is a soft ceiling that still covers most realistic chains.
		maxHops = 6
	}
	q := buildFindPathQuery(from, to, maxHops)
	s.logger.Debug(ctx, "kg find path", "from", from, "to", to, "hops", maxHops)
	res, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return res.Triples, nil
}

// --- SPARQL builders ---

// formatIRI wraps an IRI in <...> for embedding into a SPARQL string. The
// caller is responsible for not passing already-bracketed IRIs (we don't try
// to be clever — that's the projector's job).
func formatIRI(iri string) string {
	return "<" + iri + ">"
}

// buildExpandQuery returns a CONSTRUCT query for the one-hop neighborhood of
// an entity. depth>1 walks outgoing predicates via property paths; this stays
// simple (forward edges only) on purpose — a more general solution can come
// later if the LLM needs reverse traversal.
func buildExpandQuery(iri string, depth int) string {
	if depth == 1 {
		return fmt.Sprintf(`
CONSTRUCT { %s ?p ?o }
WHERE     { %s ?p ?o }`, formatIRI(iri), formatIRI(iri))
	}
	return fmt.Sprintf(`
CONSTRUCT { ?s ?p ?o }
WHERE {
  %s (:|!:){0,%d} ?s .
  ?s ?p ?o .
}`, formatIRI(iri), depth-1)
}

// buildSearchQuery returns a SELECT for IRIs with label-like predicates
// matching the user query. CONTAINS+LCASE gives case-insensitive substring
// matching against the major naming predicates.
func buildSearchQuery(q string, limit int) string {
	escaped := escapeSPARQLLiteral(strings.ToLower(q))
	return fmt.Sprintf(`
SELECT DISTINCT ?s WHERE {
  ?s ?labelPred ?lbl .
  FILTER(?labelPred IN (
    <http://www.w3.org/2000/01/rdf-schema#label>,
    <http://schema.org/name>,
    <https://schema.org/name>,
    <http://xmlns.com/foaf/0.1/name>,
    <http://purl.org/dc/terms/title>
  ))
  FILTER(CONTAINS(LCASE(STR(?lbl)), "%s"))
} LIMIT %d`, escaped, limit)
}

// buildFindPathQuery returns a CONSTRUCT that materializes the triples on a
// path from `from` to `to` using the BIND/property-path approximation. This
// returns *a* path (not necessarily shortest) — callers asked for "best
// effort" not "optimal."
func buildFindPathQuery(from, to string, maxHops int) string {
	return fmt.Sprintf(`
CONSTRUCT { ?s ?p ?o }
WHERE {
  %s (<>|!<>){1,%d} %s .
  %s (<>|!<>)* ?s .
  ?s ?p ?o .
  ?o (<>|!<>)* %s .
}`, formatIRI(from), maxHops, formatIRI(to), formatIRI(from), formatIRI(to))
}

// escapeSPARQLLiteral escapes a string so it can be embedded inside a
// double-quoted SPARQL literal. Per SPARQL 1.1 grammar we escape `\` and `"`;
// newlines and tabs become their escape sequences so a multi-line search
// term doesn't break the query.
func escapeSPARQLLiteral(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return r.Replace(s)
}

// detectQuerySummary returns a short tag for the query form (used in debug
// logs only — keeps full SPARQL out of unstructured logs).
func detectQuerySummary(q string) string {
	q = strings.TrimSpace(q)
	for _, p := range []string{"SELECT", "ASK", "CONSTRUCT", "DESCRIBE"} {
		if len(q) >= len(p) && strings.EqualFold(q[:len(p)], p) {
			return p
		}
	}
	return "OTHER"
}
