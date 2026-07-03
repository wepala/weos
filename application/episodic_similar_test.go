package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
)

func similarFixture() (*stubEventLog, *recordingRefsRepo, *episodicRecall) {
	at := func(day int) time.Time {
		return time.Date(2026, 6, day, 9, 0, 0, 0, time.UTC)
	}
	project := "urn:project:2p111111111111111111111111"
	stub := &stubEventLog{log: []repositories.EventLogEntry{
		{ID: "seed", AggregateID: "urn:task:2t111111111111111111111111",
			EventType: "Resource.Created", CreatedAt: at(10),
			Payload: map[string]any{"TypeSlug": "task"}},
		// Shares the project reference; far from the seed in time.
		{ID: "shared-ref", AggregateID: "urn:task:2t222222222222222222222222",
			EventType: "Resource.Created", CreatedAt: at(2),
			Payload: map[string]any{"TypeSlug": "task"}},
		// Same type + resource type, adjacent day, but no shared reference.
		{ID: "same-kind", AggregateID: "urn:task:2t333333333333333333333333",
			EventType: "Resource.Created", CreatedAt: at(9),
			Payload: map[string]any{"TypeSlug": "task"}},
		// Same event type only.
		{ID: "same-type", AggregateID: "urn:project:2p444444444444444444444444",
			EventType: "Resource.Created", CreatedAt: at(11),
			Payload: map[string]any{"TypeSlug": "project"}},
	}}
	refs := &recordingRefsRepo{refs: map[string][]string{
		"seed":       {"urn:task:2t111111111111111111111111", project},
		"shared-ref": {"urn:task:2t222222222222222222222222", project},
		"same-kind":  {"urn:task:2t333333333333333333333333"},
		"same-type":  {"urn:project:2p444444444444444444444444"},
	}}
	return stub, refs, &episodicRecall{events: stub, references: refs, now: time.Now}
}

// TestSimilarSharedReferenceDominates pins the weight hierarchy: one shared
// referenced resource outranks same-kind matches at maximal temporal
// proximity, and the seed never ranks itself.
func TestSimilarSharedReferenceDominates(t *testing.T) {
	_, _, recall := similarFixture()
	res, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:seed"})
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	got := make([]string, 0, len(res.Events))
	for _, e := range res.Events {
		got = append(got, strings.TrimPrefix(e.ID, "urn:event:"))
	}
	want := []string{"shared-ref", "same-kind", "same-type"}
	if len(got) != len(want) {
		t.Fatalf("ranked %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranking = %v, want %v", got, want)
		}
	}
	if res.Events[0].Similarity <= res.Events[1].Similarity ||
		res.Events[1].Similarity <= res.Events[2].Similarity {
		t.Errorf("scores not strictly ordered: %+v", res.Events)
	}
}

// TestSimilarDeterministicTieBreak pins the ascending-ID tie-break for fully
// tied candidates.
func TestSimilarDeterministicTieBreak(t *testing.T) {
	at := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	entry := func(id, agg string) repositories.EventLogEntry {
		return repositories.EventLogEntry{ID: id, AggregateID: agg,
			EventType: "Resource.Created", CreatedAt: at,
			Payload: map[string]any{"TypeSlug": "task"}}
	}
	stub := &stubEventLog{log: []repositories.EventLogEntry{
		entry("seed", "urn:task:2t111111111111111111111111"),
		entry("tie-b", "urn:task:2t222222222222222222222222"),
		entry("tie-a", "urn:task:2t333333333333333333333333"),
	}}
	refs := &recordingRefsRepo{refs: map[string][]string{}}
	recall := &episodicRecall{events: stub, references: refs, now: time.Now}

	for range 3 {
		res, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:seed"})
		if err != nil {
			t.Fatalf("Similar: %v", err)
		}
		if len(res.Events) != 2 ||
			res.Events[0].ID != "urn:event:tie-a" || res.Events[1].ID != "urn:event:tie-b" {
			t.Fatalf("tie-break order = %+v, want tie-a before tie-b", res.Events)
		}
	}
}

