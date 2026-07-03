package application

import (
	"context"
	"reflect"
	"testing"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

func refEnvelope(id, aggregateID, eventType string, payload map[string]any) pericarpdomain.EventEnvelope[any] {
	return pericarpdomain.EventEnvelope[any]{
		ID:          id,
		AggregateID: aggregateID,
		EventType:   eventType,
		Payload:     payload,
	}
}

func TestExtractEventReferences(t *testing.T) {
	cases := []struct {
		name  string
		event pericarpdomain.EventEnvelope[any]
		want  []string
	}{
		{
			name: "no payload references reports just the aggregate",
			event: refEnvelope("ev1", "urn:project:2p111111111111111111111111", "Resource.Created",
				map[string]any{"TypeSlug": "project", "Data": map[string]any{"name": "Client onboarding"}}),
			want: []string{"urn:project:2p111111111111111111111111"},
		},
		{
			name: "flat payload property referencing another resource",
			event: refEnvelope("ev2", "urn:task:2t111111111111111111111111", "Resource.Created",
				map[string]any{"TypeSlug": "task", "Data": map[string]any{
					"name": "Chase overdue invoices", "project": "urn:project:2p111111111111111111111111",
				}}),
			want: []string{
				"urn:project:2p111111111111111111111111",
				"urn:task:2t111111111111111111111111",
			},
		},
		{
			name: "graph edges node with @id maps and arrays",
			event: refEnvelope("ev3", "urn:task:2t111111111111111111111111", "Resource.Updated",
				map[string]any{"Data": map[string]any{
					"@graph": []any{
						map[string]any{"@id": "urn:task:2t111111111111111111111111", "name": "Task"},
						map[string]any{
							"@id": "urn:task:2t111111111111111111111111",
							"https://schema.org/isPartOf": map[string]any{
								"@id": "urn:project:2p111111111111111111111111",
							},
							"https://schema.org/attendee": []any{
								map[string]any{"@id": "urn:person:2q111111111111111111111111"},
							},
						},
					},
				}}),
			want: []string{
				"urn:person:2q111111111111111111111111",
				"urn:project:2p111111111111111111111111",
				"urn:task:2t111111111111111111111111",
			},
		},
		{
			name: "triple event references subject and object",
			event: refEnvelope("ev4", "urn:task:2t111111111111111111111111", "Triple.Created",
				map[string]any{
					"subject":   "urn:task:2t111111111111111111111111",
					"predicate": "https://schema.org/isPartOf",
					"object":    "urn:project:2p111111111111111111111111",
				}),
			want: []string{
				"urn:project:2p111111111111111111111111",
				"urn:task:2t111111111111111111111111",
			},
		},
		{
			name: "non-resource URNs and literals are ignored",
			event: refEnvelope("ev5", "urn:fact:2f111111111111111111111111", "Resource.Created",
				map[string]any{"Data": map[string]any{
					"statement":      "the sky is blue",
					"wasDerivedFrom": []any{"urn:event:2e111111111111111111111111"},
					"seeAlso":        "urn:type:task",
					"theme":          "urn:theme:default",
				}}),
			want: []string{"urn:fact:2f111111111111111111111111"},
		},
		{
			name: "type-config aggregate yields no references",
			event: refEnvelope("ev6", "urn:type:task", "ResourceType.Created",
				map[string]any{"Slug": "task"}),
			want: []string{},
		},
		{
			name: "nil payload still reports the aggregate",
			event: pericarpdomain.EventEnvelope[any]{
				ID: "ev7", AggregateID: "urn:task:2t111111111111111111111111",
				EventType: "Resource.Deleted",
			},
			want: []string{"urn:task:2t111111111111111111111111"},
		},
		{
			name:  "empty aggregate and no payload yields nothing",
			event: pericarpdomain.EventEnvelope[any]{ID: "ev8", EventType: "Resource.Created"},
			want:  []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractEventReferences(tc.event)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("references = %v, want %v", got, tc.want)
			}
		})
	}
}

type fakeEventReferenceRepo struct {
	saves map[string]map[string]int // eventID → URN → save count
}

func newFakeEventReferenceRepo() *fakeEventReferenceRepo {
	return &fakeEventReferenceRepo{saves: map[string]map[string]int{}}
}

func (f *fakeEventReferenceRepo) SaveForEvent(_ context.Context, eventID string, urns []string) error {
	rows, ok := f.saves[eventID]
	if !ok {
		rows = map[string]int{}
		f.saves[eventID] = rows
	}
	for _, urn := range urns {
		rows[urn]++
	}
	return nil
}

