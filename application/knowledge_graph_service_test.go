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
func (q *queryRecordingStore) Update(_ context.Context, _ string) error { return nil }
func (q *queryRecordingStore) LoadOntology(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (q *queryRecordingStore) Clear(_ context.Context) error           { return nil }
func (q *queryRecordingStore) IsEmpty(_ context.Context) (bool, error) { return true, nil }

// resourceFilterStub is a minimal ResourceService stub exposing only the
// FilterAccessibleResourceIDs method that the KG permission filter calls.
// `forbidden` lists the URNs that should be excluded from the allowed set.
type resourceFilterStub struct {
	forbidden map[string]bool
	calls     int
	requested [][]string
}

func (r *resourceFilterStub) FilterAccessibleResourceIDs(_ context.Context, ids []string) ([]string, error) {
	r.calls++
	captured := append([]string(nil), ids...)
	r.requested = append(r.requested, captured)
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
	return &knowledgeGraphService{
		stores:   repositories.NewSingleKnowledgeGraphStores(store),
		resource: rsvc,
		logger:   noopLogger{},
	}
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
	if _, err := svc.SearchEntities(ctx, "x", "", 0); !errors.Is(err, ErrKGUnavailable) {
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

	if _, err := svc.SearchEntities(context.Background(), `she said "hi"`, "", 0); err != nil {
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

	if _, err := svc.SearchEntities(context.Background(), "x", "", 9999); err != nil {
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

func TestKGService_SearchEntities_ClassFilterAddsSubClassPath(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	if _, err := svc.SearchEntities(context.Background(), "alice", "https://schema.org/Person", 0); err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	q := store.queries[0]
	if !strings.Contains(q, "<https://schema.org/Person>") {
		t.Errorf("class IRI missing from query: %q", q)
	}
	if !strings.Contains(q, "rdf-schema#subClassOf>*") {
		t.Errorf("class filter must use subClassOf property path so subclasses match: %q", q)
	}
}

func TestKGService_SearchEntities_RejectsMaliciousClassIRI(t *testing.T) {
	t.Parallel()
	svc := newKGSvc(&queryRecordingStore{active: true})

	if _, err := svc.SearchEntities(context.Background(), "x", "urn:c<inj>", 0); err == nil {
		t.Fatal("expected validation error on injected class IRI")
	}
}

func TestKGService_ListClasses_ReturnsClassIRIs(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Bindings: []map[string]repositories.KGTerm{
			{"s": {Type: repositories.KGTermIRI, Value: "urn:type:product"}},
			{"s": {Type: repositories.KGTermIRI, Value: "https://schema.org/Person"}},
		}},
	}
	svc := newKGSvc(store)

	classes, err := svc.ListClasses(context.Background())
	if err != nil {
		t.Fatalf("ListClasses: %v", err)
	}
	if len(classes) != 2 {
		t.Fatalf("expected 2 classes; got %d", len(classes))
	}
	if !strings.Contains(store.queries[0], "rdf-schema#Class") || !strings.Contains(store.queries[0], "owl#Class") {
		t.Errorf("query should match both rdfs:Class and owl:Class: %q", store.queries[0])
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

func TestKGService_DescribeClass_DropsPredicatesOnlyOnForbiddenInstances(t *testing.T) {
	t.Parallel()
	// Predicate query projects (?s, ?p). schema:name occurs on an accessible
	// instance; schema:ssn occurs only on a forbidden one. After permission
	// filtering, ssn must not be exposed.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Bindings: []map[string]repositories.KGTerm{
			{"s": {Type: repositories.KGTermIRI, Value: "urn:person:visible"},
				"p": {Type: repositories.KGTermIRI, Value: "https://schema.org/name"}},
			{"s": {Type: repositories.KGTermIRI, Value: "urn:person:hidden"},
				"p": {Type: repositories.KGTermIRI, Value: "https://schema.org/ssn"}},
		}},
	}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:person:hidden": true}}
	svc := newKGSvcWithResource(store, rsvc)

	desc, err := svc.DescribeClass(scopedCtx(), "https://schema.org/Person", 0)
	if err != nil {
		t.Fatalf("DescribeClass: %v", err)
	}
	if len(desc.Predicates) != 1 || desc.Predicates[0] != "https://schema.org/name" {
		t.Errorf("predicates = %v, want [https://schema.org/name] (ssn must be filtered out)", desc.Predicates)
	}
	// The predicate query must project ?s so the filter has a subject to gate.
	if !strings.Contains(store.queries[0], "?s ?p") {
		t.Errorf("predicate query must project ?s alongside ?p: %q", store.queries[0])
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
	if _, err := svc.SearchEntities(ctx, "  ", "", 0); err == nil {
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
	svc := &knowledgeGraphService{stores: nil, logger: noopLogger{}}
	if svc.Active() {
		t.Error("nil store should report Active()=false")
	}
	if _, err := svc.Query(context.Background(), "ASK {}"); err != ErrKGUnavailable {
		t.Errorf("nil store should return ErrKGUnavailable, got %v", err)
	}
}

// TestResolveAccountID_Branches pins the per-account read-side routing policy:
// identity→account, local-transport→local graph, remote-unresolved→fail closed
// (including the identity-present-but-empty-account case), and single-tenant→"".
func TestResolveAccountID_Branches(t *testing.T) {
	t.Parallel()
	perAccount := &knowledgeGraphService{stores: &fakeStores{perAccount: true, active: true}, logger: noopLogger{}}
	single := &knowledgeGraphService{stores: &fakeStores{perAccount: false, active: true}, logger: noopLogger{}}
	base := context.Background()
	withAccount := auth.ContextWithAgent(base, &auth.Identity{
		AgentID: "agent", AccountIDs: []string{"acct"}, ActiveAccountID: "acct",
	})
	withoutActiveAccount := auth.ContextWithAgent(base, &auth.Identity{AgentID: "agent"})
	local := WithLocalTransport(base)

	if got, err := perAccount.resolveAccountID(withAccount); err != nil || got != "acct" {
		t.Errorf("identity+account: got (%q,%v), want (acct,nil)", got, err)
	}
	if got, err := perAccount.resolveAccountID(local); err != nil || got != LocalAccountID {
		t.Errorf("local no-account: got (%q,%v), want (%q,nil)", got, err, LocalAccountID)
	}
	if _, err := perAccount.resolveAccountID(base); !errors.Is(err, ErrKGUnavailable) {
		t.Errorf("remote no-account: want ErrKGUnavailable, got %v", err)
	}
	if _, err := perAccount.resolveAccountID(withoutActiveAccount); !errors.Is(err, ErrKGUnavailable) {
		t.Errorf("identity with empty active account (remote): want ErrKGUnavailable, got %v", err)
	}
	if got, err := single.resolveAccountID(withAccount); err != nil || got != "" {
		t.Errorf("single-tenant: got (%q,%v), want (\"\",nil)", got, err)
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

func scopedCtx() context.Context {
	return auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID:         "agent-1",
		ActiveAccountID: "acct-1",
	})
}

func TestKGService_ASKRejectedForScopedCaller(t *testing.T) {
	t.Parallel()
	// Both a gated-naming ASK and an unbound ontology-predicate ASK must be
	// rejected for scoped callers — a bare boolean has no IRI for the
	// post-filter to remove, so `ASK { ?s <https://schema.org/ssn> ?o }` would
	// otherwise leak whether any (possibly forbidden) resource exists.
	for _, q := range []string{
		"ASK { <urn:product:secret> ?p ?o }",
		"ASK { ?s <https://schema.org/ssn> ?o }",
		"ASK { schema:Person rdfs:subClassOf schema:Thing }",
		// Prologue must not be a bypass: a PREFIX/BASE-prefixed ASK still
		// has to be detected and rejected — including the single-line form
		// where the prologue and ASK share a line.
		"PREFIX s: <https://schema.org/>\nASK { ?x s:ssn ?o }",
		"PREFIX s: <https://schema.org/> ASK { ?x s:ssn ?o }",
		"# comment\nBASE <https://schema.org/>\nASK { ?x ?p ?o }",
	} {
		yes := true
		store := &queryRecordingStore{active: true, response: repositories.KGQueryResult{Boolean: &yes}}
		svc := newKGSvc(store)
		if _, err := svc.Query(scopedCtx(), q); !errors.Is(err, ErrKGQueryNotPermitted) {
			t.Errorf("Query(%q): got %v, want ErrKGQueryNotPermitted", q, err)
		}
		if len(store.queries) != 0 {
			t.Errorf("Query(%q): store must not be hit for a rejected ASK; got %d queries", q, len(store.queries))
		}
	}
}

func TestKGService_AggregateSelectRejectedForScopedCaller(t *testing.T) {
	t.Parallel()
	for _, q := range []string{
		"SELECT (COUNT(?s) AS ?count) WHERE { ?s ?p ?o }",
		"SELECT ?p (SUM(?n) AS ?total) WHERE { ?s ?p ?n } GROUP BY ?p",
		// Prologue must not be a bypass for aggregate detection either.
		"PREFIX s: <https://schema.org/>\nSELECT (COUNT(?s) AS ?c) WHERE { ?s ?p ?o }",
	} {
		store := &queryRecordingStore{active: true}
		svc := newKGSvc(store)
		if _, err := svc.Query(scopedCtx(), q); !errors.Is(err, ErrKGQueryNotPermitted) {
			t.Errorf("Query(%q): got %v, want ErrKGQueryNotPermitted", q, err)
		}
		if len(store.queries) != 0 {
			t.Errorf("Query(%q): store must not be hit for a rejected aggregate; got %d queries", q, len(store.queries))
		}
	}
}

func TestKGService_ScopedSelectRequiresGatedIRIPerRow(t *testing.T) {
	t.Parallel()
	// Rows that surface no gated resource IRI (literal-only or predicate-only
	// projections — the prefix-named-subject and bare-predicate leaks) must be
	// dropped for scoped callers; a row that does bind a gated IRI is kept.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Bindings: []map[string]repositories.KGTerm{
			{"email": {Type: repositories.KGTermLiteral, Value: "a@b.com"}},          // literal only
			{"p": {Type: repositories.KGTermIRI, Value: "https://schema.org/email"}}, // predicate only
			{"s": {Type: repositories.KGTermIRI, Value: "urn:person:visible"}, // gated IRI present
				"p": {Type: repositories.KGTermIRI, Value: "https://schema.org/email"}},
		}},
	}
	svc := newKGSvc(store)

	res, err := svc.Query(scopedCtx(), "SELECT ?email WHERE { ?s <https://schema.org/email> ?email }")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Bindings) != 1 {
		t.Fatalf("expected 1 surviving row (the one with a gated IRI); got %d: %+v", len(res.Bindings), res.Bindings)
	}
	if got := res.Bindings[0]["s"].Value; got != "urn:person:visible" {
		t.Errorf("surviving row should be the gated-IRI one; got %q", got)
	}
}

