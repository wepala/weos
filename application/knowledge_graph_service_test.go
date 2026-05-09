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
}

func TestKGService_ExpandEntity_DepthClampedTo3(t *testing.T) {
	t.Parallel()
	store := &queryRecordingStore{active: true}
	svc := newKGSvc(store)

	// depth=99 should clamp; we verify by checking the {0,N} length in the
	// generated SPARQL property path is 2 (=depth-1=3-1).
	if _, err := svc.ExpandEntity(context.Background(), "urn:x", 99); err != nil {
		t.Fatalf("ExpandEntity: %v", err)
	}
	if !strings.Contains(store.queries[0], "{0,2}") {
		t.Errorf("depth not clamped to 3: %q", store.queries[0])
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
