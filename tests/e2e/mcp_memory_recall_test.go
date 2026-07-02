package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPMemoryRecall runs the agent-memory acceptance scenarios (epic #386):
// fact recording via resource_create, same-turn recall through memory_recall
// (working-set path — no Oxigraph is configured in tests, so this exercises
// the degradation the recall surface promises), supersession filtering, and
// playbook outcome recording.
func TestMCPMemoryRecall(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "mcp-memory-recall",
		ScenarioInitializer: initMemoryRecallScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/memory_recall.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("memory recall acceptance scenarios failed")
	}
}

// memoryWorld extends the shared MCP world with fact/playbook bookkeeping.
type memoryWorld struct {
	*mcpWorld
	facts      map[string]recordedFact // alias → fact
	playbookID string
}

type recordedFact struct {
	id        string
	statement string
	about     string
}

func initMemoryRecallScenario(sc *godog.ScenarioContext) {
	w := &memoryWorld{mcpWorld: &mcpWorld{}, facts: map[string]recordedFact{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^I call resource_create for type "([^"]*)" with the data:$`, w.iCallResourceCreate)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^a recorded fact "([^"]*)" stating "([^"]*)" about "([^"]*)"$`, w.aRecordedFact)
	sc.Step(`^I record a fact superseding "([^"]*)" stating "([^"]*)"$`, w.iRecordSupersedingFact)
	sc.Step(`^I call memory_recall for facts about "([^"]*)"$`, w.iCallMemoryRecall)
	sc.Step(`^the recall includes a fact stating "([^"]*)"$`, w.recallIncludes)
	sc.Step(`^the recall does not include a fact stating "([^"]*)"$`, w.recallExcludes)
	sc.Step(`^a recorded playbook named "([^"]*)"$`, w.aRecordedPlaybook)
	sc.Step(`^I call playbook_record_outcome for that playbook with outcome "([^"]*)"$`,
		w.iCallPlaybookRecordOutcome)
	sc.Step(`^the playbook outcome reports success count (\d+) and failure count (\d+)$`,
		w.playbookOutcomeReports)
}

func (w *memoryWorld) presetIsInstalled(ctx context.Context, name string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted")
	}
	if _, err := w.rts.InstallPreset(ctx, name, true); err != nil {
		return fmt.Errorf("failed to install %q preset: %w", name, err)
	}
	return nil
}

// createFact records a fact through the same MCP tool an agent would use and
// returns its URN.
func (w *memoryWorld) createFact(ctx context.Context, data map[string]any) (string, error) {
	raw, err := json.Marshal(map[string]any{"type_slug": "fact", "data": data})
	if err != nil {
		return "", err
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_create", Arguments: json.RawMessage(raw),
	})
	if err != nil {
		return "", fmt.Errorf("resource_create protocol error: %w", err)
	}
	if res.IsError {
		return "", fmt.Errorf("resource_create failed: %s", textOf(res))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("no id in resource_create result: %s", textOf(res))
	}
	return out.ID, nil
}

func (w *memoryWorld) aRecordedFact(ctx context.Context, alias, statement, about string) error {
	id, err := w.createFact(ctx, map[string]any{"statement": statement, "about": about})
	if err != nil {
		return err
	}
	w.facts[alias] = recordedFact{id: id, statement: statement, about: about}
	return nil
}

// iRecordSupersedingFact performs the two-step supersession an agent (or the
// consolidation policy) runs: create the revision with wasRevisionOf, then
// stamp invalidatedAtTime on the predecessor via a full-data update.
func (w *memoryWorld) iRecordSupersedingFact(ctx context.Context, alias, statement string) error {
	old, ok := w.facts[alias]
	if !ok {
		return fmt.Errorf("no recorded fact aliased %q", alias)
	}
	if _, err := w.createFact(ctx, map[string]any{
		"statement":     statement,
		"about":         old.about,
		"wasRevisionOf": old.id,
	}); err != nil {
		return fmt.Errorf("create superseding fact: %w", err)
	}
	update, err := json.Marshal(map[string]any{
		"id": old.id,
		"data": map[string]any{
			"statement":         old.statement,
			"about":             old.about,
			"invalidatedAtTime": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return err
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_update", Arguments: json.RawMessage(update),
	})
	if err != nil {
		return fmt.Errorf("resource_update protocol error: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("invalidating predecessor failed: %s", textOf(res))
	}
	return nil
}

func (w *memoryWorld) iCallMemoryRecall(ctx context.Context, about string) error {
	args := fmt.Sprintf(`{"about":%q}`, about)
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "memory_recall", Arguments: json.RawMessage(args),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *memoryWorld) recalledStatements() ([]string, error) {
	if w.lastErr != nil {
		return nil, fmt.Errorf("memory_recall protocol error: %v", w.lastErr)
	}
	if w.lastResult == nil || w.lastResult.IsError {
		return nil, fmt.Errorf("memory_recall failed: %s", w.lastText)
	}
	var out struct {
		Facts []struct {
			Statement string `json:"statement"`
		} `json:"facts"`
	}
	if err := json.Unmarshal([]byte(w.lastText), &out); err != nil {
		return nil, fmt.Errorf("unparseable memory_recall output: %s", w.lastText)
	}
	statements := make([]string, 0, len(out.Facts))
	for _, f := range out.Facts {
		statements = append(statements, f.Statement)
	}
	return statements, nil
}

func (w *memoryWorld) recallIncludes(statement string) error {
	statements, err := w.recalledStatements()
	if err != nil {
		return err
	}
	for _, s := range statements {
		if s == statement {
			return nil
		}
	}
	return fmt.Errorf("recall missing %q; got %v", statement, statements)
}

func (w *memoryWorld) recallExcludes(statement string) error {
	statements, err := w.recalledStatements()
	if err != nil {
		return err
	}
	for _, s := range statements {
		if s == statement {
			return fmt.Errorf("superseded/unrelated fact %q still recalled; got %v", statement, statements)
		}
	}
	return nil
}

func (w *memoryWorld) aRecordedPlaybook(ctx context.Context, name string) error {
	raw, err := json.Marshal(map[string]any{
		"type_slug": "playbook",
		"data": map[string]any{
			"name":    name,
			"trigger": "acceptance test",
			"steps":   []string{"do the thing"},
		},
	})
	if err != nil {
		return err
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_create", Arguments: json.RawMessage(raw),
	})
	if err != nil {
		return fmt.Errorf("create playbook protocol error: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("create playbook failed: %s", textOf(res))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil || out.ID == "" {
		return fmt.Errorf("no id in playbook create result: %s", textOf(res))
	}
	w.playbookID = out.ID
	return nil
}

func (w *memoryWorld) iCallPlaybookRecordOutcome(ctx context.Context, outcome string) error {
	if w.playbookID == "" {
		return fmt.Errorf("no recorded playbook")
	}
	args := fmt.Sprintf(`{"id":%q,"outcome":%q}`, w.playbookID, outcome)
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "playbook_record_outcome", Arguments: json.RawMessage(args),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *memoryWorld) playbookOutcomeReports(success, failure int) error {
	var out struct {
		SuccessCount int `json:"successCount"`
		FailureCount int `json:"failureCount"`
	}
	if err := json.Unmarshal([]byte(w.lastText), &out); err != nil {
		return fmt.Errorf("unparseable playbook_record_outcome output: %s", w.lastText)
	}
	if out.SuccessCount != success || out.FailureCount != failure {
		return fmt.Errorf("counters = %d/%d, want %d/%d",
			out.SuccessCount, out.FailureCount, success, failure)
	}
	return nil
}
