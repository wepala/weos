package gorm

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
	weosmodels "github.com/wepala/weos/v3/infrastructure/models"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"gorm.io/gorm"
)

func newEventLogTestRepo(t *testing.T) (*EventLogRepository, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&infrastructure.GormEventModel{}); err != nil {
		t.Fatalf("failed to migrate events table: %v", err)
	}
	return &EventLogRepository{db: db}, db
}

func seedLogEvent(
	t *testing.T, db *gorm.DB, id, aggregateID, eventType string, at time.Time, position int64,
) {
	t.Helper()
	err := db.Create(&infrastructure.GormEventModel{
		ID:          id,
		AggregateID: aggregateID,
		EventType:   eventType,
		// Position doubles as the per-aggregate sequence: unique per row, so
		// multi-event aggregates never trip idx_aggregate_sequence.
		SequenceNo: int(position),
		Position:   position,
		Payload:    infrastructure.JSONB{"TypeSlug": "task"},
		CreatedAt:  at,
	}).Error
	if err != nil {
		t.Fatalf("failed to seed event %s: %v", id, err)
	}
}

// TestEventLogQuery_PaginatesEqualTimestamps drives the cursor's equality
// tie-break: five events share one created_at (with sub-second precision), so
// every page boundary exercises the (created_at = ? AND id > ?) arm. The walk
// must visit each event exactly once, in id order.
func TestEventLogQuery_PaginatesEqualTimestamps(t *testing.T) {
	t.Parallel()
	repo, db := newEventLogTestRepo(t)
	at := time.Date(2026, 6, 20, 9, 0, 0, 123456789, time.UTC)
	want := []string{"ev1", "ev2", "ev3", "ev4", "ev5"}
	for i, id := range want {
		seedLogEvent(t, db, id, fmt.Sprintf("urn:task:a%d", i+1), "Resource.Created", at, int64(i+1))
	}

	var got []string
	filter := repositories.EventLogFilter{EventType: "Resource.Created", Limit: 2}
	for {
		page, err := repo.Query(context.Background(), filter)
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		for _, e := range page.Data {
			got = append(got, e.ID)
		}
		if !page.HasMore {
			break
		}
		if page.Cursor == "" {
			t.Fatal("HasMore without a cursor")
		}
		filter.Cursor = page.Cursor
	}

	if len(got) != len(want) {
		t.Fatalf("walked %d events %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("walk order = %v, want %v", got, want)
		}
	}
}

func TestEventLogQuery_RejectsMalformedCursors(t *testing.T) {
	t.Parallel()
	repo, _ := newEventLogTestRepo(t)
	cases := []struct {
		name   string
		cursor string
	}{
		{name: "not base64", cursor: "not-a-cursor!!!"},
		{name: "base64 but not json", cursor: base64.RawURLEncoding.EncodeToString([]byte("junk"))},
		{name: "json but not a timestamp value",
			cursor: base64.RawURLEncoding.EncodeToString([]byte(`{"v":"urn:task:x","id":"x"}`))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.Query(context.Background(), repositories.EventLogFilter{
				Limit: 20, Cursor: tc.cursor,
			})
			if err == nil {
				t.Fatal("malformed cursor accepted, want validation error")
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "validation") || !strings.Contains(msg, "cursor") {
				t.Errorf("error %q should mention validation and the cursor", err)
			}
		})
	}
}

