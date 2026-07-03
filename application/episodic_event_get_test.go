package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
)

func TestEventByURNReturnsFullPayload(t *testing.T) {
	stub := &stubEventLog{log: []repositories.EventLogEntry{{
		ID:          "ev1",
		AggregateID: "urn:task:2t111111111111111111111111",
		EventType:   "Resource.Created",
		CreatedAt:   time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"TypeSlug": "task",
			"Data": map[string]any{
				"name":        "May reconciliation",
				"description": "Match receipts to the ledger",
			},
		},
	}}}
	refs := &recordingRefsRepo{refs: map[string][]string{
		"ev1": {"urn:task:2t111111111111111111111111"},
	}}
	recall := &episodicRecall{events: stub, references: refs, now: time.Now}

	fetched, err := recall.EventByURN(context.Background(), "urn:event:ev1")
	if err != nil {
		t.Fatalf("EventByURN: %v", err)
	}
	if fetched.ID != "urn:event:ev1" || fetched.EventType != "Resource.Created" {
		t.Errorf("envelope facts wrong: %+v", fetched.RecalledEvent)
	}
	data, _ := fetched.Payload["Data"].(map[string]any)
	if data["description"] != "Match receipts to the ledger" {
		t.Errorf("full payload missing the description: %v", fetched.Payload)
	}
	if len(fetched.ReferencedResources) != 1 {
		t.Errorf("referenced resources = %v, want the projected reference", fetched.ReferencedResources)
	}
}

// TestEventByURNNormalizesNilPayload pins that a NULL-stored payload fetches
// as an empty object — the MCP output schema types payload as a required
// object, and null would fail it with an opaque protocol error.
func TestEventByURNNormalizesNilPayload(t *testing.T) {
	stub := &stubEventLog{log: []repositories.EventLogEntry{{
		ID:          "bare",
		AggregateID: "urn:task:2t111111111111111111111111",
		EventType:   "Resource.Deleted",
		CreatedAt:   time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC),
	}}}
	recall := &episodicRecall{events: stub, references: &recordingRefsRepo{}, now: time.Now}

	fetched, err := recall.EventByURN(context.Background(), "urn:event:bare")
	if err != nil {
		t.Fatalf("EventByURN: %v", err)
	}
	if fetched.Payload == nil {
		t.Error("nil stored payload not normalized to an empty object")
	}
}

func TestEventByURNUnknownErrors(t *testing.T) {
	recall := &episodicRecall{
		events: &stubEventLog{}, references: &recordingRefsRepo{}, now: time.Now,
	}
	_, err := recall.EventByURN(context.Background(), "urn:event:absent")
	if err == nil {
		t.Fatal("unknown event accepted, want error")
	}
	if !errors.Is(err, ErrUnknownEvent) {
		t.Errorf("error %v is not ErrUnknownEvent", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown") {
		t.Errorf("error %q should say the event is unknown", err)
	}
}

func TestEventByURNValidatesShape(t *testing.T) {
	recall := &episodicRecall{
		events: &stubEventLog{}, references: &recordingRefsRepo{}, now: time.Now,
	}
	for _, urn := range []string{"not-an-event-urn", "urn:task:2t111111111111111111111111", "urn:event:", ""} {
		_, err := recall.EventByURN(context.Background(), urn)
		if err == nil {
			t.Fatalf("identifier %q accepted, want validation error", urn)
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "validation") || !strings.Contains(msg, "event urn") {
			t.Errorf("error %q should mention validation and the event URN shape", err)
		}
	}
}
