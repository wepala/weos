package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/identity"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestEpisodicRecall runs the episodic_recall MCP-tool acceptance scenarios
// (epic #409, story #410) against a freshly booted application with a clean
// SQLite database. Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestEpisodicRecall -v
func TestEpisodicRecall(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "episodic-recall",
		ScenarioInitializer: initEpisodicRecallScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/episodic_recall.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("episodic recall acceptance scenarios failed")
	}
}

// episodicWorld extends the shared MCP world with event-log seeding and the
// bookkeeping the pagination/determinism scenarios need.
type episodicWorld struct {
	*mcpWorld
	// aggregates maps a seeded resource's name to its aggregate URN so
	// scenarios can speak in names while assertions match on IDs.
	aggregates map[string]string
	// seqs tracks the next sequence number per aggregate.
	seqs      map[string]int
	firstPage *episodicPage
	// firstFrom/firstUntil replay the first page's window on the cursor call,
	// the same way resource_list callers re-send their filters each page.
	firstFrom  string
	firstUntil string
	// lastMatched is the event the most recent includes-assertion found, for
	// follow-up "that event" assertions.
	lastMatched *episodicEvent
	// secondText holds the second response of the determinism scenario.
	secondText string
}

// episodicEvent mirrors the compact result shape episodic_recall returns.
type episodicEvent struct {
	ID           string `json:"id"`
	EventType    string `json:"eventType"`
	Timestamp    string `json:"timestamp"`
	AggregateID  string `json:"aggregateId"`
	ResourceType string `json:"resourceType"`
	Summary      string `json:"summary"`
}

type episodicPage struct {
	Events  []episodicEvent `json:"events"`
	Cursor  string          `json:"cursor"`
	HasMore bool            `json:"has_more"`
}

func initEpisodicRecallScenario(sc *godog.ScenarioContext) {
	w := &episodicWorld{
		mcpWorld:   &mcpWorld{},
		aggregates: map[string]string{},
		seqs:       map[string]int{},
	}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^the following activity is recorded in the event log:$`, w.activityIsRecorded)
	sc.Step(`^(\d+) "([^"]*)" resources were created on consecutive days starting "([^"]*)"$`,
		w.resourcesCreatedOnConsecutiveDays)
	sc.Step(`^I call episodic_recall for events between "([^"]*)" and "([^"]*)"$`, w.callBetween)
	sc.Step(`^I call episodic_recall for events between "([^"]*)" and "([^"]*)" twice$`, w.callBetweenTwice)
	sc.Step(`^I call episodic_recall for events from the last (\d+) days$`, w.callLastDays)
	sc.Step(`^I call episodic_recall for events about the resource named "([^"]*)"$`, w.callAboutResource)
	sc.Step(`^I call episodic_recall for events of type "([^"]*)"$`, w.callEventType)
	sc.Step(`^I call episodic_recall for events of resource type "([^"]*)"$`, w.callResourceType)
	sc.Step(`^I call episodic_recall with the filters:$`, w.callWithFilters)
	sc.Step(`^I call episodic_recall requesting up to (\d+) events$`, w.callWithLimit)
	sc.Step(`^I have recalled the first page of events between "([^"]*)" and "([^"]*)"$`, w.recallFirstPage)
	sc.Step(`^I call episodic_recall with the cursor from the first page$`, w.callWithCursor)
	sc.Step(`^I call episodic_recall with the cursor "([^"]*)"$`, w.callWithLiteralCursor)
	sc.Step(`^I call resource_create for type "([^"]*)" with the data:$`, w.iCallResourceCreateTracked)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the recall returns exactly these events in order:$`, w.recallReturnsExactly)
	sc.Step(`^the recall returns (\d+) events$`, w.recallReturnsCount)
	sc.Step(`^the recall returns no events$`, w.recallReturnsNone)
	sc.Step(`^the recall reports more events are available$`, w.recallReportsMore)
	sc.Step(`^the recall reports no more events are available$`, w.recallReportsNoMore)
	sc.Step(`^the recall provides a cursor for the next page$`, w.recallProvidesCursor)
	sc.Step(`^no event from the first page is repeated$`, w.noFirstPageEventRepeated)
	sc.Step(`^every recalled event carries:$`, w.everyEventCarries)
	sc.Step(`^the recall does not include the full payload text "([^"]*)"$`, w.recallExcludesPayloadText)
	sc.Step(`^every recalled event has type "([^"]*)"$`, w.everyEventHasType)
	sc.Step(`^every recalled event is for a "([^"]*)" resource$`, w.everyEventIsForResourceType)
	sc.Step(`^the recall includes the "([^"]*)" event for "([^"]*)"$`, w.recallIncludesEventFor)
	sc.Step(`^the recall does not include an event for "([^"]*)"$`, w.recallExcludesEventsFor)
	sc.Step(`^the error explains the time range is invalid$`, w.errorExplainsInvalidTimeRange)
	sc.Step(`^the error explains the cursor is invalid$`, w.errorExplainsInvalidCursor)
	sc.Step(`^the payload summary for that event carries "([^"]*)"$`, w.matchedEventSummaryCarries)
	sc.Step(`^both recalls return identical events in identical order$`, w.bothRecallsIdentical)
}

