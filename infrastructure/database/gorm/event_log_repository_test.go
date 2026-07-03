package gorm

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"

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
		SequenceNo:  1,
		Position:    position,
		Payload:     infrastructure.JSONB{"TypeSlug": "task"},
		CreatedAt:   at,
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
