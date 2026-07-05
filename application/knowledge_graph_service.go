package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/sparql"

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
	ClassIRI   string                `json:"class_iri"`
	Predicates []string              `json:"predicates"`
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
	Stores   repositories.KnowledgeGraphStores
	Resource ResourceService
	Logger   entities.Logger
}) KnowledgeGraphService {
	return &knowledgeGraphService{
		stores:   params.Stores,
		resource: params.Resource,
		logger:   params.Logger,
	}
}

type knowledgeGraphService struct {
	stores   repositories.KnowledgeGraphStores
	resource ResourceService
	logger   entities.Logger
}

func (s *knowledgeGraphService) Active() bool {
	return s.stores != nil && s.stores.Active()
}

// storeFor resolves the KnowledgeGraphStore this request should read. In
// single-tenant mode it's the one process store. In per-account mode it's the
// caller's account store, resolved from request identity the same way the
// resource services scope visibility:
//
//   - identity with an active account -> that account's store;
//   - no account over the LOCAL stdio transport -> the reserved local graph
//     (the stdio exception, so a local operator still has a working graph);
//   - no account over any other (remote) transport -> ErrKGUnavailable, so an
//     unauthenticated multi-tenant request is refused rather than served a
//     default/global graph.
//
// A resolved-but-inactive store (single-tenant nop) also yields ErrKGUnavailable
// so tool handlers report "not configured" instead of empty results.
func (s *knowledgeGraphService) storeFor(ctx context.Context) (repositories.KnowledgeGraphStore, error) {
	if s.stores == nil {
		return nil, ErrKGUnavailable
	}
	accountID, err := s.resolveAccountID(ctx)
	if err != nil {
		return nil, err
	}
	store, err := s.stores.ForAccount(ctx, accountID)
	if err != nil {
		s.logger.Debug(ctx, "kg store resolution failed", "error", err)
		return nil, ErrKGUnavailable
	}
	if store == nil || !store.Active() {
		return nil, ErrKGUnavailable
	}
	return store, nil
}

// resolveAccountID returns the account id storeFor should route to. Single-tenant
// mode ignores the account (empty id -> the one store). Per-account mode reads
// the caller's active account, applying the local-transport exception and the
// remote fail-closed rule.
func (s *knowledgeGraphService) resolveAccountID(ctx context.Context) (string, error) {
	if !s.stores.PerAccount() {
		return "", nil
	}
	if id := auth.AgentFromCtx(ctx); id != nil && id.ActiveAccountID != "" {
		return id.ActiveAccountID, nil
	}
	if isLocalTransport(ctx) {
		return LocalAccountID, nil
	}
	return "", ErrKGUnavailable
}

func (s *knowledgeGraphService) Query(
	ctx context.Context, sparql string,
) (repositories.KGQueryResult, error) {
	store, err := s.storeFor(ctx)
	if err != nil {
		return repositories.KGQueryResult{}, err
	}
	if sparql == "" {
		return repositories.KGQueryResult{}, fmt.Errorf("sparql query is required")
	}
	if err := s.guardScopedQuery(ctx, sparql); err != nil {
		return repositories.KGQueryResult{}, err
	}
	s.logger.Debug(ctx, "kg query", "form", detectQuerySummary(sparql))
	res, err := store.Query(ctx, sparql)
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
		// Scoped callers: require a permission-filterable resource IRI in each
		// returned row, so predicate/literal-only projections can't leak data
		// from forbidden resources. System callers get rows unchanged.
		requireGatedIRI := auth.AgentFromCtx(ctx) != nil
		filtered, ferr := s.filterBindings(ctx, res.Bindings, requireGatedIRI)
		if ferr != nil {
			return repositories.KGQueryResult{}, ferr
		}
		res.Bindings = filtered
	}
	// res.Boolean (ASK) needs no post-filtering here: guardScopedQuery rejects
	// ASK for scoped callers (a bare boolean has no IRI for the filter to
	// remove, so it can't be made safe after the fact). Only system callers
	// reach this point with an ASK, and they get the unfiltered answer to
	// match every other service.
	return res, nil
}

// ErrKGQueryNotPermitted is returned when a scoped (non-system) caller submits
// a free-form SPARQL query whose results cannot be permission-filtered after
// execution. Post-execution filtering only works when forbidden data surfaces
// as a gated IRI in the result rows; it cannot redact a bare boolean (ASK),
// an aggregate literal (COUNT/SUM/…), or a row deliberately built to omit the
// forbidden subject. Those shapes are refused for scoped callers. Row-
// projecting SELECT/CONSTRUCT/DESCRIBE queries and the structured KG tools
// (expand/search/describe/path) remain available. System callers (nil
// identity) are unaffected.
var ErrKGQueryNotPermitted = errors.New("knowledge graph query form not permitted for this caller")

