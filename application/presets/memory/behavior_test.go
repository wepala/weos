package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

func factResource(t *testing.T, id string, flat map[string]any) *entities.Resource {
	t.Helper()
	raw, err := json.Marshal(flat)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	refProps := application.ExtractReferenceProperties(
		json.RawMessage(factSchema), json.RawMessage(factContext))
	graph, err := application.BuildResourceGraph(raw, refProps, id, "Fact", json.RawMessage(factContext))
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	res, err := new(entities.Resource).With(id, "fact", graph, "", "")
	if err != nil {
		t.Fatalf("build resource: %v", err)
	}
	return res
}

func eventTypes(res *entities.Resource) []string {
	var types []string
	for _, e := range res.GetUncommittedEvents() {
		types = append(types, e.EventType)
	}
	return types
}

func TestFactBehavior_RecordsFactRecorded(t *testing.T) {
	t.Parallel()

	res := factResource(t, "urn:fact:new", map[string]any{
		"statement":      "Akeem prefers PRs based on v3",
		"wasDerivedFrom": []string{"urn:event:ev1"},
	})
	b := FactBehavior(application.BehaviorServices{})
	if err := b.BeforeCreateCommit(context.Background(), res); err != nil {
		t.Fatalf("BeforeCreateCommit: %v", err)
	}

	var recorded entities.FactRecorded
	found := false
	for _, e := range res.GetUncommittedEvents() {
		if p, ok := e.Payload.(entities.FactRecorded); ok {
			recorded, found = p, true
		}
		if _, ok := e.Payload.(entities.FactSuperseded); ok {
			t.Errorf("unexpected Fact.Superseded without wasRevisionOf (events: %v)", eventTypes(res))
		}
	}
	if !found {
		t.Fatalf("no Fact.Recorded event recorded (events: %v)", eventTypes(res))
	}
	if recorded.FactID != "urn:fact:new" {
		t.Errorf("FactID = %s", recorded.FactID)
	}
	if recorded.Statement != "Akeem prefers PRs based on v3" {
		t.Errorf("Statement = %s", recorded.Statement)
	}
	if len(recorded.DerivedFrom) != 1 || recorded.DerivedFrom[0] != "urn:event:ev1" {
		t.Errorf("DerivedFrom = %v", recorded.DerivedFrom)
	}
}

func TestFactBehavior_RecordsFactSupersededForRevisions(t *testing.T) {
	t.Parallel()

	res := factResource(t, "urn:fact:new", map[string]any{
		"statement":     "updated belief",
		"wasRevisionOf": "urn:fact:old",
	})
	b := FactBehavior(application.BehaviorServices{})
	if err := b.BeforeCreateCommit(context.Background(), res); err != nil {
		t.Fatalf("BeforeCreateCommit: %v", err)
	}

	var superseded entities.FactSuperseded
	found := false
	for _, e := range res.GetUncommittedEvents() {
		if p, ok := e.Payload.(entities.FactSuperseded); ok {
			superseded, found = p, true
		}
	}
	if !found {
		t.Fatalf("no Fact.Superseded event recorded (events: %v)", eventTypes(res))
	}
	if superseded.FactID != "urn:fact:old" {
		t.Errorf("FactID = %s, want the superseded fact", superseded.FactID)
	}
	if superseded.SupersededBy != "urn:fact:new" {
		t.Errorf("SupersededBy = %s, want the new fact", superseded.SupersededBy)
	}
}
