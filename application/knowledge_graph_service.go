package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"go.uber.org/fx"
)

// kgRedactedIRI is the sentinel substituted for forbidden object IRIs so the
// predicate stays visible (the LLM still learns "this resource has an owner")
// without exposing the hidden entity. Using a stable opaque marker (rather
// than "") avoids collisions with legitimate empty-string literals and gives
// downstream renderers a clear signal.
const kgRedactedIRI = "urn:weos:redacted"

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
	// substring). When classIRI is non-empty, results are restricted to
	// instances of that class (subclasses included via rdfs:subClassOf*).
	// limit defaults to 20, capped at 100.
	SearchEntities(ctx context.Context, q, classIRI string, limit int) ([]repositories.KGTerm, error)

	// DescribeClass returns the predicates and (optionally) example instances
	// of a class IRI. Useful for LLM introspection — "what does foaf:Person
	// look like in this graph?".
	DescribeClass(ctx context.Context, classIRI string, sampleInstances int) (ClassDescription, error)

	// FindPath returns a best-effort path of triples connecting two IRIs
	// within maxHops. Returns nil triples when no path is found within the
	// hop budget; nil error in that case (path-not-found is not an error).
	FindPath(ctx context.Context, from, to string, maxHops int) ([]repositories.Triple, error)

	// ListClasses returns the IRIs of every class declared in the graph
	// (anything typed `rdfs:Class` or `owl:Class`). Useful for LLM
	// introspection — "what kinds of things does this user's graph
	// contain?".
	ListClasses(ctx context.Context) ([]repositories.KGTerm, error)
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
//
// The ResourceService dependency is for the permission filter: every result
// returned to an MCP tool handler is passed through FilterAccessibleResourceIDs
// so the LLM never sees a resource the calling agent can't read. System
// context (nil identity) bypasses the filter — that matches the existing
// services' behavior for stdio/CLI/system callers.
func ProvideKnowledgeGraphService(params struct {
	fx.In
	Store    repositories.KnowledgeGraphStore
	Resource ResourceService
	Logger   entities.Logger
}) KnowledgeGraphService {
	return &knowledgeGraphService{
		store:    params.Store,
		resource: params.Resource,
		logger:   params.Logger,
	}
}

type knowledgeGraphService struct {
	store    repositories.KnowledgeGraphStore
	resource ResourceService
	logger   entities.Logger
}

func (s *knowledgeGraphService) Active() bool {
	return s.store != nil && s.store.Active()
}

func (s *knowledgeGraphService) Query(
	ctx context.Context, sparql string,
) (repositories.KGQueryResult, error) {
	if !s.Active() {
		return repositories.KGQueryResult{}, ErrKGUnavailable
	}
	if sparql == "" {
		return repositories.KGQueryResult{}, fmt.Errorf("sparql query is required")
	}
	s.logger.Debug(ctx, "kg query", "form", detectQuerySummary(sparql))
	res, err := s.store.Query(ctx, sparql)
	if err != nil {
		return res, err
	}
	// Apply permission filter on whichever shape the response carries.
	if res.Triples != nil {
		filtered, ferr := s.filterTriples(ctx, res.Triples)
		if ferr != nil {
			return repositories.KGQueryResult{}, ferr
		}
		res.Triples = filtered
	}
	if res.Bindings != nil {
		filtered, ferr := s.filterBindings(ctx, res.Bindings)
		if ferr != nil {
			return repositories.KGQueryResult{}, ferr
		}
		res.Bindings = filtered
	}
	if res.Boolean != nil && *res.Boolean && auth.AgentFromCtx(ctx) != nil {
		// ASK true on data the caller can't see is an existence leak: the
		// LLM can probe `ASK { <forbidden:iri> ?p ?o }` and learn that
		// gated resources exist. Without re-running the query under a
		// permission-aware rewrite (which Oxigraph doesn't support
		// natively), the safe call for non-system callers is to flip
		// true → false. System callers (auth.AgentFromCtx == nil)
		// retain the unfiltered answer to match every other service.
		falsy := false
		res.Boolean = &falsy
		s.logger.Debug(ctx, "kg ASK forced to false for non-system caller")
	}
	return res, nil
}

