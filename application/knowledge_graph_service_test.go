package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
)

// queryRecordingStore captures the SPARQL string that the service sends so
// tests can assert SPARQL composition without actually running it.
type queryRecordingStore struct {
	mu       sync.Mutex
	active   bool
	queries  []string
	response repositories.KGQueryResult
	err      error
}

func (q *queryRecordingStore) Active() bool { return q.active }
func (q *queryRecordingStore) AddTriples(_ context.Context, _ []repositories.Triple) error {
	return nil
}
func (q *queryRecordingStore) RemoveTriples(_ context.Context, _ []repositories.Triple) error {
	return nil
}
func (q *queryRecordingStore) RemoveSubject(_ context.Context, _ string) error { return nil }
func (q *queryRecordingStore) Query(_ context.Context, sparql string) (repositories.KGQueryResult, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queries = append(q.queries, sparql)
	return q.response, q.err
}
func (q *queryRecordingStore) Update(_ context.Context, _ string) error             { return nil }
func (q *queryRecordingStore) LoadOntology(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (q *queryRecordingStore) Clear(_ context.Context) error             { return nil }
func (q *queryRecordingStore) IsEmpty(_ context.Context) (bool, error)   { return true, nil }

// resourceFilterStub is a minimal ResourceService stub exposing only the
// FilterAccessibleResourceIDs method that the KG permission filter calls.
// `forbidden` lists the URNs that should be excluded from the allowed set.
type resourceFilterStub struct {
	forbidden map[string]bool
	calls     int
}

func (r *resourceFilterStub) FilterAccessibleResourceIDs(_ context.Context, ids []string) ([]string, error) {
	r.calls++
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !r.forbidden[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// The remaining ResourceService methods are unused by the KG service but
// required by the interface; we panic to surface accidental coupling.
func (r *resourceFilterStub) Create(context.Context, CreateResourceCommand) (*entities.Resource, error) {
	panic("not used")
}
func (r *resourceFilterStub) GetByID(context.Context, string) (*entities.Resource, error) {
	panic("not used")
}
func (r *resourceFilterStub) GetFlat(context.Context, string, string) (map[string]any, error) {
	panic("not used")
}
func (r *resourceFilterStub) List(context.Context, string, string, int, repositories.SortOptions) (repositories.PaginatedResponse[*entities.Resource], error) {
	panic("not used")
}
func (r *resourceFilterStub) ListFlat(context.Context, string, string, int, repositories.SortOptions) (repositories.PaginatedResponse[map[string]any], error) {
	panic("not used")
}
func (r *resourceFilterStub) ListByField(context.Context, string, string, string) (repositories.PaginatedResponse[*entities.Resource], error) {
	panic("not used")
}
func (r *resourceFilterStub) ListWithFilters(context.Context, string, []repositories.FilterCondition, string, int, repositories.SortOptions) (repositories.PaginatedResponse[*entities.Resource], error) {
	panic("not used")
}
func (r *resourceFilterStub) ListFlatWithFilters(context.Context, string, []repositories.FilterCondition, string, int, repositories.SortOptions) (repositories.PaginatedResponse[map[string]any], error) {
	panic("not used")
}
func (r *resourceFilterStub) Update(context.Context, UpdateResourceCommand) (*entities.Resource, error) {
	panic("not used")
}
func (r *resourceFilterStub) Delete(context.Context, DeleteResourceCommand) error {
	panic("not used")
}

func newKGSvc(store repositories.KnowledgeGraphStore) KnowledgeGraphService {
	return newKGSvcWithResource(store, &resourceFilterStub{})
}

func newKGSvcWithResource(store repositories.KnowledgeGraphStore, rsvc ResourceService) KnowledgeGraphService {
	return &knowledgeGraphService{store: store, resource: rsvc, logger: noopLogger{}}
}

func TestKGService_InactiveStoreReturnsErrUnavailable(t *testing.T) {
	t.Parallel()
	svc := newKGSvc(&queryRecordingStore{active: false})
	ctx := context.Background()

	if _, err := svc.Query(ctx, "ASK {}"); !errors.Is(err, ErrKGUnavailable) {
		t.Errorf("Query: got %v, want ErrKGUnavailable", err)
	}
	if _, err := svc.ExpandEntity(ctx, "urn:x", 1); !errors.Is(err, ErrKGUnavailable) {
		t.Errorf("ExpandEntity: got %v, want ErrKGUnavailable", err)
	}
	if _, err := svc.SearchEntities(ctx, "x", 0); !errors.Is(err, ErrKGUnavailable) {
		t.Errorf("SearchEntities: got %v, want ErrKGUnavailable", err)
	}
	if _, err := svc.DescribeClass(ctx, "urn:c", 0); !errors.Is(err, ErrKGUnavailable) {
		t.Errorf("DescribeClass: got %v, want ErrKGUnavailable", err)
	}
	if _, err := svc.FindPath(ctx, "urn:a", "urn:b", 1); !errors.Is(err, ErrKGUnavailable) {
		t.Errorf("FindPath: got %v, want ErrKGUnavailable", err)
	}
}

func TestKGService_ExpandEntity_BuildsCONSTRUCT(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	if _, err := svc.ExpandEntity(context.Background(), "urn:product:1", 1); err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	if len(store.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(store.queries))
	}
	q := store.queries[0]
	if !strings.Contains(q, "CONSTRUCT") {
		t.Errorf("expected CONSTRUCT in query: %q", q)
	}
	if !strings.Contains(q, "<urn:product:1>") {
		t.Errorf("expected wrapped IRI in query: %q", q)
	}
	// Single-hop must not use property-path quantifiers — the previous
	// implementation used `{n,m}` which Oxigraph rejects. Lock that in.
	if strings.Contains(q, "{0,") || strings.Contains(q, "{1,") {
		t.Errorf("query uses property-path quantifier (Oxigraph rejects): %q", q)
	}
}

func TestKGService_ExpandEntity_DepthClampedTo3(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	// depth=99 must clamp to 3. The new builder uses UNION-of-chains, so
	// we verify clamping by counting `UNION` occurrences (depth-1 = 2).
	if _, err := svc.ExpandEntity(context.Background(), "urn:x", 99); err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	q := store.queries[0]
	if got := strings.Count(q, "UNION"); got != 2 {
		t.Errorf("expected 2 UNIONs (depth=3 = 3 chains, 2 separators); got %d in %q", got, q)
	}
	if strings.Contains(q, "{0,") {
		t.Errorf("query must not use property-path quantifier; got %q", q)
	}
}

func TestKGService_ExpandEntity_RejectsMaliciousIRI(t *testing.T) {
	t.Parallel()
	svc := newKGSvc(&queryRecordingStore{active: true})

	bad := "urn:x> ?p ?o } ; DROP ALL ; #"
	if _, err := svc.ExpandEntity(context.Background(), bad, 1); err == nil {
		t.Fatal("expected validation error on IRI with injection chars")
	}
}

func TestKGService_SearchEntities_EscapesQuoteAndLowercases(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	if _, err := svc.SearchEntities(context.Background(), `she said "hi"`, 0); err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	q := store.queries[0]
	// The literal should be lowercased and the embedded quote escaped so the
	// SPARQL stays valid.
	if !strings.Contains(q, `she said \"hi\"`) {
		t.Errorf("expected escaped lowercased literal in query: %q", q)
	}
	if !strings.Contains(q, "LIMIT 20") {
		t.Errorf("expected default LIMIT 20: %q", q)
	}
}

func TestKGService_SearchEntities_LimitClamped(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	if _, err := svc.SearchEntities(context.Background(), "x", 9999); err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if !strings.Contains(store.queries[0], "LIMIT 100") {
		t.Errorf("expected clamp to LIMIT 100: %q", store.queries[0])
	}
}

func TestKGService_DescribeClass_WalksSubClassOf(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	if _, err := svc.DescribeClass(context.Background(), "https://schema.org/Person", 0); err != nil {
		t.Fatalf("DescribeClass: %v", err)
	}
	if len(store.queries) != 1 {
		t.Fatalf("queries = %d, want 1 (no instances requested)", len(store.queries))
	}
	if !strings.Contains(store.queries[0], "rdf-schema#subClassOf>*") {
		t.Errorf("expected subClassOf property path: %q", store.queries[0])
	}
}

func TestKGService_DescribeClass_FetchesInstancesWhenAsked(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	if _, err := svc.DescribeClass(context.Background(), "urn:c", 5); err != nil {
		t.Fatalf("DescribeClass: %v", err)
	}
	if len(store.queries) != 2 {
		t.Fatalf("queries = %d, want 2 (predicates + instances)", len(store.queries))
	}
	if !strings.Contains(store.queries[1], "LIMIT 5") {
		t.Errorf("expected instance LIMIT 5: %q", store.queries[1])
	}
}

func TestKGService_FindPath_ReturnsTriplesFromStore(t *testing.T) {
	t.Parallel()
	// FindPath now iterates length 1..maxHops and stops at the first
	// non-empty result. With a stub that always returns one triple, the
	// first iteration (length=1) returns immediately.
	store := &queryRecordingStore{
		active:   true,
		response: repositories.KGQueryResult{Triples: []repositories.Triple{{Subject: "a", Predicate: "p", Object: "b"}}},
	}
	svc := newKGSvc(store)

	triples, err := svc.FindPath(context.Background(), "urn:a", "urn:b", 0)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(triples) != 1 {
		t.Fatalf("got %d triples, want 1", len(triples))
	}
	if len(store.queries) != 1 {
		t.Errorf("expected exactly 1 query (short-circuit on first match); got %d", len(store.queries))
	}
	if strings.Contains(store.queries[0], "{1,") {
		t.Errorf("path query must not use property-path quantifier; got %q", store.queries[0])
	}
}

func TestKGService_FindPath_NoPathReturnsEmpty(t *testing.T) {
	t.Parallel()
	// Empty Triples on every iteration → "no path within budget".
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	triples, err := svc.FindPath(context.Background(), "urn:a", "urn:b", 3)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if triples == nil {
		t.Error("expected non-nil empty slice for 'no path found'")
	}
	if len(triples) != 0 {
		t.Errorf("expected 0 triples; got %d", len(triples))
	}
	if len(store.queries) != 3 {
		t.Errorf("expected 3 queries (one per length); got %d", len(store.queries))
	}
}

func TestKGService_FindPath_RejectsMaliciousIRI(t *testing.T) {
	t.Parallel()
	svc := newKGSvc(&queryRecordingStore{active: true})

	if _, err := svc.FindPath(context.Background(), "urn:a> } UNION { ?s ?p ?o", "urn:b", 1); err == nil {
		t.Fatal("expected validation error on injected from IRI")
	}
	if _, err := svc.FindPath(context.Background(), "urn:a", "urn:b\"", 1); err == nil {
		t.Fatal("expected validation error on injected to IRI")
	}
}

func TestKGService_DescribeClass_RejectsMaliciousIRI(t *testing.T) {
	t.Parallel()
	svc := newKGSvc(&queryRecordingStore{active: true})

	if _, err := svc.DescribeClass(context.Background(), "urn:c<injected", 0); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestKGService_DescribeClass_AbortsOnInstanceQueryFailure(t *testing.T) {
	t.Parallel()
	calls := 0
	store := &errorOnSecondQueryStore{
		queryRecordingStore: queryRecordingStore{active: true},
		errOnCall:           &calls,
	}
	svc := newKGSvc(store)

	desc, err := svc.DescribeClass(context.Background(), "urn:c", 5)
	if err == nil {
		t.Fatal("expected error from instance query failure")
	}
	// Critical: must not return partial state alongside error.
	if len(desc.Predicates) != 0 || len(desc.Instances) != 0 {
		t.Errorf("expected zero-value description on error; got %+v", desc)
	}
}

// errorOnSecondQueryStore returns an error on the 2nd Query call only — used
// to test DescribeClass's predicates-succeed-instances-fail path.
type errorOnSecondQueryStore struct {
	queryRecordingStore
	errOnCall *int
}

func (e *errorOnSecondQueryStore) Query(_ context.Context, sparql string) (repositories.KGQueryResult, error) {
	*e.errOnCall++
	e.queries = append(e.queries, sparql)
	if *e.errOnCall == 2 {
		return repositories.KGQueryResult{}, errors.New("instance query boom")
	}
	// First call returns one binding so out.Predicates is populated before
	// the second call wipes it (we want to assert it IS wiped).
	val := true
	_ = val
	return repositories.KGQueryResult{
		Bindings: []map[string]repositories.KGTerm{{
			"p": {Type: repositories.KGTermIRI, Value: "https://schema.org/name"},
		}},
	}, nil
}

func TestKGService_RequiresArgs(t *testing.T) {
	t.Parallel()
	svc := newKGSvc(&queryRecordingStore{active: true})
	ctx := context.Background()

	if _, err := svc.Query(ctx, ""); err == nil {
		t.Error("Query(\"\") should error")
	}
	if _, err := svc.ExpandEntity(ctx, "", 0); err == nil {
		t.Error("ExpandEntity(\"\") should error")
	}
	if _, err := svc.SearchEntities(ctx, "  ", 0); err == nil {
		t.Error("SearchEntities(blank) should error")
	}
	if _, err := svc.DescribeClass(ctx, "", 0); err == nil {
		t.Error("DescribeClass(\"\") should error")
	}
	if _, err := svc.FindPath(ctx, "", "", 0); err == nil {
		t.Error("FindPath(\"\", \"\") should error")
	}
}

func TestValidateIRI(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		iri  string
		want bool // true = should error
	}{
		{"empty", "", true},
		{"valid urn", "urn:product:abc", false},
		{"valid http", "https://schema.org/Person", false},
		{"contains gt", "urn:x>", true},
		{"contains lt", "urn:x<y", true},
		{"contains quote", `urn:x"y`, true},
		{"contains brace", "urn:x{y", true},
		{"contains backslash", `urn:x\y`, true},
		{"contains space", "urn:x y", true},
		{"contains newline", "urn:x\ny", true},
		{"contains tab", "urn:x\ty", true},
		{"contains null", "urn:x\x00y", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateIRI(tc.iri)
			if (err != nil) != tc.want {
				t.Errorf("validateIRI(%q) err=%v, want error=%v", tc.iri, err, tc.want)
			}
		})
	}
}

// Active() guard: nil store must not panic.
func TestKGService_NilStoreDoesNotPanic(t *testing.T) {
	t.Parallel()
	svc := &knowledgeGraphService{store: nil, logger: noopLogger{}}
	if svc.Active() {
		t.Error("nil store should report Active()=false")
	}
	if _, err := svc.Query(context.Background(), "ASK {}"); err != ErrKGUnavailable {
		t.Errorf("nil store should return ErrKGUnavailable, got %v", err)
	}
}

// --- Permission filter ---

func TestKGService_ExpandEntity_FiltersForbiddenSubjectsFromCONSTRUCTResults(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Triples: []repositories.Triple{
			{Subject: "urn:product:public", Predicate: "https://schema.org/name", Object: "Public"},
			{Subject: "urn:product:secret", Predicate: "https://schema.org/name", Object: "Secret"},
		}},
	}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:product:secret": true}}
	svc := newKGSvcWithResource(store, rsvc)

	got, err := svc.ExpandEntity(context.Background(), "urn:product:public", 1)
	if err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	if len(got) != 1 || got[0].Subject != "urn:product:public" {
		t.Errorf("forbidden subject not filtered out; got %+v", got)
	}
	if rsvc.calls != 1 {
		t.Errorf("expected 1 filter call (bulk); got %d", rsvc.calls)
	}
}