// --- Seeding ---

var daysAgo = regexp.MustCompile(`^(\d+) days? ago$`)

// parseOccurredAt accepts RFC3339 or the relative "N days ago" the feature
// file uses for scenarios anchored to the current date.
func parseOccurredAt(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if m := daysAgo.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		days, err := strconv.Atoi(m[1])
		if err == nil {
			return time.Now().UTC().AddDate(0, 0, -days), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported occurred-at value %q", s)
}

// seedEvent appends one event with a controlled occurred-at timestamp
// directly to the event store — the entity API always stamps time.Now(), so
// backdating has to bypass it. Payload shapes mirror the real resource events.
func (w *episodicWorld) seedEvent(
	ctx context.Context, typeSlug, name, description, eventType string, occurredAt time.Time,
) error {
	if w.eventStore == nil {
		return fmt.Errorf("application not booted")
	}
	aggregateID, ok := w.aggregates[name]
	if !ok {
		aggregateID = identity.NewResource(typeSlug)
		w.aggregates[name] = aggregateID
	}
	w.seqs[aggregateID]++

	data := map[string]any{"name": name}
	if description != "" {
		data["description"] = description
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var payload any
	switch eventType {
	case "Resource.Created":
		payload = entities.ResourceCreated{TypeSlug: typeSlug, Data: raw, Timestamp: occurredAt}
	case "Resource.Updated":
		payload = entities.ResourceUpdated{Data: raw, Timestamp: occurredAt}
	default:
		return fmt.Errorf("seeding step does not support event type %q", eventType)
	}

	envelope := pericarpdomain.NewEventEnvelope(payload, aggregateID, eventType, w.seqs[aggregateID])
	envelope.Created = occurredAt
	return w.eventStore.Append(ctx, aggregateID, -1, envelope)
}

func (w *episodicWorld) activityIsRecorded(ctx context.Context, table *godog.Table) error {
	if len(table.Rows) < 2 {
		return fmt.Errorf("activity table needs a header and at least one row")
	}
	cols := map[string]int{}
	for i, cell := range table.Rows[0].Cells {
		cols[cell.Value] = i
	}
	for _, required := range []string{"resource-type", "name", "event-type", "occurred-at"} {
		if _, ok := cols[required]; !ok {
			return fmt.Errorf("activity table is missing the %q column", required)
		}
	}
	for _, row := range table.Rows[1:] {
		occurredAt, err := parseOccurredAt(row.Cells[cols["occurred-at"]].Value)
		if err != nil {
			return err
		}
		description := ""
		if i, ok := cols["description"]; ok {
			description = row.Cells[i].Value
		}
		if err := w.seedEvent(ctx,
			row.Cells[cols["resource-type"]].Value,
			row.Cells[cols["name"]].Value,
			description,
			row.Cells[cols["event-type"]].Value,
			occurredAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (w *episodicWorld) resourcesCreatedOnConsecutiveDays(
	ctx context.Context, count int, typeSlug, start string,
) error {
	startAt, err := parseOccurredAt(start)
	if err != nil {
		return err
	}
	for i := range count {
		name := fmt.Sprintf("%s %03d", typeSlug, i+1)
		if err := w.seedEvent(
			ctx, typeSlug, name, "", "Resource.Created", startAt.AddDate(0, 0, i),
		); err != nil {
			return err
		}
	}
	return nil
}

// --- Actions ---

func (w *episodicWorld) callEpisodic(ctx context.Context, args map[string]any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "episodic_recall", Arguments: json.RawMessage(raw),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *episodicWorld) callBetween(ctx context.Context, from, until string) error {
	return w.callEpisodic(ctx, map[string]any{"from": from, "until": until})
}

func (w *episodicWorld) callBetweenTwice(ctx context.Context, from, until string) error {
	if err := w.callBetween(ctx, from, until); err != nil {
		return err
	}
	first := w.lastText
	if err := w.callBetween(ctx, from, until); err != nil {
		return err
	}
	w.secondText = w.lastText
	w.lastText = first
	return nil
}

func (w *episodicWorld) callLastDays(ctx context.Context, days int) error {
	return w.callEpisodic(ctx, map[string]any{"from": fmt.Sprintf("last %d days", days)})
}

func (w *episodicWorld) callAboutResource(ctx context.Context, name string) error {
	aggregateID, ok := w.aggregates[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	return w.callEpisodic(ctx, map[string]any{"about": aggregateID})
}

func (w *episodicWorld) callEventType(ctx context.Context, eventType string) error {
	return w.callEpisodic(ctx, map[string]any{"eventType": eventType})
}

func (w *episodicWorld) callResourceType(ctx context.Context, typeSlug string) error {
	return w.callEpisodic(ctx, map[string]any{"resourceType": typeSlug})
}

func (w *episodicWorld) callWithFilters(ctx context.Context, table *godog.Table) error {
	args := map[string]any{}
	keys := map[string]string{
		"from": "from", "until": "until",
		"event-type": "eventType", "resource-type": "resourceType",
	}
	for _, row := range table.Rows[1:] {
		if len(row.Cells) != 2 {
			return fmt.Errorf("filter rows need exactly a filter and a value")
		}
		key, ok := keys[row.Cells[0].Value]
		if !ok {
			return fmt.Errorf("unsupported filter %q", row.Cells[0].Value)
		}
		args[key] = row.Cells[1].Value
	}
	return w.callEpisodic(ctx, args)
}

func (w *episodicWorld) callWithLimit(ctx context.Context, limit int) error {
	return w.callEpisodic(ctx, map[string]any{"limit": limit})
}

func (w *episodicWorld) recallFirstPage(ctx context.Context, from, until string) error {
	if err := w.callBetween(ctx, from, until); err != nil {
		return err
	}
	page, err := w.page()
	if err != nil {
		return fmt.Errorf("first page: %w", err)
	}
	w.firstPage = page
	w.firstFrom, w.firstUntil = from, until
	return nil
}

func (w *episodicWorld) callWithLiteralCursor(ctx context.Context, cursor string) error {
	return w.callEpisodic(ctx, map[string]any{"cursor": cursor})
}

// iCallResourceCreateTracked creates a resource through the live tool and
// records its name → URN mapping so recall assertions can resolve it like the
// seeded ones.
func (w *episodicWorld) iCallResourceCreateTracked(
	ctx context.Context, typeSlug string, data *godog.DocString,
) error {
	if err := w.iCallResourceCreate(ctx, typeSlug, data); err != nil {
		return err
	}
	if w.lastErr != nil || w.lastResult == nil || w.lastResult.IsError {
		return fmt.Errorf("resource_create failed: %s", w.lastText)
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(data.Content), &in); err != nil || in.Name == "" {
		return fmt.Errorf("no name in resource_create input: %s", data.Content)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(w.lastText), &out); err != nil || out.ID == "" {
		return fmt.Errorf("no id in resource_create result: %s", w.lastText)
	}
	w.aggregates[in.Name] = out.ID
	return nil
}

func (w *episodicWorld) callWithCursor(ctx context.Context) error {
	if w.firstPage == nil || w.firstPage.Cursor == "" {
		return fmt.Errorf("no cursor captured from a first page")
	}
	return w.callEpisodic(ctx, map[string]any{
		"from": w.firstFrom, "until": w.firstUntil, "cursor": w.firstPage.Cursor,
	})
}

// --- Outcomes ---

func (w *episodicWorld) page() (*episodicPage, error) {
	if w.lastErr != nil {
		return nil, fmt.Errorf("episodic_recall protocol error: %v", w.lastErr)
	}
	if w.lastResult == nil || w.lastResult.IsError {
		return nil, fmt.Errorf("episodic_recall failed: %s", w.lastText)
	}
	var page episodicPage
	if err := json.Unmarshal([]byte(w.lastText), &page); err != nil {
		return nil, fmt.Errorf("unparseable episodic_recall output: %s", w.lastText)
	}
	return &page, nil
}

// resourceName reverse-maps an aggregate URN to the seeded resource's name
// for readable assertion failures.
func (w *episodicWorld) resourceName(aggregateID string) string {
	for name, id := range w.aggregates {
		if id == aggregateID {
			return name
		}
	}
	return aggregateID
}

func (w *episodicWorld) recallReturnsExactly(table *godog.Table) error {
	page, err := w.page()
	if err != nil {
		return err
	}
	expected := table.Rows[1:]
	if len(page.Events) != len(expected) {
		return fmt.Errorf("recall returned %d events, want %d: %s",
			len(page.Events), len(expected), w.lastText)
	}
	for i, row := range expected {
		wantName, wantType := row.Cells[0].Value, row.Cells[1].Value
		wantAggregate, ok := w.aggregates[wantName]
		if !ok {
			return fmt.Errorf("no seeded resource named %q", wantName)
		}
		got := page.Events[i]
		if got.AggregateID != wantAggregate || got.EventType != wantType {
			return fmt.Errorf("event %d = %s on %q, want %s on %q",
				i, got.EventType, w.resourceName(got.AggregateID), wantType, wantName)
		}
	}
	return nil
}

func (w *episodicWorld) recallReturnsCount(count int) error {
	page, err := w.page()
	if err != nil {
		return err
	}
	if len(page.Events) != count {
		return fmt.Errorf("recall returned %d events, want %d", len(page.Events), count)
	}
	return nil
}

func (w *episodicWorld) recallReturnsNone() error {
	return w.recallReturnsCount(0)
}

func (w *episodicWorld) recallReportsMore() error {
	page, err := w.page()
	if err != nil {
		return err
	}
	if !page.HasMore {
		return fmt.Errorf("recall reports no more events, want has_more")
	}
	return nil
}

func (w *episodicWorld) recallReportsNoMore() error {
	page, err := w.page()
	if err != nil {
		return err
	}
	if page.HasMore {
		return fmt.Errorf("recall reports more events, want none")
	}
	return nil
}

func (w *episodicWorld) recallProvidesCursor() error {
	page, err := w.page()
	if err != nil {
		return err
	}
	if page.Cursor == "" {
		return fmt.Errorf("recall provided no cursor")
	}
	return nil
}

func (w *episodicWorld) noFirstPageEventRepeated() error {
	if w.firstPage == nil {
		return fmt.Errorf("no first page captured")
	}
	page, err := w.page()
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(w.firstPage.Events))
	for _, e := range w.firstPage.Events {
		seen[e.ID] = true
	}
	for _, e := range page.Events {
		if seen[e.ID] {
			return fmt.Errorf("event %s appears on both pages", e.ID)
		}
	}
	return nil
}

func (w *episodicWorld) everyEventCarries(table *godog.Table) error {
	page, err := w.page()
	if err != nil {
		return err
	}
	if len(page.Events) == 0 {
		return fmt.Errorf("recall returned no events to inspect")
	}
	for i, e := range page.Events {
		for _, row := range table.Rows[1:] {
			if err := eventCarries(e, row.Cells[0].Value); err != nil {
				return fmt.Errorf("event %d: %w", i, err)
			}
		}
	}
	return nil
}

func eventCarries(e episodicEvent, field string) error {
	switch field {
	case "event URN":
		if !strings.HasPrefix(e.ID, "urn:event:") {
			return fmt.Errorf("id %q is not an event URN", e.ID)
		}
	case "event type":
		if e.EventType == "" {
			return fmt.Errorf("missing event type")
		}
	case "timestamp":
		if _, err := time.Parse(time.RFC3339, e.Timestamp); err != nil {
			return fmt.Errorf("timestamp %q is not RFC3339", e.Timestamp)
		}
	case "aggregate ID":
		if e.AggregateID == "" {
			return fmt.Errorf("missing aggregate ID")
		}
	case "payload summary":
		if e.Summary == "" {
			return fmt.Errorf("missing payload summary")
		}
	default:
		return fmt.Errorf("unknown field %q", field)
	}
	return nil
}

func (w *episodicWorld) recallExcludesPayloadText(text string) error {
	if _, err := w.page(); err != nil {
		return err
	}
	if strings.Contains(w.lastText, text) {
		return fmt.Errorf("full payload text %q leaked into the recall output", text)
	}
	return nil
}

func (w *episodicWorld) everyEventHasType(eventType string) error {
	page, err := w.page()
	if err != nil {
		return err
	}
	if len(page.Events) == 0 {
		return fmt.Errorf("recall returned no events")
	}
	for _, e := range page.Events {
		if e.EventType != eventType {
			return fmt.Errorf("event %s has type %s, want %s", e.ID, e.EventType, eventType)
		}
	}
	return nil
}

func (w *episodicWorld) everyEventIsForResourceType(typeSlug string) error {
	page, err := w.page()
	if err != nil {
		return err
	}
	if len(page.Events) == 0 {
		return fmt.Errorf("recall returned no events")
	}
	for _, e := range page.Events {
		if !strings.HasPrefix(e.AggregateID, "urn:"+typeSlug+":") {
			return fmt.Errorf("event %s is on %s, want a %q resource", e.ID, e.AggregateID, typeSlug)
		}
	}
	return nil
}

func (w *episodicWorld) recallIncludesEventFor(eventType, name string) error {
	page, err := w.page()
	if err != nil {
		return err
	}
	aggregateID, ok := w.aggregates[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	for i, e := range page.Events {
		if e.AggregateID == aggregateID && e.EventType == eventType {
			w.lastMatched = &page.Events[i]
			return nil
		}
	}
	return fmt.Errorf("recall missing the %s event for %q: %s", eventType, name, w.lastText)
}

func (w *episodicWorld) recallExcludesEventsFor(name string) error {
	page, err := w.page()
	if err != nil {
		return err
	}
	aggregateID, ok := w.aggregates[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	for _, e := range page.Events {
		if e.AggregateID == aggregateID {
			return fmt.Errorf("recall unexpectedly includes the %s event for %q", e.EventType, name)
		}
	}
	return nil
}

func (w *episodicWorld) errorExplainsInvalidCursor() error {
	msg := strings.ToLower(w.errMessage())
	if !strings.Contains(msg, "cursor") {
		return fmt.Errorf("error %q does not explain the cursor is invalid", w.errMessage())
	}
	return nil
}

func (w *episodicWorld) matchedEventSummaryCarries(text string) error {
	if w.lastMatched == nil {
		return fmt.Errorf("no event matched by a preceding includes step")
	}
	if !strings.Contains(w.lastMatched.Summary, text) {
		return fmt.Errorf("summary %q does not carry %q", w.lastMatched.Summary, text)
	}
	return nil
}

func (w *episodicWorld) errorExplainsInvalidTimeRange() error {
	msg := strings.ToLower(w.errMessage())
	if !strings.Contains(msg, "time range") {
		return fmt.Errorf("error %q does not explain the time range is invalid", w.errMessage())
	}
	return nil
}

func (w *episodicWorld) bothRecallsIdentical() error {
	first, err := w.page()
	if err != nil {
		return fmt.Errorf("first recall: %w", err)
	}
	var second episodicPage
	if err := json.Unmarshal([]byte(w.secondText), &second); err != nil {
		return fmt.Errorf("unparseable second recall output: %s", w.secondText)
	}
	if len(first.Events) != len(second.Events) {
		return fmt.Errorf("recalls returned %d and %d events", len(first.Events), len(second.Events))
	}
	for i := range first.Events {
		if first.Events[i] != second.Events[i] {
			return fmt.Errorf("event %d differs between recalls: %+v vs %+v",
				i, first.Events[i], second.Events[i])
		}
	}
	return nil
}
