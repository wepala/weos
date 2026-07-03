package gorm

import (
	"context"
	"reflect"
	"testing"

	weosmodels "github.com/wepala/weos/v3/infrastructure/models"
)

func newEventReferenceTestRepo(t *testing.T) *EventReferenceRepository {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&weosmodels.EventReference{}); err != nil {
		t.Fatalf("failed to migrate event_references: %v", err)
	}
	return &EventReferenceRepository{db: db}
}

// TestEventReferenceSaveIsIdempotent replays the same save; the projected
// rows must not duplicate (ON CONFLICT DO NOTHING on the composite key).
func TestEventReferenceSaveIsIdempotent(t *testing.T) {
	t.Parallel()
	repo := newEventReferenceTestRepo(t)
	ctx := context.Background()
	urns := []string{"urn:task:2t111111111111111111111111", "urn:project:2p111111111111111111111111"}

	for range 2 {
		if err := repo.SaveForEvent(ctx, "ev1", urns); err != nil {
			t.Fatalf("SaveForEvent: %v", err)
		}
	}

	refs, err := repo.ForEvents(ctx, []string{"ev1"})
	if err != nil {
		t.Fatalf("ForEvents: %v", err)
	}
	want := []string{"urn:project:2p111111111111111111111111", "urn:task:2t111111111111111111111111"}
	if !reflect.DeepEqual(refs["ev1"], want) {
		t.Errorf("refs = %v, want %v (sorted, no duplicates)", refs["ev1"], want)
	}
}

func TestEventReferenceForEventsBatches(t *testing.T) {
	t.Parallel()
	repo := newEventReferenceTestRepo(t)
	ctx := context.Background()
	if err := repo.SaveForEvent(ctx, "ev1", []string{"urn:task:2t111111111111111111111111"}); err != nil {
		t.Fatalf("SaveForEvent ev1: %v", err)
	}
	if err := repo.SaveForEvent(ctx, "ev2", []string{"urn:project:2p111111111111111111111111"}); err != nil {
		t.Fatalf("SaveForEvent ev2: %v", err)
	}

	refs, err := repo.ForEvents(ctx, []string{"ev1", "ev2", "ev-unknown"})
	if err != nil {
		t.Fatalf("ForEvents: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("got refs for %d events, want 2: %v", len(refs), refs)
	}
	if _, ok := refs["ev-unknown"]; ok {
		t.Error("unknown event should have no entry, not an empty one")
	}

	empty, err := repo.ForEvents(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("ForEvents(nil) = %v, %v; want empty, nil", empty, err)
	}
}

func TestEventReferenceClearTruncates(t *testing.T) {
	t.Parallel()
	repo := newEventReferenceTestRepo(t)
	ctx := context.Background()
	if err := repo.SaveForEvent(ctx, "ev1", []string{"urn:task:2t111111111111111111111111"}); err != nil {
		t.Fatalf("SaveForEvent: %v", err)
	}
	if err := repo.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	refs, err := repo.ForEvents(ctx, []string{"ev1"})
	if err != nil {
		t.Fatalf("ForEvents: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("projection not empty after Clear: %v", refs)
	}
}