func TestKGService_SystemSelectKeepsLiteralOnlyRows(t *testing.T) {
	t.Parallel()
	// System callers (nil identity) are unaffected — literal-only rows pass.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Bindings: []map[string]repositories.KGTerm{
			{"email": {Type: repositories.KGTermLiteral, Value: "a@b.com"}},
		}},
	}
	svc := newKGSvc(store)

	res, err := svc.Query(context.Background(), "SELECT ?email WHERE { ?s <https://schema.org/email> ?email }")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Bindings) != 1 {
		t.Errorf("system caller must keep literal-only rows; got %d", len(res.Bindings))
	}
}

func TestKGService_QueryNamingForbiddenURNRejectedForScopedCaller(t *testing.T) {
	t.Parallel()
	// A non-aggregate SELECT that names a forbidden resource leaks its
	// predicates without ever projecting its IRI, so the guard refuses it.
	store := &queryRecordingStore{active: true}
	rsvc := &resourceFilterStub{forbidden: map[string]bool{"urn:product:secret": true}}
	svc := newKGSvcWithResource(store, rsvc)

	_, err := svc.Query(scopedCtx(), "SELECT ?p WHERE { <urn:product:secret> ?p ?o }")
	if !errors.Is(err, ErrKGQueryNotPermitted) {
		t.Fatalf("got %v, want ErrKGQueryNotPermitted", err)
	}
	if len(store.queries) != 0 {
		t.Errorf("store must not be hit for a rejected query; got %d queries", len(store.queries))
	}
}

