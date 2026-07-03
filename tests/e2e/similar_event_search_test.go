package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSimilarEventSearch runs the similar-event search acceptance scenarios
// (epic #409, story #413): episodic_similar ranks events by deterministic
// structural similarity to a seed event. Ranking reads the event-reference
// projection, so the suite runs the background subscribers in-process and
// polls. Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestSimilarEventSearch -v
func TestSimilarEventSearch(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "similar-event-search",
		ScenarioInitializer: initSimilarEventSearchScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/similar_event_search.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("similar-event search acceptance scenarios failed")
	}
}

// similarEvent mirrors one ranked result: the compact shape plus its score.
// Similarity is a pointer so shape assertions can tell "absent" from zero.
type similarEvent struct {
	ID                  string   `json:"id"`
	EventType           string   `json:"eventType"`
	Timestamp           string   `json:"timestamp"`
	AggregateID         string   `json:"aggregateId"`
	ResourceType        string   `json:"resourceType"`
	Summary             string   `json:"summary"`
	ReferencedResources []string `json:"referencedResources"`
	Similarity          *float64 `json:"similarity"`
}

type similarPage struct {
	Results []similarEvent `json:"results"`
}

type similarWorld struct {
	*anchoredWorld
	// similarArgs replays the last episodic_similar call while polling.
	similarArgs map[string]any
	seedURN     string
	// firstText/secondText hold the determinism pair's raw responses.
	firstText  string
	secondText string
}

func initSimilarEventSearchScenario(sc *godog.ScenarioContext) {
	w := &similarWorld{anchoredWorld: &anchoredWorld{eventRefsWorld: &eventRefsWorld{
		episodicWorld: &episodicWorld{
			mcpWorld:   &mcpWorld{runWorkers: true},
			aggregates: map[string]string{},
			seqs:       map[string]int{},
			eventIDs:   map[string]string{},
		},
	}}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^the following activity is recorded in the event log:$`, w.activityIsRecorded)
	sc.Step(`^(\d+) "([^"]*)" resources linked to "([^"]*)" were created on consecutive days starting "([^"]*)"$`,
		w.linkedResourcesCreatedOnConsecutiveDays)
	sc.Step(`^I call episodic_similar seeded with the "([^"]*)" event for "([^"]*)"$`, w.callSimilarSeededNamed)
	sc.Step(`^I call episodic_similar seeded with the "([^"]*)" event for "([^"]*)" twice$`,
		w.callSimilarSeededNamedTwice)
	//nolint:lll // step regex
	sc.Step(`^I call episodic_similar seeded with the "([^"]*)" event for "([^"]*)" requesting up to (\d+) events$`,
		w.callSimilarSeededNamedWithLimit)
	sc.Step(`^I call episodic_similar seeded with the event "([^"]*)"$`, w.callSimilarSeededLiteral)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails$`, w.theCallFails)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the results do not include the seed event$`, w.resultsExcludeSeed)
	sc.Step(`^the results list exactly these events in order:$`, w.resultsListExactly)
	sc.Step(`^the results contain (\d+) events$`, w.resultsContainCount)
	sc.Step(`^every result carries:$`, w.everyResultCarries)
	sc.Step(`^the results are ordered from most to least similar$`, w.resultsOrderedByScore)
	sc.Step(`^both searches return identical events in identical order$`, w.bothSearchesIdentical)
	sc.Step(`^the error explains the seed event is unknown$`, w.errorExplainsUnknownSeed)
	sc.Step(`^the error explains the seed must be an event URN$`, w.errorExplainsSeedShape)
}

// --- Actions ---

func (w *similarWorld) callSimilar(ctx context.Context, args map[string]any) error {
	w.similarArgs = args
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "episodic_similar", Arguments: json.RawMessage(raw),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *similarWorld) seedFor(eventType, name string) (string, error) {
	urn, ok := w.eventIDs[name+"|"+eventType]
	if !ok {
		return "", fmt.Errorf("no seeded %s event for %q", eventType, name)
	}
	return urn, nil
}

func (w *similarWorld) callSimilarSeededNamed(ctx context.Context, eventType, name string) error {
	seed, err := w.seedFor(eventType, name)
	if err != nil {
		return err
	}
	w.seedURN = seed
	return w.callSimilar(ctx, map[string]any{"seed": seed})
}