func (f *fakeEventReferenceRepo) ForEvents(_ context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, id := range ids {
		for urn := range f.saves[id] {
			out[id] = append(out[id], urn)
		}
	}
	return out, nil
}

func (f *fakeEventReferenceRepo) Clear(context.Context) error {
	f.saves = map[string]map[string]int{}
	return nil
}

// TestEventReferenceHandlerReplayIsIdempotent processes the same event twice;
// the projected reference set must not change (the repository sees identical
// keyed rows, which ON CONFLICT DO NOTHING makes a no-op in production).
func TestEventReferenceHandlerReplayIsIdempotent(t *testing.T) {
	repo := newFakeEventReferenceRepo()
	handler := eventReferenceHandler(repo, noopLogger{})
	event := refEnvelope("ev1", "urn:task:2t111111111111111111111111", "Triple.Created",
		map[string]any{
			"subject": "urn:task:2t111111111111111111111111",
			"object":  "urn:project:2p111111111111111111111111",
		})

	for range 2 {
		if err := handler(context.Background(), event); err != nil {
			t.Fatalf("handler: %v", err)
		}
	}

	if len(repo.saves["ev1"]) != 2 {
		t.Errorf("event ev1 projects %d URNs, want 2", len(repo.saves["ev1"]))
	}
	refs, _ := repo.ForEvents(context.Background(), []string{"ev1"})
	if len(refs["ev1"]) != 2 {
		t.Errorf("replay changed the reference set: %v", refs["ev1"])
	}
}

// TestEventReferenceGroupDeclaresRebuild pins the group's contract: stable
// checkpoint name, a Truncate action that actually clears the projection (so
// --truncate rebuilds work), and no StartAtHead (completeness over history
// requires replay from position 0).
func TestEventReferenceGroupDeclaresRebuild(t *testing.T) {
	repo := newFakeEventReferenceRepo()
	groups := ProvideEventReferenceGroup(EventReferenceGroupParams{
		References: repo,
		Logger:     noopLogger{},
	})
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	g := groups[0]
	if g.Name != "event-references" {
		t.Errorf("group name = %q, want event-references", g.Name)
	}
	if g.StartAtHead {
		t.Error("group must not StartAtHead — the projection needs full history")
	}
	if g.Truncate == nil {
		t.Fatal("group declares no Truncate; checkpoint reset --truncate would be rejected")
	}
	if err := repo.SaveForEvent(context.Background(), "ev1", []string{"urn:task:x"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := g.Truncate(context.Background()); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if refs, _ := repo.ForEvents(context.Background(), []string{"ev1"}); len(refs) != 0 {
		t.Errorf("Truncate left projection rows behind: %v", refs)
	}
}

// errorRecordingLogger captures error logs so tests can assert observability.
type errorRecordingLogger struct {
	noopLogger
	errors []string
}

func (l *errorRecordingLogger) Error(_ context.Context, msg string, _ ...any) {
	l.errors = append(l.errors, msg)
}

// TestEventReferenceHandlerLogsUnexpectedPayloadShape pins the degradation
// contract: a reference-bearing event with a non-map payload still projects
// its aggregate but logs the shape problem; other event types stay quiet.
func TestEventReferenceHandlerLogsUnexpectedPayloadShape(t *testing.T) {
	repo := newFakeEventReferenceRepo()
	logger := &errorRecordingLogger{}
	handler := eventReferenceHandler(repo, logger)

	weird := pericarpdomain.EventEnvelope[any]{
		ID: "ev1", AggregateID: "urn:task:2t111111111111111111111111",
		EventType: "Triple.Created", Payload: "not-a-map",
	}
	if err := handler(context.Background(), weird); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(logger.errors) != 1 {
		t.Errorf("reference-bearing event with weird payload logged %d errors, want 1", len(logger.errors))
	}
	if len(repo.saves["ev1"]) != 1 {
		t.Errorf("aggregate-only fallback saved %d refs, want 1", len(repo.saves["ev1"]))
	}

	quiet := pericarpdomain.EventEnvelope[any]{
		ID: "ev2", AggregateID: "urn:task:2t222222222222222222222222",
		EventType: "User.LoggedIn", Payload: "not-a-map",
	}
	if err := handler(context.Background(), quiet); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(logger.errors) != 1 {
		t.Errorf("non-reference-bearing event logged; errors = %v", logger.errors)
	}
}
