package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// fakeStores is a KnowledgeGraphStores test double: it lets a test choose the
// mode (PerAccount) and observe/inject the ForAccount resolution without a real
// backend. Shared by the projector and service unit tests.
type fakeStores struct {
	perAccount  bool
	active      bool
	store       repositories.KnowledgeGraphStore
	forErr      error
	lastAccount string
}

func (f *fakeStores) Active() bool     { return f.active }
func (f *fakeStores) PerAccount() bool { return f.perAccount }
func (f *fakeStores) ForAccount(_ context.Context, id string) (repositories.KnowledgeGraphStore, error) {
	f.lastAccount = id
	if f.forErr != nil {
		return nil, f.forErr
	}
	if f.store != nil {
		return f.store, nil
	}
	return &fakeKGStore{active: true}, nil
}
func (f *fakeStores) Truncate(context.Context) error { return nil }
func (f *fakeStores) Close() error                   { return nil }

func envWithPayload(eventType, aggregateID string, payload any) domain.EventEnvelope[any] {
	return domain.EventEnvelope[any]{AggregateID: aggregateID, EventType: eventType, Payload: payload}
}

func createdEnvelope(accountID, createdBy string) domain.EventEnvelope[any] {
	return domain.EventEnvelope[any]{
		EventType: "Resource.Created",
		Payload:   map[string]any{"AccountID": accountID, "CreatedBy": createdBy},
	}
}

func TestAccountFromPayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload any
		want    string
	}{
		{"map with account", map[string]any{"AccountID": "acct-a"}, "acct-a"},
		{"map without account", map[string]any{}, ""},
		{"ResourceCreated", entities.ResourceCreated{AccountID: "acct-a"}, "acct-a"},
		{"ResourcePublished", entities.ResourcePublished{AccountID: "acct-a"}, "acct-a"},
		{"ResourceUpdated", entities.ResourceUpdated{AccountID: "acct-a"}, "acct-a"},
		{"ResourceDeleted", entities.ResourceDeleted{AccountID: "acct-a"}, "acct-a"},
		{"TripleCreated", entities.TripleCreated{AccountID: "acct-a"}, "acct-a"},
		{"TripleDeleted", entities.TripleDeleted{AccountID: "acct-a"}, "acct-a"},
		{"unknown payload", 42, ""},
	}
	for _, c := range cases {
		if got := accountFromPayload(c.payload); got != c.want {
			t.Errorf("accountFromPayload(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCreatedByFromPayload(t *testing.T) {
	t.Parallel()
	if got := createdByFromPayload(map[string]any{"CreatedBy": "agent-a"}); got != "agent-a" {
		t.Errorf("map: got %q", got)
	}
	if got := createdByFromPayload(entities.ResourceCreated{CreatedBy: "agent-a"}); got != "agent-a" {
		t.Errorf("ResourceCreated: got %q", got)
	}
	// Only Resource.Created carries a creator; other payloads yield "".
	if got := createdByFromPayload(entities.ResourcePublished{AccountID: "acct-a"}); got != "" {
		t.Errorf("ResourcePublished should have no creator, got %q", got)
	}
}

// TestEventAccountID_SerializesAsAccountIDKey guards accountFromPayload's map
// path against JSON-key drift. The background subscriber sees events deserialized
// to map[string]any, so the routing-critical key MUST stay "AccountID". If a
// json tag is ever added that renames it, this fails loudly instead of silently
// routing every event to the local graph (a total isolation collapse).
func TestEventAccountID_SerializesAsAccountIDKey(t *testing.T) {
	t.Parallel()
	events := map[string]any{
		"ResourceCreated":   entities.ResourceCreated{AccountID: "acct-a"},
		"ResourcePublished": entities.ResourcePublished{AccountID: "acct-a"},
		"ResourceUpdated":   entities.ResourceUpdated{AccountID: "acct-a"},
		"ResourceDeleted":   entities.ResourceDeleted{AccountID: "acct-a"},
		"TripleCreated":     entities.TripleCreated{AccountID: "acct-a"},
		"TripleDeleted":     entities.TripleDeleted{AccountID: "acct-a"},
	}
	for name, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if got := accountFromPayload(m); got != "acct-a" {
			t.Errorf("%s: round-tripped account = %q, want acct-a (JSON key drift?): %s", name, got, b)
		}
	}
}

func TestProjector_accountForEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	proj := func(es *fakeEventStore, perAccount bool) *oxigraphProjector {
		return newOxigraphProjector(es, &fakeStores{perAccount: perAccount, active: true}, &kgTypeRepo{}, noopLogger{})
	}

	t.Run("single-tenant ignores account", func(t *testing.T) {
		got, err := proj(&fakeEventStore{}, false).accountForEvent(ctx,
			envWithPayload("Resource.Published", "urn:project:1", map[string]any{"AccountID": "acct-a"}))
		if err != nil || got != "" {
			t.Fatalf("got (%q,%v), want (\"\",nil)", got, err)
		}
	})

	t.Run("account from payload", func(t *testing.T) {
		got, err := proj(&fakeEventStore{}, true).accountForEvent(ctx,
			envWithPayload("Resource.Published", "urn:project:1", map[string]any{"AccountID": "acct-a"}))
		if err != nil || got != "acct-a" {
			t.Fatalf("got (%q,%v), want (acct-a,nil)", got, err)
		}
	})

	t.Run("legacy event recovers account from history", func(t *testing.T) {
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			"urn:project:1": {createdEnvelope("acct-a", "agent-a")},
		}}
		got, err := proj(es, true).accountForEvent(ctx,
			envWithPayload("Triple.Created", "urn:project:1", map[string]any{}))
		if err != nil || got != "acct-a" {
			t.Fatalf("got (%q,%v), want (acct-a,nil)", got, err)
		}
	})

	t.Run("accountless authenticated write is refused", func(t *testing.T) {
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			"urn:project:1": {createdEnvelope("", "agent-a")}, // creator, no account
		}}
		_, err := proj(es, true).accountForEvent(ctx,
			envWithPayload("Triple.Created", "urn:project:1", map[string]any{}))
		if !errors.Is(err, errAccountlessWrite) {
			t.Fatalf("want errAccountlessWrite, got %v", err)
		}
	})

	t.Run("genuinely ownerless write routes to local", func(t *testing.T) {
		es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
			"urn:project:1": {createdEnvelope("", "")}, // no account, no creator
		}}
		got, err := proj(es, true).accountForEvent(ctx,
			envWithPayload("Triple.Created", "urn:project:1", map[string]any{}))
		if err != nil || got != LocalAccountID {
			t.Fatalf("got (%q,%v), want (%q,nil)", got, err, LocalAccountID)
		}
	})

	t.Run("transient history-load failure propagates", func(t *testing.T) {
		es := &fakeEventStore{getEventsErr: errors.New("db down")}
		_, err := proj(es, true).accountForEvent(ctx,
			envWithPayload("Triple.Created", "urn:project:1", map[string]any{}))
		if err == nil || errors.Is(err, errAccountlessWrite) {
			t.Fatalf("want a transient error to propagate, got %v", err)
		}
	})
}