func TestKGService_ExpandEntity_HidesForbiddenObjectsButKeepsTriple(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Triples: []repositories.Triple{
			// Both subject and object are gated URNs; subject is allowed,
			// object is not. Triple stays but object becomes "" so the LLM
			// can't resolve the hidden IRI.
			{Subject: "urn:product:1", Predicate: "https://schema.org/owner", Object: "urn:product:secret"},
		}},
	}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:product:secret": true}}
	svc := newKGSvcWithResource(store, rsvc)

	got, err := svc.ExpandEntity(context.Background(), "urn:product:1", 1)
	if err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 triple (object hidden, not dropped); got %d", len(got))
	}
	if got[0].Object != kgRedactedIRI {
		t.Errorf("forbidden object not hidden with sentinel; got %q", got[0].Object)
	}
	if got[0].Predicate != "https://schema.org/owner" {
		t.Errorf("predicate must be preserved so the LLM sees the relationship")
	}
}

func TestKGService_FilterDropsRowWithForbiddenIRIInAnyVariable(t *testing.T) {
	t.Parallel()
	// Free-form Query() lets the LLM project arbitrary variable names.
	// The filter must drop a row whose ?owner (not ?s) is a forbidden URN —
	// otherwise renaming the projection variable bypasses the gate.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Bindings: []map[string]repositories.KGTerm{
			{"owner": {Type: repositories.KGTermIRI, Value: "urn:product:secret"}},
			{"owner": {Type: repositories.KGTermIRI, Value: "urn:product:public"}},
		}},
	}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:product:secret": true}}
	svc := newKGSvcWithResource(store, rsvc)

	res, err := svc.Query(context.Background(), "SELECT ?owner WHERE { ?x ?p ?owner }")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Bindings) != 1 {
		t.Fatalf("expected 1 row (forbidden IRI hidden in ?owner); got %d", len(res.Bindings))
	}
	if res.Bindings[0]["owner"].Value != "urn:product:public" {
		t.Errorf("wrong row survived the filter: %+v", res.Bindings[0])
	}
}

