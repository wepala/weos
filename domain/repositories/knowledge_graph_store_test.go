package repositories

import (
	"context"
	"strings"
	"testing"
)

// recordingStore records the updates and subject removals it is asked for,
// in order, and nothing else.
type recordingStore struct {
	KnowledgeGraphStore
	calls []string
}

func (s *recordingStore) Active() bool { return true }

func (s *recordingStore) Update(_ context.Context, sparql string) error {
	s.calls = append(s.calls, "update: "+sparql)
	return nil
}

func (s *recordingStore) RemoveSubject(_ context.Context, subject string) error {
	s.calls = append(s.calls, "remove: "+subject)
	return nil
}

// wm-fo2f9: a single-tenant drop takes the nested blank nodes deepest first,
// then the triples pointing at the resource, then the resource's own.
func TestSingleStores_DropAccountTakesNestedNodesObjectTriplesThenTheSubject(t *testing.T) {
	store := &recordingStore{}
	stores := NewSingleKnowledgeGraphStores(store)
	if err := stores.DropAccount(context.Background(), "acct-harbor", []string{"urn:recipe:1"}); err != nil {
		t.Fatalf("DropAccount: %v", err)
	}
	if len(store.calls) != nestedNodeDepth+2 {
		t.Fatalf("%d calls, want %d nested deletes, one object delete and one subject removal:\n%s",
			len(store.calls), nestedNodeDepth, strings.Join(store.calls, "\n"))
	}
	deepest := store.calls[0]
	if !strings.HasPrefix(deepest, "update: DELETE { ?b8 ?p ?o } WHERE { <urn:recipe:1> ?q1 ?b1 . FILTER(isBlank(?b1)) ?b1 ?q2 ?b2 . FILTER(isBlank(?b2))") ||
		!strings.HasSuffix(deepest, "?b8 ?p ?o }") {
		t.Errorf("the first update is not the deepest nested delete: %s", deepest)
	}
	shallowest := store.calls[nestedNodeDepth-1]
	if shallowest != "update: DELETE { ?b1 ?p ?o } WHERE { <urn:recipe:1> ?q1 ?b1 . FILTER(isBlank(?b1)) ?b1 ?p ?o }" {
		t.Errorf("the last nested delete is not the one-hop delete: %s", shallowest)
	}
	if store.calls[nestedNodeDepth] != "update: DELETE WHERE { ?s ?p <urn:recipe:1> }" {
		t.Errorf("the object delete is wrong or out of place: %s", store.calls[nestedNodeDepth])
	}
	if store.calls[nestedNodeDepth+1] != "remove: urn:recipe:1" {
		t.Errorf("the subject removal is wrong or out of place: %s", store.calls[nestedNodeDepth+1])
	}
}

func TestSingleStores_DropAccountRefusesASubjectThatIsNotAPlainIRI(t *testing.T) {
	store := &recordingStore{}
	stores := NewSingleKnowledgeGraphStores(store)
	err := stores.DropAccount(context.Background(), "acct-harbor", []string{"urn:recipe:1> } DELETE WHERE { ?s ?p ?o"})
	if err == nil {
		t.Fatal("a subject carrying SPARQL syntax was written into an update")
	}
	if len(store.calls) != 0 {
		t.Errorf("the store was asked %v for an unsafe subject", store.calls)
	}
}

func TestSingleStores_DropAccountIsANoOpForAnInactiveStore(t *testing.T) {
	if err := NewSingleKnowledgeGraphStores(nil).DropAccount(context.Background(), "acct-harbor", []string{"urn:recipe:1"}); err != nil {
		t.Fatalf("DropAccount with no store: %v", err)
	}
}