// guardScopedQuery rejects free-form SPARQL shapes that would leak forbidden
// graph state past the post-execution permission filter. No-op for system
// callers (nil identity).
func (s *knowledgeGraphService) guardScopedQuery(ctx context.Context, sparql string) error {
	if auth.AgentFromCtx(ctx) == nil {
		return nil // system caller: full, unfiltered access
	}
	switch detectQuerySummary(sparql) {
	case "ASK":
		// A boolean answer carries no IRI for the post-filter to remove, so
		// even an unbound pattern like `ASK { ?s <https://schema.org/ssn> ?o }`
		// would leak whether any (possibly forbidden) resource exists.
		return fmt.Errorf("%w: ASK queries are not available; project the entities "+
			"you need with SELECT, or use the structured knowledge-graph tools", ErrKGQueryNotPermitted)
	case "SELECT":
		if isAggregateQuery(sparql) {
			// Aggregates collapse rows into literals (COUNT, SUM, GROUP_CONCAT,
			// …) with no gated IRI left to filter, leaking counts/derived facts
			// about forbidden data.
			return fmt.Errorf("%w: aggregate or GROUP BY queries are not available; "+
				"project the underlying rows instead", ErrKGQueryNotPermitted)
		}
	}
	// Refuse any query that explicitly names a gated resource the caller
	// cannot read (e.g. `SELECT ?p WHERE { <urn:product:secret> ?p ?o }`,
	// which would leak the predicates of a hidden resource without ever
	// projecting its IRI). Forbidden and non-existent URNs are refused
	// identically, so this is not an existence oracle.
	named := gatedURNsInQuery(sparql)
	if len(named) == 0 {
		return nil
	}
	allowed, err := s.resource.FilterAccessibleResourceIDs(ctx, named)
	if err != nil {
		s.logger.Error(ctx, "kg query guard permission check failed", "error", err)
		return fmt.Errorf("kg query guard: %w", err)
	}
	allowedSet := stringSet(allowed)
	for _, u := range named {
		if !allowedSet[u] {
			return fmt.Errorf("%w: query references a resource you cannot access", ErrKGQueryNotPermitted)
		}
	}
	return nil
}