func (s *knowledgeGraphService) ExpandEntity(
	ctx context.Context, iri string, depth int,
) ([]repositories.Triple, error) {
	if !s.Active() {
		return nil, ErrKGUnavailable
	}
	if err := validateIRI(iri); err != nil {
		return nil, err
	}
	requested := depth
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		// Beyond 3 hops the result set explodes for typical graphs; the LLM
		// should chain ExpandEntity calls instead of asking for the full
		// closure.
		depth = 3
	}
	if requested != 0 && requested != depth {
		s.logger.Debug(ctx, "kg expand depth clamped",
			"requested", requested, "applied", depth)
	}
	q := buildExpandQuery(iri, depth)
	s.logger.Debug(ctx, "kg expand entity", "iri", iri, "depth", depth)
	res, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	if res.Triples == nil {
		return []repositories.Triple{}, nil
	}
	return s.filterTriples(ctx, res.Triples)
}

func (s *knowledgeGraphService) SearchEntities(
	ctx context.Context, q, classIRI string, limit int,
) ([]repositories.KGTerm, error) {
	if !s.Active() {
		return nil, ErrKGUnavailable
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query string is required")
	}
	if classIRI != "" {
		if err := validateIRI(classIRI); err != nil {
			return nil, fmt.Errorf("class iri: %w", err)
		}
	}
	// Clamp pathological lengths so a runaway LLM call can't wedge SPARQL
	// parsing on a multi-megabyte literal. 256 chars is well over what any
	// realistic name/title search needs.
	const maxQueryLen = 256
	if len(q) > maxQueryLen {
		q = q[:maxQueryLen]
		s.logger.Debug(ctx, "kg search query truncated", "max", maxQueryLen)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	sparql := buildSearchQuery(q, classIRI, limit)
	s.logger.Debug(ctx, "kg search entities", "q", q, "limit", limit)
	res, err := s.store.Query(ctx, sparql)
	if err != nil {
		return nil, err
	}
	filteredRows, ferr := s.filterBindings(ctx, res.Bindings)
	if ferr != nil {
		return nil, ferr
	}
	out := make([]repositories.KGTerm, 0, len(filteredRows))
	missing := 0
	for _, row := range filteredRows {
		if t, ok := row["s"]; ok {
			out = append(out, t)
			continue
		}
		missing++
	}
	if missing > 0 {
		s.logger.Debug(ctx, "kg search dropped bindings without ?s", "missing", missing)
	}
	return out, nil
}

func (s *knowledgeGraphService) DescribeClass(
	ctx context.Context, classIRI string, sampleInstances int,
) (ClassDescription, error) {
	if !s.Active() {
		return ClassDescription{}, ErrKGUnavailable
	}
	if err := validateIRI(classIRI); err != nil {
		return ClassDescription{}, err
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
		return ClassDescription{}, fmt.Errorf("describe class: predicate query failed: %w", err)
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
			// Predicates succeeded but instances failed — return zero-value
			// to avoid the partial-state trap where a non-Go-aware caller
			// reads `out` despite the error.
			return ClassDescription{}, fmt.Errorf("describe class: instance query failed: %w", err)
		}
		filteredRows, ferr := s.filterBindings(ctx, instRes.Bindings)
		if ferr != nil {
			return ClassDescription{}, ferr
		}
		for _, row := range filteredRows {
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
	if !s.Active() {
		return nil, ErrKGUnavailable
	}
	if err := validateIRI(from); err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	if err := validateIRI(to); err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	if maxHops <= 0 {
		maxHops = 4
	}
	if maxHops > 6 {
		// 6 is a soft ceiling that still covers most realistic chains;
		// each hop adds another query so cost grows linearly.
		maxHops = 6
	}
	s.logger.Debug(ctx, "kg find path", "from", from, "to", to, "hops", maxHops)

	// Iterate length 1..maxHops issuing a CONSTRUCT per length. Stop at
	// the first non-empty result — that's the shortest path; longer ones
	// would just add noise. Returns []Triple{} (not nil) when no path
	// exists so the caller can distinguish "ran successfully, no path"
	// from "didn't run at all".
	for length := 1; length <= maxHops; length++ {
		q := buildPathQueryForLength(from, to, length)
		res, err := s.store.Query(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("find path: length %d query failed: %w", length, err)
		}
		if len(res.Triples) > 0 {
			filtered, ferr := s.filterTriples(ctx, res.Triples)
			if ferr != nil {
				return nil, ferr
			}
			// Stop at the first length where the *unfiltered* query found a
			// path, regardless of whether the filter then emptied it.
			// Continuing to a longer length when the filter blanked us at
			// this length would tell the LLM "a shorter forbidden path
			// exists" via response time and the returned chain length —
			// a side-channel oracle. Return the (possibly empty) filtered
			// triples; the caller treats `[]` as "no path you're allowed
			// to see."
			s.logger.Debug(ctx, "kg find path: found",
				"length", length, "triples", len(filtered))
			return filtered, nil
		}
	}
	s.logger.Debug(ctx, "kg find path: no path within maxHops", "maxHops", maxHops)
	return []repositories.Triple{}, nil
}

// --- SPARQL builders ---

// validateIRI rejects strings that contain characters forbidden in an IRIREF
// (RFC 3987 / SPARQL grammar: control chars, `<>"{}|^\` and backtick, plus
// whitespace). The application boundary is where SPARQL injection has to be
// stopped — once formatIRI wraps it in <...>, an embedded `>` lets the LLM
// terminate the IRI early and inject arbitrary query syntax.
func validateIRI(iri string) error {
	if iri == "" {
		return fmt.Errorf("iri must not be empty")
	}
	const forbidden = "<>\"{}|^\\`"
	for i := 0; i < len(iri); i++ {
		c := iri[i]
		if c <= 0x20 || c == 0x7f || strings.IndexByte(forbidden, c) >= 0 {
			return fmt.Errorf("iri contains forbidden character at position %d", i)
		}
	}
	return nil
}

// formatIRI wraps a validated IRI in <...> for embedding into a SPARQL
// string. Callers MUST run validateIRI first; this function trusts its input.
func formatIRI(iri string) string {
	return "<" + iri + ">"
}

// buildExpandQuery returns a CONSTRUCT query for the N-hop forward
// neighborhood of an entity. We use a UNION of triple-pattern chains rather
// than a property-path quantifier (`{n,m}`) because the SPARQL 1.1 standard
// only defines `*`/`+`/`?` quantifiers — `{n,m}` is a Jena extension that
// Oxigraph rejects. UNION-of-chains is verbose but portable and lets the
// returned CONSTRUCT include every triple on the path, not just terminal
// edges.
func buildExpandQuery(iri string, depth int) string {
	subj := formatIRI(iri)
	if depth <= 1 {
		return fmt.Sprintf(`
CONSTRUCT { %s ?p ?o }
WHERE     { %s ?p ?o }`, subj, subj)
	}
	var construct, where strings.Builder
	for i := 1; i <= depth; i++ {
		if i > 1 {
			construct.WriteString(" .\n  ")
			where.WriteString("  UNION\n")
		}
		construct.WriteString(buildChainTriples(subj, "p", "x", i))
		where.WriteString("  { ")
		where.WriteString(buildChainTriples(subj, "p", "x", i))
		where.WriteString(" }\n")
	}
	return fmt.Sprintf(`
CONSTRUCT { %s }
WHERE {
%s}`, construct.String(), where.String())
}

// buildChainTriples renders i chained triple patterns starting at `subj`,
// using `?<predPrefix>1..i` for predicates and `?<varPrefix>1..i` for
// intermediate/final variables. Both BGPs (CONSTRUCT and WHERE) need
// identical patterns; we share the helper to keep them in sync.
//
// Example for i=2: `<urn:s> ?p1 ?x1 . ?x1 ?p2 ?x2`.
func buildChainTriples(subj, predPrefix, varPrefix string, i int) string {
	var sb strings.Builder
	sb.WriteString(subj)
	for n := 1; n <= i; n++ {
		fmt.Fprintf(&sb, " ?%s%d ?%s%d", predPrefix, n, varPrefix, n)
		if n < i {
			fmt.Fprintf(&sb, " . ?%s%d", varPrefix, n)
		}
	}
	return sb.String()
}

// buildSearchQuery returns a SELECT for IRIs with label-like predicates
// matching the user query. We let the SPARQL engine's LCASE do the
// case-folding on both sides — Go's strings.ToLower and SPARQL's LCASE
// disagree on non-ASCII (Turkish dotless I, German ß), so applying both
// would skip legitimate matches. The literal is escaped but its case is
// preserved going in.
//
// When classIRI is non-empty, results are restricted to instances of that
// class (or any rdfs:subClassOf descendant). Lets the LLM ask "find people
// named Alice" without first listing every Person subclass.
func buildSearchQuery(q, classIRI string, limit int) string {
	escaped := escapeSPARQLLiteral(q)
	classClause := ""
	if classIRI != "" {
		classClause = fmt.Sprintf(`
  ?s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type>/<http://www.w3.org/2000/01/rdf-schema#subClassOf>* %s .`,
			formatIRI(classIRI))
	}
	return fmt.Sprintf(`
SELECT DISTINCT ?s WHERE {%s
  ?s ?labelPred ?lbl .
  FILTER(?labelPred IN (
    <http://www.w3.org/2000/01/rdf-schema#label>,
    <http://schema.org/name>,
    <https://schema.org/name>,
    <http://xmlns.com/foaf/0.1/name>,
    <http://purl.org/dc/terms/title>
  ))
  FILTER(CONTAINS(LCASE(STR(?lbl)), LCASE("%s")))
} LIMIT %d`, classClause, escaped, limit)
}

// buildPathQueryForLength returns a CONSTRUCT for a single chain length:
// "is there a path of exactly N edges from `from` to `to`, and if so what
// triples are on it?". The caller composes a full path-find by running this
// for every length 1..maxHops and aggregating the triples — much cleaner
// (and portable) than trying to express the union in one query, which
// requires a CONSTRUCT template large enough to cover every chain length.
//
// Example length=2: matches `<from> ?p1 ?n1 . ?n1 ?p2 <to>` and returns
// both triples in the CONSTRUCT.
func buildPathQueryForLength(from, to string, length int) string {
	fromIRI := formatIRI(from)
	toIRI := formatIRI(to)
	if length <= 1 {
		return fmt.Sprintf(`
CONSTRUCT { %s ?p1 %s }
WHERE     { %s ?p1 %s }`, fromIRI, toIRI, fromIRI, toIRI)
	}
	var construct, where strings.Builder
	construct.WriteString(fromIRI)
	where.WriteString(fromIRI)
	for n := 1; n <= length; n++ {
		nodeStr := fmt.Sprintf("?n%d", n)
		if n == length {
			nodeStr = toIRI
		}
		fmt.Fprintf(&construct, " ?p%d %s", n, nodeStr)
		fmt.Fprintf(&where, " ?p%d %s", n, nodeStr)
		if n < length {
			fmt.Fprintf(&construct, " . ?n%d", n)
			fmt.Fprintf(&where, " . ?n%d", n)
		}
	}
	return fmt.Sprintf("CONSTRUCT { %s } WHERE { %s }", construct.String(), where.String())
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

func (s *knowledgeGraphService) ListClasses(
	ctx context.Context,
) ([]repositories.KGTerm, error) {
	if !s.Active() {
		return nil, ErrKGUnavailable
	}
	// Anything typed rdfs:Class or owl:Class — covers both the explicit
	// triples we emit (rdfs:Class) and ontology imports that use
	// owl:Class. Distinct in case the same IRI carries both types.
	q := `
SELECT DISTINCT ?s WHERE {
  ?s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> ?cls .
  FILTER(?cls IN (
    <http://www.w3.org/2000/01/rdf-schema#Class>,
    <http://www.w3.org/2002/07/owl#Class>
  ))
}`
	res, err := s.store.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]repositories.KGTerm, 0, len(res.Bindings))
	for _, row := range res.Bindings {
		if t, ok := row["s"]; ok {
			out = append(out, t)
		}
	}
	s.logger.Debug(ctx, "kg list classes", "count", len(out))
	return out, nil
}

// --- Permission filtering ---

// filterTriples drops triples whose subject the caller cannot read. Triples
// with a forbidden *object* are kept but the object is rewritten to the
// kgRedactedIRI sentinel so the LLM still sees the predicate but can't
// resolve the hidden entity. (Stripping the triple entirely would let the
// LLM infer existence via "this resource references X" patterns; rewriting
// preserves the predicate without leaking the IRI. Using a stable sentinel
// instead of "" prevents collision with legitimate empty literals.)
//
// Implementation: collect all distinct resource URNs across subjects and
// objects, ask ResourceService for the accessible subset, then walk the
// triples once filtering subjects and rewriting objects.
func (s *knowledgeGraphService) filterTriples(
	ctx context.Context, triples []repositories.Triple,
) ([]repositories.Triple, error) {
	if len(triples) == 0 {
		return triples, nil
	}
	candidates := uniqueGatedURNs(triples)
	if len(candidates) == 0 {
		return triples, nil
	}
	allowed, err := s.resource.FilterAccessibleResourceIDs(ctx, candidates)
	if err != nil {
		s.logger.Error(ctx, "kg permission filter failed",
			"error", err, "candidates", len(candidates))
		return nil, fmt.Errorf("kg permission filter: %w", err)
	}
	allowedSet := stringSet(allowed)
	var (
		out             = make([]repositories.Triple, 0, len(triples))
		droppedSubjects int
		hiddenObjects   int
	)
	for _, t := range triples {
		if isGatedResourceURN(t.Subject) && !allowedSet[t.Subject] {
			droppedSubjects++
			continue
		}
		if isGatedResourceURN(t.Object) && !allowedSet[t.Object] {
			t.Object = kgRedactedIRI
			hiddenObjects++
		}
		out = append(out, t)
	}
	if droppedSubjects > 0 || hiddenObjects > 0 {
		s.logger.Debug(ctx, "kg permission filter applied",
			"dropped_subjects", droppedSubjects, "hidden_objects", hiddenObjects)
	}
	return out, nil
}

// filterBindings drops SELECT rows that bind any forbidden gated URN to ANY
// variable. Free-form Query() lets the LLM project arbitrary names
// (?owner, ?friend, ?subject, …); gating only ?s would let an LLM trivially
// rename its way around the filter. We collect every IRI cell across every
// row, ask the resource service for the accessible subset in one bulk call,
// then drop any row that contains a forbidden cell.
func (s *knowledgeGraphService) filterBindings(
	ctx context.Context, bindings []map[string]repositories.KGTerm,
) ([]map[string]repositories.KGTerm, error) {
	if len(bindings) == 0 {
		return bindings, nil
	}
	var candidates []string
	seen := map[string]bool{}
	for _, row := range bindings {
		for _, t := range row {
			if t.Type == repositories.KGTermIRI && isGatedResourceURN(t.Value) && !seen[t.Value] {
				seen[t.Value] = true
				candidates = append(candidates, t.Value)
			}
		}
	}
	if len(candidates) == 0 {
		return bindings, nil
	}
	allowed, err := s.resource.FilterAccessibleResourceIDs(ctx, candidates)
	if err != nil {
		s.logger.Error(ctx, "kg permission filter failed",
			"error", err, "candidates", len(candidates))
		return nil, fmt.Errorf("kg permission filter: %w", err)
	}
	allowedSet := stringSet(allowed)
	out := make([]map[string]repositories.KGTerm, 0, len(bindings))
	dropped := 0
	for _, row := range bindings {
		if rowHasForbiddenIRI(row, allowedSet) {
			dropped++
			continue
		}
		out = append(out, row)
	}
	if dropped > 0 {
		s.logger.Debug(ctx, "kg permission filter dropped bindings", "dropped", dropped)
	}
	return out, nil
}

// rowHasForbiddenIRI reports whether any IRI cell in the binding row is a
// gated URN absent from the allowedSet. Used by filterBindings to drop
// rows that project a forbidden IRI under any variable name.
func rowHasForbiddenIRI(row map[string]repositories.KGTerm, allowedSet map[string]bool) bool {
	for _, t := range row {
		if t.Type == repositories.KGTermIRI && isGatedResourceURN(t.Value) && !allowedSet[t.Value] {
			return true
		}
	}
	return false
}

// uniqueGatedURNs returns the distinct gated resource URNs found in the
// subject/object positions of `triples`. "Gated" here means anything
// isGatedResourceURN treats as needing a permission check —
// `urn:<typeSlug>:<ksuid>` resources plus `urn:person:*` / `urn:org:*`
// (FOAF/vCard PII). Duplicates and truly non-gated terms (literals,
// ontology IRIs, blank nodes, `urn:type:*`, `urn:theme:*`) are skipped.
func uniqueGatedURNs(triples []repositories.Triple) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range triples {
		for _, term := range []string{t.Subject, t.Object} {
			if isGatedResourceURN(term) && !seen[term] {
				seen[term] = true
				out = append(out, term)
			}
		}
	}
	return out
}

func stringSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
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
