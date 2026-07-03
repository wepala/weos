package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestEventPayloadFetch runs the full-payload fetch acceptance scenarios
// (epic #409, story #414): episodic_event_get returns one event's stored
// payload by URN — the explicit drill-in complement to the compact recall
// shape. Fetching reads the event store directly (no projection), so this
// suite runs worker-less. Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestEventPayloadFetch -v
func TestEventPayloadFetch(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "event-payload-fetch",
		ScenarioInitializer: initEventPayloadFetchScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/event_payload_fetch.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("event payload fetch acceptance scenarios failed")
	}
}

// fetchedEvent mirrors episodic_event_get's output: the compact shape plus
// the full stored payload.
type fetchedEvent struct {
	ID          string         `json:"id"`
	EventType   string         `json:"eventType"`
	Timestamp   string         `json:"timestamp"`
	AggregateID string         `json:"aggregateId"`
	Payload     map[string]any `json:"payload"`
}

type payloadFetchWorld struct {
	*episodicWorld
}

func initEventPayloadFetchScenario(sc *godog.ScenarioContext) {
	w := &payloadFetchWorld{episodicWorld: &episodicWorld{
		mcpWorld:   &mcpWorld{},
		aggregates: map[string]string{},
		seqs:       map[string]int{},
		eventIDs:   map[string]string{},
	}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^the following activity is recorded in the event log:$`, w.activityIsRecorded)
	sc.Step(`^I call episodic_recall for events between "([^"]*)" and "([^"]*)"$`, w.callBetween)
	sc.Step(`^I call episodic_event_get with the URN of the "([^"]*)" event for "([^"]*)"$`, w.fetchNamedEvent)
	sc.Step(`^I call episodic_event_get with the event "([^"]*)"$`, w.fetchLiteral)
	sc.Step(`^I call episodic_event_get with the event URN from the first recalled event$`, w.fetchFirstRecalled)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails$`, w.theCallFails)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the fetched event carries:$`, w.fetchedEventCarries)
	sc.Step(`^the fetched payload contains the text "([^"]*)"$`, w.fetchedPayloadContains)
	sc.Step(`^the fetched event is the "([^"]*)" event for "([^"]*)"$`, w.fetchedEventIs)
	sc.Step(`^the error explains the event is unknown$`, w.errorExplainsUnknownEvent)
	sc.Step(`^the error explains the identifier must be an event URN$`, w.errorExplainsEventURNShape)
}

// --- Actions ---

func (w *payloadFetchWorld) fetch(ctx context.Context, urn string) error {
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name:      "episodic_event_get",
		Arguments: json.RawMessage(fmt.Sprintf(`{"urn":%q}`, urn)),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *payloadFetchWorld) fetchNamedEvent(ctx context.Context, eventType, name string) error {
	urn, ok := w.eventIDs[name+"|"+eventType]
	if !ok {
		return fmt.Errorf("no seeded %s event for %q", eventType, name)
	}
	return w.fetch(ctx, urn)
}

func (w *payloadFetchWorld) fetchLiteral(ctx context.Context, urn string) error {
	return w.fetch(ctx, urn)
}

func (w *payloadFetchWorld) fetchFirstRecalled(ctx context.Context) error {
	page, err := w.page()
	if err != nil {
		return fmt.Errorf("no recall to take a URN from: %w", err)
	}
	if len(page.Events) == 0 {
		return fmt.Errorf("the recall returned no events to fetch")
	}
	return w.fetch(ctx, page.Events[0].ID)
}

// --- Outcomes ---

func (w *payloadFetchWorld) fetched() (*fetchedEvent, error) {
	if w.lastErr != nil {
		return nil, fmt.Errorf("episodic_event_get protocol error: %v", w.lastErr)
	}
	if w.lastResult == nil || w.lastResult.IsError {
		return nil, fmt.Errorf("episodic_event_get failed: %s", w.lastText)
	}
	var event fetchedEvent
	if err := json.Unmarshal([]byte(w.lastText), &event); err != nil {
		return nil, fmt.Errorf("unparseable episodic_event_get output: %s", w.lastText)
	}
	return &event, nil
}

func (w *payloadFetchWorld) fetchedEventCarries(table *godog.Table) error {
	event, err := w.fetched()
	if err != nil {
		return err
	}
	shape := episodicEvent{
		ID: event.ID, EventType: event.EventType,
		Timestamp: event.Timestamp, AggregateID: event.AggregateID,
	}
	for _, row := range table.Rows[1:] {
		if err := eventCarries(shape, row.Cells[0].Value); err != nil {
			return err
		}
	}
	return nil
}

func (w *payloadFetchWorld) fetchedPayloadContains(text string) error {
	event, err := w.fetched()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	if !strings.Contains(string(raw), text) {
		return fmt.Errorf("fetched payload %s does not contain %q", raw, text)
	}
	return nil
}

func (w *payloadFetchWorld) fetchedEventIs(eventType, name string) error {
	event, err := w.fetched()
	if err != nil {
		return err
	}
	aggregateID, ok := w.aggregates[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	if event.EventType != eventType || event.AggregateID != aggregateID {
		return fmt.Errorf("fetched %s on %q, want %s on %q",
			event.EventType, w.resourceName(event.AggregateID), eventType, name)
	}
	return nil
}

func (w *payloadFetchWorld) errorExplainsUnknownEvent() error {
	if !strings.Contains(strings.ToLower(w.errMessage()), "unknown") {
		return fmt.Errorf("error %q does not explain the event is unknown", w.errMessage())
	}
	return nil
}

func (w *payloadFetchWorld) errorExplainsEventURNShape() error {
	if !strings.Contains(strings.ToLower(w.errMessage()), "event urn") {
		return fmt.Errorf("error %q does not explain the identifier must be an event URN", w.errMessage())
	}
	return nil
}