func (s *knowledgeGraphService) ExpandEntity(
	ctx context.Context, iri string, depth int,
) ([]repositories.Triple, error) {
	store, err := s.storeFor(ctx)
	if err != nil {
		return nil, err
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
	res, err := store.Query(ctx, q)
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
	store, err := s.storeFor(ctx)
	if err != nil {
		return nil, err
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
	res, err := store.Query(ctx, sparql)
	if err != nil {
		return nil, err
	}
	filteredRows, ferr := s.filterBindings(ctx, res.Bindings, false)
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
	store, err := s.storeFor(ctx)
	if err != nil {
		return ClassDescription{}, err
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

	// Distinct (subject, predicate) pairs used by instances of the class.
	// Property paths (`rdf:type/rdfs:subClassOf*`) walk the subclass chain so
	// subclasses are included automatically. We project ?s alongside ?p so the
	// permission filter can drop predicates that occur ONLY on forbidden
	// instances — collecting predicates without ?s would leak the existence of
	// properties carried solely by resources the caller can't read.
	predQuery := fmt.Sprintf(`
SELECT DISTINCT ?s ?p WHERE {
  ?s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type>/<http://www.w3.org/2000/01/rdf-schema#subClassOf>* %s .
  ?s ?p ?o .
}`, formatIRI(classIRI))
	predRes, err := store.Query(ctx, predQuery)
	if err != nil {
		return ClassDescription{}, fmt.Errorf("describe class: predicate query failed: %w", err)
	}
	filteredPredRows, ferr := s.filterBindings(ctx, predRes.Bindings, false)
	if ferr != nil {
		return ClassDescription{}, ferr
	}
	out := ClassDescription{ClassIRI: classIRI}
	predSeen := map[string]bool{}
	for _, row := range filteredPredRows {
		if t, ok := row["p"]; ok && t.Value != "" && !predSeen[t.Value] {
			predSeen[t.Value] = true
			out.Predicates = append(out.Predicates, t.Value)
		}
	}

	if sampleInstances > 0 {
		instQuery := fmt.Sprintf(`
SELECT ?s WHERE {
  ?s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type>/<http://www.w3.org/2000/01/rdf-schema#subClassOf>* %s .
} LIMIT %d`, formatIRI(classIRI), sampleInstances)
		instRes, err := store.Query(ctx, instQuery)
		if err != nil {
			// Predicates succeeded but instances failed — return zero-value
			// to avoid the partial-state trap where a non-Go-aware caller
			// reads `out` despite the error.
			return ClassDescription{}, fmt.Errorf("describe class: instance query failed: %w", err)
		}
		filteredRows, ferr := s.filterBindings(ctx, instRes.Bindings, false)
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
	store, err := s.storeFor(ctx)
	if err != nil {
		return nil, err
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
		res, err := store.Query(ctx, q)
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
	store, err := s.storeFor(ctx)
	if err != nil {
		return nil, err
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
	res, err := store.Query(ctx, q)
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
//
// When requireGatedIRI is set (free-form queries from scoped callers), a row
// is ALSO dropped unless it contains at least one gated resource IRI. Without
// this, a projection that returns only predicates or literals
// (`SELECT ?email WHERE { ?s <https://schema.org/email> ?email }`, or a
// prefix-named subject like `PREFIX r: <urn:product:> SELECT ?p WHERE
// { r:secret ?p ?o }`) has no gated cell for the filter to catch and would
// leak data derived from forbidden resources. Structured tools pass false —
// they build their own shapes and legitimately return ontology rows.
func (s *knowledgeGraphService) filterBindings(
	ctx context.Context, bindings []map[string]repositories.KGTerm, requireGatedIRI bool,
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
	if len(candidates) == 0 && !requireGatedIRI {
		return bindings, nil
	}
	allowedSet := map[string]bool{}
	if len(candidates) > 0 {
		allowed, err := s.resource.FilterAccessibleResourceIDs(ctx, candidates)
		if err != nil {
			s.logger.Error(ctx, "kg permission filter failed",
				"error", err, "candidates", len(candidates))
			return nil, fmt.Errorf("kg permission filter: %w", err)
		}
		allowedSet = stringSet(allowed)
	}
	out := make([]map[string]repositories.KGTerm, 0, len(bindings))
	dropped := 0
	for _, row := range bindings {
		if rowHasForbiddenIRI(row, allowedSet) {
			dropped++
			continue
		}
		if requireGatedIRI && !rowHasGatedIRI(row) {
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

// rowHasGatedIRI reports whether the row binds at least one gated resource IRI
// (in any variable) — i.e. a value the permission filter can actually check.
func rowHasGatedIRI(row map[string]repositories.KGTerm) bool {
	for _, t := range row {
		if t.Type == repositories.KGTermIRI && isGatedResourceURN(t.Value) {
			return true
		}
	}
	return false
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

var (
	// kgAggregateFuncRe matches a SPARQL aggregate function call. Lexical and
	// deliberately broad — a false positive only refuses a scoped query (the
	// safe direction), never leaks.
	kgAggregateFuncRe = regexp.MustCompile(`(?i)\b(count|sum|avg|min|max|sample|group_concat)\s*\(`)
	kgGroupByRe       = regexp.MustCompile(`(?i)\bgroup\s+by\b`)
)

// isAggregateQuery reports whether the SPARQL text uses an aggregate function
// or GROUP BY, both of which collapse rows into literals that the gated-IRI
// post-filter cannot inspect.
func isAggregateQuery(sparql string) bool {
	return kgAggregateFuncRe.MatchString(sparql) || kgGroupByRe.MatchString(sparql)
}

// gatedURNsInQuery returns the distinct gated resource URNs named explicitly
// (as <...> IRIs) in the SPARQL text. Used by guardScopedQuery to refuse
// queries that point at a resource the caller cannot read.
func gatedURNsInQuery(sparql string) []string {
	seen := map[string]bool{}
	var out []string
	for _, iri := range bracketedIRIs(sparql) {
		if isGatedResourceURN(iri) && !seen[iri] {
			seen[iri] = true
			out = append(out, iri)
		}
	}
	return out
}

// bracketedIRIs returns the contents of every `<...>` IRI reference in the
// SPARQL text. It is a lexical scan, not a full SPARQL parse — sufficient for
// spotting explicitly-named resource URNs, which is all the ASK guard needs.
func bracketedIRIs(sparql string) []string {
	var out []string
	for {
		open := strings.IndexByte(sparql, '<')
		if open < 0 {
			break
		}
		sparql = sparql[open+1:]
		end := strings.IndexByte(sparql, '>')
		if end < 0 {
			break
		}
		out = append(out, sparql[:end])
		sparql = sparql[end+1:]
	}
	return out
}

// detectQuerySummary returns a short tag for the query form. It delegates to
// sparql.DetectForm, which strips the prologue (comments + PREFIX/BASE,
// including inline single-line prologues) before reading the keyword. This
// matters for the scoped-query guard: a caller must not be able to slip an
// ASK or aggregate past the guard by prefixing it with `PREFIX`/`BASE`.
// Sharing the helper with the store's detectQueryForm keeps the two from
// drifting.
func detectQuerySummary(q string) string {
	return sparql.DetectForm(q)
}