// TestProjector_handleSkipsAccountlessWrite proves the routing decision does not
// leak an accountless authenticated write into the local graph: handle returns
// nil (skip, not park) and NOTHING is written to the store.
func TestProjector_handleSkipsAccountlessWrite(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{byAggregate: map[string][]domain.EventEnvelope[any]{
		"urn:project:1": {createdEnvelope("", "agent-a")},
	}}
	proj := newOxigraphProjector(es, &fakeStores{perAccount: true, active: true, store: store}, &kgTypeRepo{}, noopLogger{})
	if err := proj.handle(context.Background(),
		tripleMapEnvelope("Triple.Created", "urn:project:1", "https://schema.org/name", "x")); err != nil {
		t.Fatalf("accountless write should be skipped without error, got %v", err)
	}
	if len(store.addCalls) != 0 {
		t.Fatalf("accountless write must not be projected into any store, got %d writes", len(store.addCalls))
	}
}

// TestProjector_handleRetriesOnHistoryLoadError proves a transient account-
// resolution failure surfaces as an error so the subscriber retries/parks the
// event rather than misrouting it to the local graph.
func TestProjector_handleRetriesOnHistoryLoadError(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{getEventsErr: errors.New("db down")}
	proj := newOxigraphProjector(es, &fakeStores{perAccount: true, active: true, store: store}, &kgTypeRepo{}, noopLogger{})
	if err := proj.handle(context.Background(),
		tripleMapEnvelope("Triple.Created", "urn:project:1", "https://schema.org/name", "x")); err == nil {
		t.Fatal("a history-load failure must surface so the subscriber retries")
	}
	if len(store.addCalls) != 0 {
		t.Fatalf("a failed resolution must not write anything, got %d writes", len(store.addCalls))
	}
}
