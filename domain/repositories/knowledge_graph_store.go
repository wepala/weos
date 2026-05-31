package repositories

import "context"

// KnowledgeGraphStore is the storage-agnostic interface used by the optional
// knowledge-graph projection and the `knowledge-graph` MCP tool group. The
// active implementation is Oxigraph over HTTP, but the interface intentionally
// avoids any Oxigraph-specific types so a future swap (Jena Fuseki, GraphDB,
// in-memory) is a provider change, not a rewrite.
//
// All terms (subjects, predicates, objects) are passed as already-formatted
// strings — IRIs as-is, literals as their lexical form. Callers that need
// typed/language-tagged literals should serialize them in N-Triples form
// (e.g. `"foo"@en`, `"42"^^<http://www.w3.org/2001/XMLSchema#integer>`).
type KnowledgeGraphStore interface {
	// Active reports whether the store is wired to a real backend. The nop
	// implementation returns false so call sites can short-circuit work
	// (e.g. skip building triples) without nil-checks.
	Active() bool

	// AddTriples inserts triples into the default graph. Idempotent: re-adding
	// an existing triple is a no-op. Empty input is a no-op.
	AddTriples(ctx context.Context, triples []Triple) error

	// RemoveTriples removes the given triples from the default graph. Missing
	// triples are tolerated.
	RemoveTriples(ctx context.Context, triples []Triple) error

	// RemoveSubject removes every triple whose subject matches `subject`.
	// Used by the Resource.Deleted projector to drop a whole resource at once.
	RemoveSubject(ctx context.Context, subject string) error

	// Query executes a SPARQL SELECT, ASK, CONSTRUCT, or DESCRIBE query.
	Query(ctx context.Context, sparql string) (KGQueryResult, error)

	// Update executes a SPARQL UPDATE (INSERT/DELETE/CLEAR/etc.). Used
	// internally by ontology-loading and backfill paths; not exposed to the
	// LLM through MCP.
	Update(ctx context.Context, sparql string) error

	// LoadOntology bulk-loads serialized RDF data (e.g. a JSON-LD `@context`
	// or a Turtle ontology) into the default graph. format is the IANA media
	// type, e.g. "text/turtle", "application/n-triples", "application/ld+json".
	LoadOntology(ctx context.Context, format string, body []byte) error

	// Clear removes every triple in the default graph. Used by the backfill
	// rebuild path.
	Clear(ctx context.Context) error

	// IsEmpty reports whether the default graph has zero triples.
	// Used by the backfill startup hook to decide whether to replay.
	IsEmpty(ctx context.Context) (bool, error)
}

// KGTermType enumerates the kind of an RDF term in a binding.
type KGTermType string

const (
	KGTermIRI     KGTermType = "uri"
	KGTermLiteral KGTermType = "literal"
	KGTermBlank   KGTermType = "bnode"
)

// KGTerm is a single RDF term in a query binding. Datatype/Lang are populated
// only for literals and only when present in the underlying response.
type KGTerm struct {
	Type     KGTermType
	Value    string
	Datatype string
	Lang     string
}

// KGQueryResult is the shape of a SPARQL query response, normalized across
// SELECT/ASK/CONSTRUCT/DESCRIBE.
type KGQueryResult struct {
	// Vars lists the projected variable names for SELECT queries (in order).
	Vars []string
	// Bindings is the row set for SELECT queries: each map keys the variable
	// name (without leading `?`) to its bound term.
	Bindings []map[string]KGTerm
	// Boolean is set for ASK queries; nil for other forms.
	Boolean *bool
	// Triples is set for CONSTRUCT/DESCRIBE queries; nil for other forms.
	Triples []Triple
}
