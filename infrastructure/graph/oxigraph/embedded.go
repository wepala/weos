//go:build oxigraph_embedded

// The embedded backend: repositories.KnowledgeGraphStore over an in-process
// oxigraph store opened on a local path — the akeemphilbert/oxigraph Go
// binding over the oxigraph-ffi C ABI, no external SPARQL endpoint.
//
// Built only under the `oxigraph_embedded` tag, which needs CGO and a
// vendored liboxigraph_ffi.a for the target platform (see the cgo_*.go files
// and lib/). Without the tag the stub in embedded_stub.go compiles instead
// and reports the backend unavailable, so a pure-Go build (CGO_ENABLED=0, no
// lib) still runs with the graph simply off.
//
// It lives in this package so it shares the HTTP Store's SPARQL formatting
// helpers (formatTriple/formatTerm) — one code path, two transports: the
// mutating methods build the same SPARQL the HTTP store does and hand it to
// the binding's Update() instead of POSTing it.

package oxigraph

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	oxidb "github.com/akeemphilbert/oxigraph/go"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// EmbeddedStore is a KnowledgeGraphStore backed by an in-process oxigraph
// store on a local path.
type EmbeddedStore struct {
	store  *oxidb.Store
	path   string
	logger entities.Logger
}

// EmbeddedAvailable reports whether this binary was built with embedded
// support (the oxigraph_embedded tag). Always true in this file; the stub's
// version returns false. The provider uses it to distinguish "embedded path
// configured but not compiled in" from an open failure.
func EmbeddedAvailable() bool { return true }

// NewEmbeddedStore opens the embedded oxigraph store at path, creating it (and
// its parent directory contents) if absent. The concrete return is
// *EmbeddedStore (which also satisfies io.Closer so the provider can flush and
// unlock on shutdown); the interface return keeps the signature identical to
// the no-embedded stub so the provider compiles either way. The store holds an
// exclusive lock on the directory until Close.
func NewEmbeddedStore(path string, logger entities.Logger) (repositories.KnowledgeGraphStore, error) {
	if path == "" {
		return nil, fmt.Errorf("oxigraph embedded: store path is required")
	}
	store, err := oxidb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("oxigraph embedded: open %q: %w", path, err)
	}
	return &EmbeddedStore{store: store, path: path, logger: logger}, nil
}

// Active reports whether the store is wired to a real backend.
func (s *EmbeddedStore) Active() bool { return s != nil && s.store != nil }

// Close releases the embedded store, flushing to disk and dropping the
// directory lock so the same path can be reopened. Idempotent: it clears the
// underlying handle so a second call (e.g. the Fx OnStop hook plus a test
// cleanup) is a no-op rather than a double-close.
func (s *EmbeddedStore) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	err := s.store.Close()
	s.store = nil
	return err
}

// AddTriples inserts triples via SPARQL UPDATE INSERT DATA — the same SPARQL
// the HTTP store builds, so the two backends behave identically.
func (s *EmbeddedStore) AddTriples(ctx context.Context, triples []repositories.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	return s.Update(ctx, wrapData("INSERT DATA", triples))
}

// RemoveTriples deletes the given triples via SPARQL UPDATE DELETE DATA.
func (s *EmbeddedStore) RemoveTriples(ctx context.Context, triples []repositories.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	return s.Update(ctx, wrapData("DELETE DATA", triples))
}

// RemoveSubject drops every triple whose subject matches the given URI.
func (s *EmbeddedStore) RemoveSubject(ctx context.Context, subject string) error {
	if subject == "" {
		return nil
	}
	return s.Update(ctx, fmt.Sprintf("DELETE WHERE { %s ?p ?o }", formatTerm(subject)))
}

// Update executes an arbitrary SPARQL UPDATE against the embedded store.
func (s *EmbeddedStore) Update(_ context.Context, sparql string) error {
	if err := s.store.Update(sparql); err != nil {
		return fmt.Errorf("oxigraph embedded update: %w", err)
	}
	return nil
}

// Query executes a SPARQL query and normalizes the binding's structured
// result into a KGQueryResult, matching the HTTP store's shape.
func (s *EmbeddedStore) Query(_ context.Context, sparql string) (repositories.KGQueryResult, error) {
	res, err := s.store.Query(sparql)
	if err != nil {
		return repositories.KGQueryResult{}, fmt.Errorf("oxigraph embedded query: %w", err)
	}
	return kgResult(res), nil
}

// LoadOntology bulk-loads a serialized RDF document into the default graph.
// The IANA media type is mapped to the binding's RdfFormat; an empty format
// defaults to Turtle, matching the HTTP store.
func (s *EmbeddedStore) LoadOntology(_ context.Context, format string, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	rdfFormat, ok := rdfFormatForMediaType(format)
	if !ok {
		return fmt.Errorf("oxigraph embedded: unsupported ontology media type %q", format)
	}
	if err := s.store.Load(bytes.NewReader(body), rdfFormat); err != nil {
		return fmt.Errorf("oxigraph embedded load (%s): %w", format, err)
	}
	return nil
}

