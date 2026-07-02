package application

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
)

const testPlaybookContext = `{"@vocab":"https://schema.org/",` +
	`"mem":"https://weos.org/vocab/memory#","@type":"mem:Playbook",` +
	`"trigger":"https://weos.org/vocab/memory#triggerCondition",` +
	`"steps":"https://weos.org/vocab/memory#steps",` +
	`"successCount":"https://weos.org/vocab/memory#successCount",` +
	`"failureCount":"https://weos.org/vocab/memory#failureCount"}`

type fakePlaybookResources struct {
	byID      map[string]*entities.Resource
	updated   []UpdateResourceCommand
	updateCtx context.Context
}

func (f *fakePlaybookResources) GetByID(_ context.Context, id string) (*entities.Resource, error) {
	return f.byID[id], nil
}

func (f *fakePlaybookResources) Update(
	ctx context.Context, cmd UpdateResourceCommand,
) (*entities.Resource, error) {
	f.updated = append(f.updated, cmd)
	f.updateCtx = ctx
	return f.byID[cmd.ID], nil
}

func testPlaybook(t *testing.T, id string, flat map[string]any) *entities.Resource {
	t.Helper()
	raw, err := json.Marshal(flat)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	graph, err := BuildResourceGraph(raw, nil, id, "Playbook", json.RawMessage(testPlaybookContext))
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	res, err := new(entities.Resource).With(id, "playbook", graph, "", "")
	if err != nil {
		t.Fatalf("build resource: %v", err)
	}
	return res
}

func TestPlaybookService_RecordOutcomeIncrementsCounterViaUpdate(t *testing.T) {
	t.Parallel()

	pb := testPlaybook(t, "urn:playbook:1", map[string]any{
		"name":         "sync upstream",
		"trigger":      "finexity features merged upstream",
		"steps":        []string{"create sync branch", "resolve conflicts", "open PR"},
		"successCount": 2,
	})
	store := &fakePlaybookResources{byID: map[string]*entities.Resource{"urn:playbook:1": pb}}
	svc := &playbookService{resources: store}

	if _, err := svc.RecordOutcome(
		context.Background(), "urn:playbook:1", entities.PlaybookOutcomeConfirmed, "worked"); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	if len(store.updated) != 1 {
		t.Fatalf("updates = %d, want 1 (counters change only via events)", len(store.updated))
	}
	var data map[string]any
	if err := json.Unmarshal(store.updated[0].Data, &data); err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	if got, _ := data["successCount"].(float64); got != 3 {
		t.Errorf("successCount = %v, want 3", data["successCount"])
	}
	if data["name"] != "sync upstream" || data["trigger"] == nil {
		t.Error("update dropped existing fields — Update is full-replace")
	}
	steps, _ := data["steps"].([]any)
	if len(steps) != 3 {
		t.Errorf("steps = %v, want the original 3", data["steps"])
	}
	outcome, note, ok := entities.PlaybookOutcomeFromCtx(store.updateCtx)
	if !ok || outcome != entities.PlaybookOutcomeConfirmed || note != "worked" {
		t.Errorf("outcome marker = (%q,%q,%v), want (confirmed,worked,true) — behavior needs it to record the signal",
			outcome, note, ok)
	}
}

func TestPlaybookService_RejectedIncrementsFailureCount(t *testing.T) {
	t.Parallel()

	pb := testPlaybook(t, "urn:playbook:1", map[string]any{"name": "deploy"})
	store := &fakePlaybookResources{byID: map[string]*entities.Resource{"urn:playbook:1": pb}}
	svc := &playbookService{resources: store}

	if _, err := svc.RecordOutcome(
		context.Background(), "urn:playbook:1", entities.PlaybookOutcomeRejected, ""); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(store.updated[0].Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, _ := data["failureCount"].(float64); got != 1 {
		t.Errorf("failureCount = %v, want 1 (absent counter starts at 0)", data["failureCount"])
	}
	if _, has := data["successCount"]; has {
		t.Error("successCount must stay absent when only a failure is recorded")
	}
}

func TestPlaybookService_RejectsInvalidOutcomeAndWrongType(t *testing.T) {
	t.Parallel()

	note := testFact(t, "urn:fact:1", map[string]any{"statement": "not a playbook"})
	store := &fakePlaybookResources{byID: map[string]*entities.Resource{"urn:fact:1": note}}
	svc := &playbookService{resources: store}

	if _, err := svc.RecordOutcome(
		context.Background(), "urn:playbook:1", "maybe", ""); err == nil {
		t.Error("expected error for invalid outcome value")
	}
	if _, err := svc.RecordOutcome(
		context.Background(), "urn:fact:1", entities.PlaybookOutcomeConfirmed, ""); err == nil {
		t.Error("expected error when the resource is not a playbook")
	}
	if len(store.updated) != 0 {
		t.Errorf("updates = %d, want 0", len(store.updated))
	}
}