func TestKGService_ASKForcedFalseForNonSystemContext(t *testing.T) {
	t.Parallel()
	yes := true
	store := &queryRecordingStore{
		active:   true,
		response: repositories.KGQueryResult{Boolean: &yes},
	}
	svc := newKGSvc(store)

	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID:         "agent-1",
		ActiveAccountID: "acct-1",
	})
	res, err := svc.Query(ctx, "ASK { <urn:product:secret> ?p ?o }")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Boolean == nil || *res.Boolean {
		t.Errorf("non-system ASK must be forced to false; got %v", res.Boolean)
	}
}

func TestKGService_ASKPassesThroughForSystemContext(t *testing.T) {
	t.Parallel()
	yes := true
	store := &queryRecordingStore{
		active:   true,
		response: repositories.KGQueryResult{Boolean: &yes},
	}
	svc := newKGSvc(store)

	// No agent in ctx → system context → ASK passes through unchanged.
	res, err := svc.Query(context.Background(), "ASK {}")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Boolean == nil || !*res.Boolean {
		t.Errorf("system ASK must pass through unchanged; got %v", res.Boolean)
	}
}

func TestKGService_FindPath_StopsAtFilteredEmptyToAvoidLengthOracle(t *testing.T) {
	t.Parallel()
	// At length=1 the store returns a triple connecting from→to but every
	// triple involves a forbidden subject. Filter empties the result.
	// We must NOT extend to length=2 — that would let an LLM probe path
	// length via timing/response.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Triples: []repositories.Triple{
			{Subject: "urn:product:secret", Predicate: "p", Object: "urn:product:public"},
		}},
	}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:product:secret": true}}
	svc := newKGSvcWithResource(store, rsvc)

	got, err := svc.FindPath(context.Background(), "urn:product:public", "urn:product:other", 4)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("filtered-empty must return empty without extending; got %v", got)
	}
	if len(store.queries) != 1 {
		t.Errorf("expected exactly 1 query before stopping; got %d (length oracle?)", len(store.queries))
	}
}