// TestSimilarSeedOutsideWindowStillScoresSharedRefs pins that a seed older
// than the candidate window still contributes its references to scoring —
// the seed is looked up by ID and its refs fetched alongside the candidates'.
func TestSimilarSeedOutsideWindowStillScoresSharedRefs(t *testing.T) {
	project := "urn:project:2p111111111111111111111111"
	seed := repositories.EventLogEntry{
		ID: "old-seed", AggregateID: "urn:task:2t111111111111111111111111",
		EventType: "Resource.Created", CreatedAt: time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC),
		Payload: map[string]any{"TypeSlug": "task"},
	}
	sharesRef := repositories.EventLogEntry{
		ID: "shares-ref", AggregateID: "urn:note:2n111111111111111111111111",
		EventType: "Resource.Updated", CreatedAt: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		Payload: map[string]any{"TypeSlug": "note"},
	}
	sameTypeOnly := repositories.EventLogEntry{
		ID: "same-type", AggregateID: "urn:task:2t222222222222222222222222",
		EventType: "Resource.Created", CreatedAt: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		Payload: map[string]any{"TypeSlug": "task"},
	}
	stub := &stubEventLog{
		log:    []repositories.EventLogEntry{seed, sharesRef, sameTypeOnly},
		recent: []repositories.EventLogEntry{sharesRef, sameTypeOnly}, // window excludes the seed
	}
	refs := &recordingRefsRepo{refs: map[string][]string{
		"old-seed":   {"urn:task:2t111111111111111111111111", project},
		"shares-ref": {"urn:note:2n111111111111111111111111", project},
	}}
	recall := &episodicRecall{events: stub, references: refs, now: time.Now}

	res, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:old-seed"})
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	if len(res.Events) == 0 || res.Events[0].ID != "urn:event:shares-ref" {
		t.Fatalf("ranking = %+v, want shares-ref first", res.Events)
	}
	if res.Events[0].Similarity < weightSharedReference {
		t.Errorf("shared-ref score %f lost the reference component — the out-of-window seed's refs were not fetched",
			res.Events[0].Similarity)
	}
}

// TestSimilarSharedReferenceOverlapScales pins "overlap scales with the
// number of shared resources": two shared refs beat one shared ref plus
// every other component combined.
func TestSimilarSharedReferenceOverlapScales(t *testing.T) {
	p1 := "urn:project:2p111111111111111111111111"
	p2 := "urn:project:2p222222222222222222222222"
	at := func(day int) time.Time { return time.Date(2026, 6, day, 9, 0, 0, 0, time.UTC) }
	stub := &stubEventLog{log: []repositories.EventLogEntry{
		{ID: "seed", AggregateID: "urn:task:2t111111111111111111111111",
			EventType: "Resource.Created", CreatedAt: at(10),
			Payload: map[string]any{"TypeSlug": "task"}},
		// Shares BOTH projects; everything else differs, and it is far away.
		{ID: "two-refs", AggregateID: "urn:note:2n111111111111111111111111",
			EventType: "Resource.Updated", CreatedAt: at(1),
			Payload: map[string]any{"TypeSlug": "note"}},
		// Shares one project plus same type, same slug, adjacent day.
		{ID: "one-ref-close", AggregateID: "urn:task:2t222222222222222222222222",
			EventType: "Resource.Created", CreatedAt: at(9),
			Payload: map[string]any{"TypeSlug": "task"}},
	}}
	refs := &recordingRefsRepo{refs: map[string][]string{
		"seed":          {"urn:task:2t111111111111111111111111", p1, p2},
		"two-refs":      {"urn:note:2n111111111111111111111111", p1, p2},
		"one-ref-close": {"urn:task:2t222222222222222222222222", p1},
	}}
	recall := &episodicRecall{events: stub, references: refs, now: time.Now}

	res, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:seed"})
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	if len(res.Events) != 2 || res.Events[0].ID != "urn:event:two-refs" {
		t.Fatalf("ranking = %+v, want two shared refs to outrank one-plus-everything", res.Events)
	}
}

