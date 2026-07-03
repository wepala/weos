package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
)

// TestEntityAnchoredRecall runs the entity-anchored recall acceptance
// scenarios (epic #409, story #412): episodic_recall anchored on one or more
// resource URNs returns the events involving them, via the event-reference
// projection. Anchored reads are eventually consistent, so this suite runs
// the background subscribers in-process and polls, like event_references.
// Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestEntityAnchoredRecall -v
func TestEntityAnchoredRecall(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "entity-anchored-recall",
		ScenarioInitializer: initEntityAnchoredRecallScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/entity_anchored_recall.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("entity-anchored recall acceptance scenarios failed")
	}
}

type anchoredWorld struct {
	*eventRefsWorld
}

func initEntityAnchoredRecallScenario(sc *godog.ScenarioContext) {
	w := &anchoredWorld{eventRefsWorld: &eventRefsWorld{episodicWorld: &episodicWorld{
		mcpWorld:   &mcpWorld{runWorkers: true},
		aggregates: map[string]string{},
		seqs:       map[string]int{},
	}}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists$`, w.aResourceExists)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists linked to the project "([^"]*)"$`, w.aResourceExistsLinked)
	sc.Step(`^the following activity is recorded in the event log:$`, w.activityIsRecorded)
	sc.Step(`^(\d+) "([^"]*)" resources linked to "([^"]*)" were created on consecutive days starting "([^"]*)"$`,
		w.linkedResourcesCreatedOnConsecutiveDays)
	sc.Step(`^the project "([^"]*)" has since been deleted$`, w.projectDeleted)
	sc.Step(`^I call episodic_recall for events about the resource named "([^"]*)"$`, w.callAboutResource)
	sc.Step(`^I call episodic_recall for events about the resources named "([^"]*)" and "([^"]*)"$`,
		w.callAboutResources)
	sc.Step(`^I call episodic_recall for events about the resource "([^"]*)"$`, w.callAboutLiteral)
	sc.Step(`^I call episodic_recall with the filters:$`, w.callWithFilters)
	sc.Step(`^I have recalled the first page of events about "([^"]*)"$`, w.recallFirstPageAbout)
	sc.Step(`^I call episodic_recall with the cursor from the first page$`, w.callWithCursor)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the recall includes the "([^"]*)" event for "([^"]*)"$`, w.recallIncludesEventForPolled)
	sc.Step(`^the recall does not include an event for "([^"]*)"$`, w.recallExcludesEventsFor)
	sc.Step(`^the recall returns exactly these events in order:$`, w.recallReturnsExactlyPolled)
	sc.Step(`^the recall returns (\d+) events$`, w.recallReturnsCountPolled)
	sc.Step(`^the recall returns no events$`, w.recallReturnsNone)
	sc.Step(`^the recall reports more events are available$`, w.recallReportsMore)
	sc.Step(`^the recall reports no more events are available$`, w.recallReportsNoMore)
	sc.Step(`^the recall provides a cursor for the next page$`, w.recallProvidesCursor)
	sc.Step(`^no event from the first page is repeated$`, w.noFirstPageEventRepeated)
	sc.Step(`^the referenced resources for that event include "([^"]*)"$`, w.matchedEventRefsInclude)
	sc.Step(`^the error explains the resource URN is invalid$`, w.errorExplainsInvalidAnchor)
}

// --- Actions ---

func (w *anchoredWorld) callAboutResources(ctx context.Context, first, second string) error {
	anchors := make([]string, 0, 2)
	for _, name := range []string{first, second} {
		urn, ok := w.aggregates[name]
		if !ok {
			return fmt.Errorf("no seeded resource named %q", name)
		}
		anchors = append(anchors, urn)
	}
	return w.callEpisodic(ctx, map[string]any{"about": anchors})
}

func (w *anchoredWorld) callAboutLiteral(ctx context.Context, urn string) error {
	return w.callEpisodic(ctx, map[string]any{"about": []string{urn}})
}

// linkedResourcesCreatedOnConsecutiveDays seeds count resources whose payload
// references the named project, one day apart.
func (w *anchoredWorld) linkedResourcesCreatedOnConsecutiveDays(
	ctx context.Context, count int, typeSlug, projectName, start string,
) error {
	projectURN, ok := w.aggregates[projectName]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", projectName)
	}
	startAt, err := parseOccurredAt(start)
	if err != nil {
		return err
	}
	for i := range count {
		name := fmt.Sprintf("%s %03d", typeSlug, i+1)
		if err := w.seedEvent(ctx, typeSlug, name, map[string]any{"project": projectURN},
			"Resource.Created", startAt.AddDate(0, 0, i)); err != nil {
			return err
		}
	}
	return nil
}

// recallFirstPageAbout waits for the anchored view to stabilize (the
// projection is asynchronous), then pins page one of the anchored query.
func (w *anchoredWorld) recallFirstPageAbout(ctx context.Context, name string) error {
	urn, ok := w.aggregates[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	// The page-one capture is only meaningful once the projection has caught
	// up past a full default page — this step exists for the cursor
	// scenarios, which seed >20 anchored events; reusing it with a smaller
	// fixture would never stabilize and error out below. Never proceed with a
	// partial page: the downstream failures ("no cursor captured") would
	// point away from the real cause.
	probe := map[string]any{"about": []string{urn}, "limit": 100}
	deadline := time.Now().Add(refsPollTimeout)
	prev, stable := -1, 0
	for stable < 2 {
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"anchored projection did not stabilize past 20 events within %s (last count %d)",
				refsPollTimeout, prev)
		}
		if err := w.callEpisodic(ctx, probe); err != nil {
			return err
		}
		page, err := w.page()
		if err != nil {
			return fmt.Errorf("anchored probe: %w", err)
		}
		if n := len(page.Events); n == prev && n > 20 {
			stable++
		} else {
			stable = 0
			prev = len(page.Events)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err := w.callAboutResource(ctx, name); err != nil {
		return err
	}
	return w.captureFirstPage()
}

// --- Outcomes ---

// pollAssert retries a page-level assertion while the reference projection
// catches up, re-running the last recall between attempts.
func (w *anchoredWorld) pollAssert(ctx context.Context, assert func() error) error {
	deadline := time.Now().Add(refsPollTimeout)
	for {
		err := assert()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		if w.lastArgs == nil {
			continue
		}
		if callErr := w.callEpisodic(ctx, w.lastArgs); callErr != nil {
			return callErr
		}
	}
}

func (w *anchoredWorld) recallIncludesEventForPolled(
	ctx context.Context, eventType, name string,
) error {
	return w.pollAssert(ctx, func() error { return w.recallIncludesEventFor(eventType, name) })
}

func (w *anchoredWorld) recallReturnsExactlyPolled(ctx context.Context, table *godog.Table) error {
	return w.pollAssert(ctx, func() error { return w.recallReturnsExactly(table) })
}

func (w *anchoredWorld) recallReturnsCountPolled(ctx context.Context, count int) error {
	return w.pollAssert(ctx, func() error { return w.recallReturnsCount(count) })
}

func (w *anchoredWorld) errorExplainsInvalidAnchor() error {
	msg := strings.ToLower(w.errMessage())
	if !strings.Contains(msg, "resource urn") || !strings.Contains(msg, "invalid") {
		return fmt.Errorf("error %q does not explain the resource URN is invalid", w.errMessage())
	}
	return nil
}