// Clear removes every triple from the default graph.
func (s *EmbeddedStore) Clear(ctx context.Context) error {
	return s.Update(ctx, "CLEAR DEFAULT")
}

// IsEmpty reports whether the default graph has zero triples.
func (s *EmbeddedStore) IsEmpty(ctx context.Context) (bool, error) {
	res, err := s.Query(ctx, "ASK { ?s ?p ?o }")
	if err != nil {
		return false, err
	}
	if res.Boolean == nil {
		return false, fmt.Errorf("oxigraph embedded: ASK response missing boolean field")
	}
	return !*res.Boolean, nil
}

// wrapData renders "<keyword> { <t> . <t> . }" for INSERT DATA / DELETE DATA,
// reusing formatTriple so the term escaping matches the HTTP store exactly.
func wrapData(keyword string, triples []repositories.Triple) string {
	var sb strings.Builder
	sb.WriteString(keyword)
	sb.WriteString(" { ")
	for _, t := range triples {
		sb.WriteString(formatTriple(t))
		sb.WriteString(" ")
	}
	sb.WriteString("}")
	return sb.String()
}

// rdfFormatForMediaType maps the IANA media types LoadOntology receives to the
// binding's RdfFormat. Parameters (e.g. "; charset=utf-8") are ignored; an
// empty type defaults to Turtle, as the HTTP store does.
func rdfFormatForMediaType(mediaType string) (oxidb.RdfFormat, bool) {
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = mediaType[:i]
	}
	switch strings.TrimSpace(strings.ToLower(mediaType)) {
	case "", "text/turtle", "application/x-turtle":
		return oxidb.Turtle, true
	case "application/n-triples":
		return oxidb.NTriples, true
	case "application/n-quads":
		return oxidb.NQuads, true
	case "application/trig":
		return oxidb.TriG, true
	case "application/ld+json":
		return oxidb.JsonLd, true
	}
	return 0, false
}

// kgResult converts the binding's structured QueryResults into the store's
// backend-neutral KGQueryResult. SELECT columns come from res.Vars (exposed
// on the binding since v0.1.0-alpha.2); each solution is read back through
// Solution.Get for those variables.
func kgResult(res oxidb.QueryResults) repositories.KGQueryResult {
	switch res.Kind {
	case oxidb.QueryBoolean:
		b := res.Bool
		return repositories.KGQueryResult{Boolean: &b}
	case oxidb.QueryTriples:
		triples := make([]repositories.Triple, 0, len(res.Triples))
		for _, t := range res.Triples {
			triples = append(triples, repositories.Triple{
				Subject:   subjectValue(t.Subject),
				Predicate: t.Predicate.Value(),
				Object:    termValue(t.Object),
			})
		}
		return repositories.KGQueryResult{Triples: triples}
	default: // QuerySolutions
		out := repositories.KGQueryResult{
			Vars:     res.Vars,
			Bindings: make([]map[string]repositories.KGTerm, 0, len(res.Solutions)),
		}
		for _, sol := range res.Solutions {
			row := make(map[string]repositories.KGTerm, len(res.Vars))
			for _, v := range res.Vars {
				if term, ok := sol.Get(v); ok {
					row[v] = kgTerm(term)
				}
			}
			out.Bindings = append(out.Bindings, row)
		}
		return out
	}
}

// kgTerm maps one binding term to a KGTerm.
func kgTerm(t oxidb.Term) repositories.KGTerm {
	switch v := t.(type) {
	case oxidb.NamedNode:
		return repositories.KGTerm{Type: repositories.KGTermIRI, Value: v.Value()}
	case oxidb.Literal:
		term := repositories.KGTerm{
			Type:     repositories.KGTermLiteral,
			Value:    v.Value(),
			Datatype: v.Datatype().Value(),
		}
		if lang, ok := v.Language(); ok {
			term.Lang = lang
		}
		return term
	case oxidb.BlankNode:
		return repositories.KGTerm{Type: repositories.KGTermBlank, Value: v.Value()}
	default:
		return repositories.KGTerm{Value: t.String()}
	}
}

// subjectValue / termValue render a graph term for a CONSTRUCT/DESCRIBE
// Triple's string fields, matching the HTTP store's N-Triples parse: IRIs
// bare, literals in their quoted N-Triples form, blank nodes as _:id.
func subjectValue(s oxidb.Subject) string {
	if nn, ok := s.(oxidb.NamedNode); ok {
		return nn.Value()
	}
	return s.String()
}

func termValue(t oxidb.Term) string {
	if nn, ok := t.(oxidb.NamedNode); ok {
		return nn.Value()
	}
	return t.String()
}