func TestKGService_QueryNamingAccessibleURNAllowedForScopedCaller(t *testing.T) {
	t.Parallel()
	// Same shape, but the named resource is accessible → allowed through.
	store := &queryRecordingStore{active: true}
	rsvc := &resourceFilterStub{} // nothing forbidden
	svc := newKGSvcWithResource(store, rsvc)

	if _, err := svc.Query(scopedCtx(), "SELECT ?p WHERE { <urn:product:mine> ?p ?o }"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(store.queries) != 1 {
		t.Errorf("accessible-URN query should reach the store; got %d queries", len(store.queries))
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

	got, err := svc.SearchEntities(context.Background(), "any", "", 0)
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(got) != 1 || got[0].Value != "urn:product:public" {
		t.Errorf("forbidden subject leaked through search; got %+v", got)
	}
}

func TestKGService_FilterPassesNonGatedTermsThrough(t *testing.T) {
	t.Parallel()
	// Mix of a gated resource URN and a pure ontology IRI. Person/org URNs
	// are gated alongside resources (PII), so they're tested separately in
	// TestKGService_FilterGatesPersonURNs. Here we confirm that ontology
	// IRIs are never sent to the permission filter at all — they're not
	// resources and shouldn't generate a check.
	store := &queryRecordingStore{
		active: true,
		response: repositories.KGQueryResult{Triples: []repositories.Triple{
			{Subject: "urn:product:1", Predicate: "https://schema.org/name", Object: "Widget"},
			{Subject: "https://schema.org/Person", Predicate: "rdfs:label", Object: "Person"},
		}},
	}
	rsvc := &resourceFilterStub{}
	svc := newKGSvcWithResource(store, rsvc)

	got, err := svc.ExpandEntity(context.Background(), "urn:product:1", 1)
	if err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("non-gated subjects must pass through; got %d, want 2", len(got))
	}
	for _, ids := range rsvc.requested {
		for _, id := range ids {
			if id == "https://schema.org/Person" {
				t.Errorf("ontology IRI %q must not be sent to the permission filter", id)
			}
		}
	}
}