// TestSimilarProximityIsSymmetric pins that temporal distance counts the same
// in both directions — a candidate N days after the seed scores exactly like
// one N days before.
func TestSimilarProximityIsSymmetric(t *testing.T) {
	at := func(day int) time.Time { return time.Date(2026, 6, day, 9, 0, 0, 0, time.UTC) }
	entry := func(id, agg string, day int) repositories.EventLogEntry {
		return repositories.EventLogEntry{ID: id, AggregateID: agg,
			EventType: "Resource.Created", CreatedAt: at(day),
			Payload: map[string]any{"TypeSlug": "task"}}
	}
	stub := &stubEventLog{log: []repositories.EventLogEntry{
		entry("seed", "urn:task:2t111111111111111111111111", 10),
		entry("before", "urn:task:2t222222222222222222222222", 7),
		entry("after", "urn:task:2t333333333333333333333333", 13),
	}}
	recall := &episodicRecall{
		events: stub, references: &recordingRefsRepo{refs: map[string][]string{}}, now: time.Now,
	}

	res, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:seed"})
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("got %d results, want 2", len(res.Events))
	}
	if res.Events[0].Similarity != res.Events[1].Similarity {
		t.Errorf("proximity is asymmetric: %f vs %f",
			res.Events[0].Similarity, res.Events[1].Similarity)
	}
	if res.Events[0].ID != "urn:event:after" {
		t.Errorf("tie-break order = %+v, want ascending event ID", res.Events)
	}
}

// TestSimilarExcludesStructurallyUnrelatedEvents pins that temporal proximity
// alone never qualifies a candidate: an event with no shared reference, a
// different event type, and a different resource type is not ranked at all —
// preset/type-registration noise stays out of "similar" results.
func TestSimilarExcludesStructurallyUnrelatedEvents(t *testing.T) {
	stub, _, _ := similarFixture()
	stub.log = append(stub.log, repositories.EventLogEntry{
		ID: "type-noise", AggregateID: "urn:type:task",
		EventType: "ResourceType.Created",
		CreatedAt: time.Date(2026, 6, 10, 9, 0, 1, 0, time.UTC), // seconds from the seed
	})
	refs := &recordingRefsRepo{refs: map[string][]string{
		"seed": {"urn:task:2t111111111111111111111111"},
	}}
	recall := &episodicRecall{events: stub, references: refs, now: time.Now}

	res, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:seed"})
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	for _, e := range res.Events {
		if e.ID == "urn:event:type-noise" {
			t.Errorf("structurally unrelated event ranked (score %f) on proximity alone", e.Similarity)
		}
	}
}

func TestSimilarSeedValidation(t *testing.T) {
	_, _, recall := similarFixture()
	for _, seed := range []string{
		"not-an-event-urn", "urn:task:2t111111111111111111111111", "urn:event:", "", "urn:event:a:b",
	} {
		_, err := recall.Similar(context.Background(), SimilarQuery{Seed: seed})
		if err == nil {
			t.Fatalf("seed %q accepted, want validation error", seed)
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "validation") || !strings.Contains(msg, "event urn") {
			t.Errorf("error %q should mention validation and the event URN shape", err)
		}
	}
}

func TestSimilarUnknownSeedErrors(t *testing.T) {
	_, _, recall := similarFixture()
	_, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:absent"})
	if err == nil {
		t.Fatal("unknown seed accepted, want error")
	}
	if !errors.Is(err, ErrUnknownSeedEvent) {
		t.Errorf("error %v is not ErrUnknownSeedEvent", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown") {
		t.Errorf("error %q should say the seed event is unknown", err)
	}
}

func TestSimilarClampsLimits(t *testing.T) {
	stub, _, _ := similarFixture()
	// Grow the log well past the hard maximum so the over-ask clamp is real.
	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	for i := range 120 {
		stub.log = append(stub.log, repositories.EventLogEntry{
			ID:          "bulk-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			AggregateID: "urn:task:2t555555555555555555555555",
			EventType:   "Resource.Updated",
			CreatedAt:   at.Add(time.Duration(i) * time.Hour),
		})
	}
	refs := &recordingRefsRepo{refs: map[string][]string{}}
	recall := &episodicRecall{events: stub, references: refs, now: time.Now}

	res, err := recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:seed"})
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	if len(res.Events) != defaultEpisodicLimit {
		t.Errorf("default limit returned %d events, want %d", len(res.Events), defaultEpisodicLimit)
	}

	res, err = recall.Similar(context.Background(), SimilarQuery{Seed: "urn:event:seed", Limit: 500})
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	if len(res.Events) != maxEpisodicLimit {
		t.Errorf("over-ask returned %d events, want exactly %d", len(res.Events), maxEpisodicLimit)
	}
}