// callSimilarSeededNamedTwice captures a back-to-back pair of responses. The
// reference projection lands asynchronously, so a pair straddling a partial
// catch-up can genuinely differ — retry the whole pair within the poll budget
// so the determinism assertion tests the contract, not projection timing. A
// still-mismatched pair at deadline is kept for the assertion to report.
func (w *similarWorld) callSimilarSeededNamedTwice(ctx context.Context, eventType, name string) error {
	deadline := time.Now().Add(refsPollTimeout)
	for {
		if err := w.callSimilarSeededNamed(ctx, eventType, name); err != nil {
			return err
		}
		if w.lastResult == nil || w.lastResult.IsError {
			return fmt.Errorf("first similar call failed: %s", w.lastText)
		}
		first := w.lastText
		if err := w.callSimilar(ctx, w.similarArgs); err != nil {
			return err
		}
		w.firstText, w.secondText = first, w.lastText
		if first == w.lastText || time.Now().After(deadline) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (w *similarWorld) callSimilarSeededNamedWithLimit(
	ctx context.Context, eventType, name string, limit int,
) error {
	seed, err := w.seedFor(eventType, name)
	if err != nil {
		return err
	}
	w.seedURN = seed
	return w.callSimilar(ctx, map[string]any{"seed": seed, "limit": limit})
}

func (w *similarWorld) callSimilarSeededLiteral(ctx context.Context, seed string) error {
	w.seedURN = seed
	return w.callSimilar(ctx, map[string]any{"seed": seed})
}

// --- Outcomes ---

func (w *similarWorld) similar() (*similarPage, error) {
	if w.lastErr != nil {
		return nil, fmt.Errorf("episodic_similar protocol error: %v", w.lastErr)
	}
	if w.lastResult == nil || w.lastResult.IsError {
		return nil, fmt.Errorf("episodic_similar failed: %s", w.lastText)
	}
	var page similarPage
	if err := json.Unmarshal([]byte(w.lastText), &page); err != nil {
		return nil, fmt.Errorf("unparseable episodic_similar output: %s", w.lastText)
	}
	return &page, nil
}

// pollSimilar retries a ranked-result assertion while the reference
// projection catches up, re-running the last search between attempts.
func (w *similarWorld) pollSimilar(ctx context.Context, assert func(*similarPage) error) error {
	deadline := time.Now().Add(refsPollTimeout)
	for {
		page, err := w.similar()
		if err == nil {
			err = assert(page)
		}
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		if callErr := w.callSimilar(ctx, w.similarArgs); callErr != nil {
			return callErr
		}
	}
}

func (w *similarWorld) resultsExcludeSeed(ctx context.Context) error {
	return w.pollSimilar(ctx, func(page *similarPage) error {
		for _, e := range page.Results {
			if e.ID == w.seedURN {
				return fmt.Errorf("the seed event %s ranked itself", w.seedURN)
			}
		}
		return nil
	})
}

func (w *similarWorld) resultsListExactly(ctx context.Context, table *godog.Table) error {
	return w.pollSimilar(ctx, func(page *similarPage) error {
		expected := table.Rows[1:]
		if len(page.Results) != len(expected) {
			return fmt.Errorf("got %d results, want %d: %s", len(page.Results), len(expected), w.lastText)
		}
		for i, row := range expected {
			wantName, wantType := row.Cells[0].Value, row.Cells[1].Value
			wantAggregate, ok := w.aggregates[wantName]
			if !ok {
				return fmt.Errorf("no seeded resource named %q", wantName)
			}
			got := page.Results[i]
			if got.AggregateID != wantAggregate || got.EventType != wantType {
				return fmt.Errorf("result %d = %s on %q, want %s on %q",
					i, got.EventType, w.resourceName(got.AggregateID), wantType, wantName)
			}
		}
		return nil
	})
}

func (w *similarWorld) resultsContainCount(ctx context.Context, count int) error {
	return w.pollSimilar(ctx, func(page *similarPage) error {
		if len(page.Results) != count {
			return fmt.Errorf("got %d results, want %d", len(page.Results), count)
		}
		return nil
	})
}

func (w *similarWorld) everyResultCarries(ctx context.Context, table *godog.Table) error {
	return w.pollSimilar(ctx, func(page *similarPage) error {
		if len(page.Results) == 0 {
			return fmt.Errorf("no results to inspect")
		}
		for i, e := range page.Results {
			for _, row := range table.Rows[1:] {
				if err := similarResultCarries(e, row.Cells[0].Value); err != nil {
					return fmt.Errorf("result %d: %w", i, err)
				}
			}
		}
		return nil
	})
}

func similarResultCarries(e similarEvent, field string) error {
	switch field {
	case "referenced resources":
		if len(e.ReferencedResources) == 0 {
			return fmt.Errorf("missing referenced resources")
		}
		return nil
	case "similarity score":
		if e.Similarity == nil {
			return fmt.Errorf("missing similarity score")
		}
		return nil
	default:
		return eventCarries(episodicEvent{
			ID: e.ID, EventType: e.EventType, Timestamp: e.Timestamp,
			AggregateID: e.AggregateID, ResourceType: e.ResourceType, Summary: e.Summary,
		}, field)
	}
}

func (w *similarWorld) resultsOrderedByScore(ctx context.Context) error {
	return w.pollSimilar(ctx, func(page *similarPage) error {
		for i := 1; i < len(page.Results); i++ {
			prev, cur := page.Results[i-1].Similarity, page.Results[i].Similarity
			if prev == nil || cur == nil {
				return fmt.Errorf("result missing a similarity score")
			}
			if *cur > *prev {
				return fmt.Errorf("results not ordered by score: %f before %f", *prev, *cur)
			}
		}
		return nil
	})
}

func (w *similarWorld) bothSearchesIdentical() error {
	var first similarPage
	if err := json.Unmarshal([]byte(w.firstText), &first); err != nil {
		return fmt.Errorf("unparseable first search output: %s", w.firstText)
	}
	var second similarPage
	if err := json.Unmarshal([]byte(w.secondText), &second); err != nil {
		return fmt.Errorf("unparseable second search output: %s", w.secondText)
	}
	if len(first.Results) != len(second.Results) {
		return fmt.Errorf("searches returned %d and %d results", len(first.Results), len(second.Results))
	}
	for i := range first.Results {
		if first.Results[i].ID != second.Results[i].ID {
			return fmt.Errorf("result %d differs between searches: %s vs %s",
				i, first.Results[i].ID, second.Results[i].ID)
		}
	}
	return nil
}

func (w *similarWorld) errorExplainsUnknownSeed() error {
	if !strings.Contains(strings.ToLower(w.errMessage()), "unknown") {
		return fmt.Errorf("error %q does not explain the seed event is unknown", w.errMessage())
	}
	return nil
}

func (w *similarWorld) errorExplainsSeedShape() error {
	if !strings.Contains(strings.ToLower(w.errMessage()), "event urn") {
		return fmt.Errorf("error %q does not explain the seed must be an event URN", w.errMessage())
	}
	return nil
}
