package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
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

func newKGSvc(store repositories.KnowledgeGraphStore) KnowledgeGraphService {
	return &knowledgeGraphService{store: store, logger: noopLogger{}}
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