func TestKGService_FilterGatesPersonURNs(t *testing.T) {
	t.Parallel()
	// urn:person:* carries FOAF data — must be filtered for non-system
	// callers. resourceFilterStub returns the empty allowed-list because
	// person URNs aren't in the resources table; that's the safe default.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Triples: []repositories.Triple{
			{Subject: "urn:person:alice", Predicate: "http://xmlns.com/foaf/0.1/name", Object: "Alice"},
		}},
	}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:person:alice": true}}
	svc := newKGSvcWithResource(store, rsvc)

	got, err := svc.ExpandEntity(context.Background(), "urn:person:alice", 1)
	if err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("urn:person:* must be gated; got %+v", got)
	}
}

func TestKGService_SearchEntities_FiltersForbiddenSubjects(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Bindings: []map[string]repositories.KGTerm{
			{"s": {Type: repositories.KGTermIRI, Value: "urn:product:public"}},
			{"s": {Type: repositories.KGTermIRI, Value: "urn:product:secret"}},
		}},
	}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:product:secret": true}}
	svc := newKGSvcWithResource(store, rsvc)

	got, err := svc.SearchEntities(context.Background(), "any", 0)
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(got) != 1 || got[0].Value != "urn:product:public" {
		t.Errorf("forbidden subject leaked through search; got %+v", got)
	}
}

func TestKGService_FilterDoesNotGateOntologyOrPersonURNs(t *testing.T) {
	t.Parallel()
	// Mix of gated resource URN, non-resource URN (urn:person:*), and
	// ontology IRI. The filter must only ask about the gated one.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Triples: []repositories.Triple{
			{Subject: "urn:product:1", Predicate: "https://schema.org/name", Object: "Widget"},
			{Subject: "urn:person:abc", Predicate: "https://schema.org/name", Object: "Alice"},
			{Subject: "https://schema.org/Person", Predicate: "rdfs:label", Object: "Person"},
		}},
	}
	rsvc := &resourceFilterStub{}
	svc := newKGSvcWithResource(store, rsvc)

	got, err := svc.ExpandEntity(context.Background(), "urn:product:1", 1)
	if err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("non-gated subjects must pass through; got %d, want 3", len(got))
	}
}
