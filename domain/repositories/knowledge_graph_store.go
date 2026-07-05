package repositories

import (
	"context"
	"errors"
	"io"
)

// ErrNoAccount is returned by KnowledgeGraphStores.ForAccount when per-account
// mode is enabled but the caller passed an empty account id. Callers resolve
// the account (and decide fail-closed vs. the local graph) before asking for a
// store, so an empty id reaching the factory is a programming error, not a
// routing decision.
var ErrNoAccount = errors.New("knowledge graph: no account for per-account store")

// KnowledgeGraphStores resolves which KnowledgeGraphStore a caller or a
// projected event should use. It is the seam that makes many-accounts-in-one-
// process isolation possible without changing the KnowledgeGraphStore interface:
//
//   - In SINGLE-TENANT mode (one embedded Path, one HTTP URL, or nop) it always
//     returns the one process store, ignoring the account — behavior is
//     identical to before per-account mode existed.
//   - In PER-ACCOUNT mode it lazily opens and caches one embedded store per
//     account under a base directory, so a query for account A can never see
//     account B's graph.
type KnowledgeGraphStores interface {
	// Active reports whether any real backend is wired (false for pure nop),
	// so registration can skip the projector/tools entirely when the graph is
	// off.
	Active() bool

	// PerAccount reports whether per-account isolation is enabled. Call sites
	// use it to decide whether to resolve an account before querying and how to
	// handle a missing one (fail closed vs. the local graph).
	PerAccount() bool

	// ForAccount returns the store for accountID, opening it lazily in
	// per-account mode. In single-tenant mode accountID is ignored and the one
	// process store is returned. In per-account mode an empty accountID returns
	// ErrNoAccount (fail closed) — callers must resolve the account first.
	ForAccount(ctx context.Context, accountID string) (KnowledgeGraphStore, error)

	// Truncate clears every graph for a checkpoint-reset rebuild: the single
	// store in single-tenant mode, every account store (open or on disk) in
	// per-account mode. The subsequent replay re-projects each event into the
	// owning account's store.
	Truncate(ctx context.Context) error

	// Close releases every open store (flush + unlock). Registered on the fx
	// OnStop hook so restarts reopen cleanly without stale directory locks.
	Close() error
}

// NewSingleKnowledgeGraphStores adapts one KnowledgeGraphStore to the
// KnowledgeGraphStores interface for single-tenant mode: ForAccount ignores the
// account and always returns the wrapped store, Truncate clears it, and Close is
// a no-op (the wrapped store's own closer is registered separately by the
// provider that built it). Kept here — rather than in the infrastructure graph
// package — so both the provider and the application/service unit tests can wrap
// a store without importing the embedded backend.
func NewSingleKnowledgeGraphStores(store KnowledgeGraphStore) KnowledgeGraphStores {
	return singleStores{store: store}
}

type singleStores struct {
	store KnowledgeGraphStore
}

func (s singleStores) Active() bool { return s.store != nil && s.store.Active() }

func (s singleStores) PerAccount() bool { return false }

func (s singleStores) ForAccount(_ context.Context, _ string) (KnowledgeGraphStore, error) {
	return s.store, nil
}

func (s singleStores) Truncate(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	return s.store.Clear(ctx)
}

// Close is a no-op: the wrapped store's io.Closer (if any) is registered on the
// fx lifecycle by the provider that opened it, so closing here would double-
// close. The type assertion is kept only to document that intent.
func (s singleStores) Close() error {
	_, _ = s.store.(io.Closer)
	return nil
}

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

	// Clear removes every triple in the default graph. Used by the oxigraph
	// subscriber's checkpoint-reset rebuild path (worker checkpoint reset
	// --truncate / OXIGRAPH_REBUILD) to clear the graph before replay.
	Clear(ctx context.Context) error

	// IsEmpty reports whether the default graph has zero triples.
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