// TestEventLogQuery_EscapesLikeWildcards pins the ESCAPE '\' behavior: an
// underscore in a type slug matches literally, and a bare wildcard slug
// matches nothing rather than everything.
func TestEventLogQuery_EscapesLikeWildcards(t *testing.T) {
	t.Parallel()
	repo, db := newEventLogTestRepo(t)
	at := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	seedLogEvent(t, db, "ev1", "urn:invoice_line:k1", "Resource.Created", at, 1)
	seedLogEvent(t, db, "ev2", "urn:invoiceXline:k2", "Resource.Created", at.Add(time.Minute), 2)

	page, err := repo.Query(context.Background(), repositories.EventLogFilter{
		TypeSlug: "invoice_line", Limit: 20,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != "ev1" {
		t.Errorf("slug with underscore matched %+v, want only ev1", page.Data)
	}

	page, err = repo.Query(context.Background(), repositories.EventLogFilter{TypeSlug: "%", Limit: 20})
	if err != nil {
		t.Fatalf("Query with wildcard slug: %v", err)
	}
	if len(page.Data) != 0 {
		t.Errorf("wildcard slug matched %d events, want none", len(page.Data))
	}
}

// TestEventLogQuery_AnchorsMatchAggregateOrProjection pins the anchored
// filter's union: an anchor matches events where it is the aggregate (works
// with an empty projection — the synchronous path #410's scenarios rely on)
// OR where the event-reference projection links it, and multiple anchors OR
// together. Composition with the event-type filter narrows as usual.
func TestEventLogQuery_AnchorsMatchAggregateOrProjection(t *testing.T) {
	t.Parallel()
	repo, db := newEventLogTestRepo(t)
	if err := db.AutoMigrate(&weosmodels.EventReference{}); err != nil {
		t.Fatalf("migrate event_references: %v", err)
	}
	at := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	project := "urn:project:2p111111111111111111111111"
	other := "urn:project:2p222222222222222222222222"
	task := "urn:task:2t111111111111111111111111"
	seedLogEvent(t, db, "ev-project", project, "Resource.Created", at, 1)
	seedLogEvent(t, db, "ev-task", task, "Resource.Created", at.Add(time.Minute), 2)
	seedLogEvent(t, db, "ev-other", other, "Resource.Created", at.Add(2*time.Minute), 3)
	seedLogEvent(t, db, "ev-task-upd", task, "Resource.Updated", at.Add(3*time.Minute), 4)
	refRepo := &EventReferenceRepository{db: db}
	ctx := context.Background()
	// The task events reference the project; the projection has no row for
	// ev-project itself, so anchoring on the project exercises both branches.
	if err := refRepo.SaveForEvent(ctx, "ev-task", []string{task, project}); err != nil {
		t.Fatalf("seed refs: %v", err)
	}
	if err := refRepo.SaveForEvent(ctx, "ev-task-upd", []string{task, project}); err != nil {
		t.Fatalf("seed refs: %v", err)
	}

	ids := func(filter repositories.EventLogFilter) []string {
		t.Helper()
		page, err := repo.Query(ctx, filter)
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		got := make([]string, 0, len(page.Data))
		for _, e := range page.Data {
			got = append(got, e.ID)
		}
		return got
	}

	if got := ids(repositories.EventLogFilter{Anchors: []string{project}, Limit: 20}); !reflect.DeepEqual(
		got, []string{"ev-project", "ev-task", "ev-task-upd"}) {
		t.Errorf("project anchor = %v, want aggregate + projection matches", got)
	}
	if got := ids(repositories.EventLogFilter{Anchors: []string{other}, Limit: 20}); !reflect.DeepEqual(
		got, []string{"ev-other"}) {
		t.Errorf("aggregate-only anchor (empty projection) = %v, want [ev-other]", got)
	}
	if got := ids(repositories.EventLogFilter{Anchors: []string{project, other}, Limit: 20}); len(got) != 4 {
		t.Errorf("multi-anchor union = %v, want all four events", got)
	}
	if got := ids(repositories.EventLogFilter{
		Anchors: []string{project}, EventType: "Resource.Updated", Limit: 20,
	}); !reflect.DeepEqual(got, []string{"ev-task-upd"}) {
		t.Errorf("anchor + event-type = %v, want [ev-task-upd]", got)
	}
	// Anchor + resource-type: the anchored project's own event drops out of
	// the task-shaped LIKE while its referenced task events remain.
	if got := ids(repositories.EventLogFilter{
		Anchors: []string{project}, TypeSlug: "task", Limit: 20,
	}); !reflect.DeepEqual(got, []string{"ev-task", "ev-task-upd"}) {
		t.Errorf("anchor + resource-type = %v, want the task events only", got)
	}
	// An explicit empty (non-nil) anchors slice means unanchored, never
	// match-nothing.
	if got := ids(repositories.EventLogFilter{Anchors: []string{}, Limit: 20}); len(got) != 4 {
		t.Errorf("empty anchors slice = %v, want all four events (unanchored)", got)
	}
	// An anchor matching an event through BOTH branches (its aggregate and a
	// projection row) yields the event once — guards a future JOIN refactor.
	if got := ids(repositories.EventLogFilter{Anchors: []string{task}, Limit: 20}); !reflect.DeepEqual(
		got, []string{"ev-task", "ev-task-upd"}) {
		t.Errorf("both-branch anchor = %v, want each event exactly once", got)
	}
}

// TestEventLogQuery_WindowBounds pins from-inclusive / until-exclusive so
// day-by-day paging (until = next from) never double-counts.
func TestEventLogQuery_WindowBounds(t *testing.T) {
	t.Parallel()
	repo, db := newEventLogTestRepo(t)
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	seedLogEvent(t, db, "ev-at-from", "urn:task:b1", "Resource.Created", from, 1)
	seedLogEvent(t, db, "ev-at-until", "urn:task:b2", "Resource.Created", until, 2)

	page, err := repo.Query(context.Background(), repositories.EventLogFilter{
		From: from, Until: until, Limit: 20,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != "ev-at-from" {
		t.Errorf("window returned %+v, want only the event at the from bound", page.Data)
	}
}
