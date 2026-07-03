package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestEventReferences runs the event-reference projection acceptance
// scenarios (epic #409, story #411): every recalled event reports the
// resources it references, and the projection rebuilds from the event log.
// Unlike the other MCP suites this one runs the background subscribers
// in-process — the projection is asynchronous, so assertions poll.
// Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestEventReferences -v
func TestEventReferences(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "event-references",
		ScenarioInitializer: initEventReferencesScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/event_references.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("event reference acceptance scenarios failed")
	}
}

// refsPollTimeout bounds how long assertions wait for the asynchronous
// event-references subscriber to catch up.
const refsPollTimeout = 5 * time.Second

type eventRefsWorld struct {
	*episodicWorld
}

func initEventReferencesScenario(sc *godog.ScenarioContext) {
	w := &eventRefsWorld{episodicWorld: &episodicWorld{
		mcpWorld:   &mcpWorld{runWorkers: true},
		aggregates: map[string]string{},
		seqs:       map[string]int{},
	}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^the following activity is recorded in the event log:$`, w.activityIsRecorded)
	sc.Step(`^I call episodic_recall for events between "([^"]*)" and "([^"]*)"$`, w.callBetween)
	sc.Step(`^I call episodic_recall for events of type "([^"]*)"$`, w.callEventType)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the recall includes the "([^"]*)" event for "([^"]*)"$`, w.recallIncludesEventFor)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists$`, w.aResourceExists)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists linked to the project "([^"]*)"$`, w.aResourceExistsLinked)
	sc.Step(`^I create a "([^"]*)" named "([^"]*)" linked to the project "([^"]*)"$`, w.aResourceExistsLinked)
	sc.Step(`^the project "([^"]*)" has since been deleted$`, w.projectDeleted)
	sc.Step(`^the event reference projection is rebuilt from the event log$`, w.projectionRebuilt)
	sc.Step(`^the referenced resources for that event are exactly:$`, w.matchedEventRefsExactly)
	sc.Step(`^the referenced resources for that event include "([^"]*)"$`, w.matchedEventRefsInclude)
}

// createTracked creates a resource through the live resource_create tool and
// records its name → URN mapping.
func (w *eventRefsWorld) createTracked(
	ctx context.Context, typeSlug, name string, data map[string]any,
) error {
	if data == nil {
		data = map[string]any{}
	}
	data["name"] = name
	raw, err := json.Marshal(map[string]any{"type_slug": typeSlug, "data": data})
	if err != nil {
		return err
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_create", Arguments: json.RawMessage(raw),
	})
	if err != nil {
		return fmt.Errorf("resource_create protocol error: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("resource_create failed: %s", textOf(res))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil || out.ID == "" {
		return fmt.Errorf("no id in resource_create result: %s", textOf(res))
	}
	w.aggregates[name] = out.ID
	return nil
}

func (w *eventRefsWorld) aResourceExists(ctx context.Context, typeSlug, name string) error {
	return w.createTracked(ctx, typeSlug, name, nil)
}

func (w *eventRefsWorld) aResourceExistsLinked(
	ctx context.Context, typeSlug, name, projectName string,
) error {
	projectURN, ok := w.aggregates[projectName]
	if !ok {
		return fmt.Errorf("no known project named %q", projectName)
	}
	return w.createTracked(ctx, typeSlug, name, map[string]any{"project": projectURN})
}

func (w *eventRefsWorld) projectDeleted(ctx context.Context, name string) error {
	urn, ok := w.aggregates[name]
	if !ok {
		return fmt.Errorf("no known project named %q", name)
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name:      "resource_delete",
		Arguments: json.RawMessage(fmt.Sprintf(`{"id":%q}`, urn)),
	})
	if err != nil {
		return fmt.Errorf("resource_delete protocol error: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("resource_delete failed: %s", textOf(res))
	}
	return nil
}

func (w *eventRefsWorld) projectionRebuilt(ctx context.Context) error {
	if w.manager == nil {
		return fmt.Errorf("worker manager not booted")
	}
	return w.manager.ResetCheckpoint(ctx, "event-references", true)
}

// pollMatchedRefs waits for the asynchronous projection to satisfy check on
// the last matched event's references, re-running the last recall while the
// subscriber catches up. Each poll re-matches from the fresh response — a
// stale match must neither satisfy the check nor shape the timeout error —
// and the freshest failure (include miss or refs mismatch) is what a timeout
// reports.
func (w *eventRefsWorld) pollMatchedRefs(ctx context.Context, check func([]string) error) error {
	deadline := time.Now().Add(refsPollTimeout)
	var lastErr error
	for {
		if w.lastMatched != nil {
			if lastErr = check(w.lastMatched.ReferencedResources); lastErr == nil {
				return nil
			}
		} else if lastErr == nil {
			lastErr = fmt.Errorf("no event matched by a preceding includes step")
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(100 * time.Millisecond)
		if w.lastArgs == nil {
			continue
		}
		if callErr := w.callEpisodic(ctx, w.lastArgs); callErr != nil {
			return callErr
		}
		if w.lastIncludeType != "" {
			w.lastMatched = nil
			if inclErr := w.recallIncludesEventFor(w.lastIncludeType, w.lastIncludeName); inclErr != nil {
				lastErr = inclErr
			}
		}
	}
}

func (w *eventRefsWorld) matchedEventRefsExactly(ctx context.Context, table *godog.Table) error {
	want := make([]string, 0, len(table.Rows)-1)
	for _, row := range table.Rows[1:] {
		urn, ok := w.aggregates[row.Cells[0].Value]
		if !ok {
			return fmt.Errorf("no known resource named %q", row.Cells[0].Value)
		}
		want = append(want, urn)
	}
	sort.Strings(want)
	return w.pollMatchedRefs(ctx, func(got []string) error {
		sorted := append([]string(nil), got...)
		sort.Strings(sorted)
		if len(sorted) != len(want) {
			return fmt.Errorf("referenced resources = %v, want %v", w.named(sorted), w.named(want))
		}
		for i := range want {
			if sorted[i] != want[i] {
				return fmt.Errorf("referenced resources = %v, want %v", w.named(sorted), w.named(want))
			}
		}
		return nil
	})
}

func (w *eventRefsWorld) matchedEventRefsInclude(ctx context.Context, name string) error {
	urn, ok := w.aggregates[name]
	if !ok {
		return fmt.Errorf("no known resource named %q", name)
	}
	return w.pollMatchedRefs(ctx, func(got []string) error {
		for _, ref := range got {
			if ref == urn {
				return nil
			}
		}
		return fmt.Errorf("referenced resources %v do not include %q", w.named(got), name)
	})
}

// named maps URNs back to seeded names for readable failures.
func (w *eventRefsWorld) named(urns []string) string {
	names := make([]string, 0, len(urns))
	for _, urn := range urns {
		names = append(names, w.resourceName(urn))
	}
	return strings.Join(names, ", ")
}
