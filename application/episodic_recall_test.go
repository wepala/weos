package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
)

type stubEventLog struct {
	lastFilter repositories.EventLogFilter
	response   *repositories.PaginatedResponse[repositories.EventLogEntry]
}

func (s *stubEventLog) Query(
	_ context.Context, filter repositories.EventLogFilter,
) (*repositories.PaginatedResponse[repositories.EventLogEntry], error) {
	s.lastFilter = filter
	if s.response != nil {
		return s.response, nil
	}
	return &repositories.PaginatedResponse[repositories.EventLogEntry]{}, nil
}

func fixedEpisodicRecall(stub *stubEventLog, now time.Time) *episodicRecall {
	return &episodicRecall{events: stub, now: func() time.Time { return now }}
}

func TestParseTimeBound(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		in    string
		want  time.Time
		fails bool
	}{
		{name: "empty stays open", in: "", want: time.Time{}},
		{name: "absolute RFC3339", in: "2026-06-15T00:00:00Z",
			want: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)},
		{name: "last N days", in: "last 7 days", want: now.AddDate(0, 0, -7)},
		{name: "N days ago", in: "3 days ago", want: now.AddDate(0, 0, -3)},
		{name: "singular day", in: "1 day ago", want: now.AddDate(0, 0, -1)},
		{name: "case insensitive", in: "Last 2 Days", want: now.AddDate(0, 0, -2)},
		{name: "garbage", in: "not-a-timestamp", fails: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTimeBound(tc.in, now)
			if tc.fails {
				if err == nil {
					t.Fatalf("parseTimeBound(%q) succeeded, want error", tc.in)
				}
				if !strings.Contains(err.Error(), "validation") {
					t.Errorf("error %q does not mention validation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTimeBound(%q): %v", tc.in, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("parseTimeBound(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestEpisodicRecallRejectsReversedRange(t *testing.T) {
	recall := fixedEpisodicRecall(&stubEventLog{}, time.Now().UTC())
	_, err := recall.Recall(context.Background(), EpisodicQuery{
		From: "2026-06-30T00:00:00Z", Until: "2026-06-01T00:00:00Z",
	})
	if err == nil {
		t.Fatal("reversed range succeeded, want validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "validation") || !strings.Contains(msg, "time range") {
		t.Errorf("error %q should mention validation and the time range", msg)
	}
}

func TestEpisodicRecallClampsLimits(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{name: "default", in: 0, want: defaultEpisodicLimit},
		{name: "negative", in: -5, want: defaultEpisodicLimit},
		{name: "passthrough", in: 42, want: 42},
		{name: "capped", in: 500, want: maxEpisodicLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubEventLog{}
			recall := fixedEpisodicRecall(stub, time.Now().UTC())
			if _, err := recall.Recall(context.Background(), EpisodicQuery{Limit: tc.in}); err != nil {
				t.Fatalf("Recall: %v", err)
			}
			if stub.lastFilter.Limit != tc.want {
				t.Errorf("limit %d clamped to %d, want %d", tc.in, stub.lastFilter.Limit, tc.want)
			}
		})
	}
}

func TestEpisodicRecallCompactShape(t *testing.T) {
	occurred := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	stub := &stubEventLog{response: &repositories.PaginatedResponse[repositories.EventLogEntry]{
		Data: []repositories.EventLogEntry{{
			ID:          "2z000000000000000000000000",
			AggregateID: "urn:task:2z111111111111111111111111",
			EventType:   "Resource.Created",
			CreatedAt:   occurred,
			Payload: map[string]any{
				"TypeSlug": "task",
				"Data": map[string]any{
					"name":        "Chase overdue invoices",
					"description": "Match receipts to the ledger",
				},
			},
		}},
		Cursor:  "next-page",
		HasMore: true,
	}}
	recall := fixedEpisodicRecall(stub, time.Now().UTC())

	res, err := recall.Recall(context.Background(), EpisodicQuery{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if res.Cursor != "next-page" || !res.HasMore {
		t.Errorf("pagination not propagated: cursor=%q hasMore=%v", res.Cursor, res.HasMore)
	}
	if len(res.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(res.Events))
	}
	e := res.Events[0]
	if e.ID != "urn:event:2z000000000000000000000000" {
		t.Errorf("ID = %q, want the urn:event: form", e.ID)
	}
	if e.Timestamp != "2026-06-20T09:00:00Z" {
		t.Errorf("Timestamp = %q, want RFC3339 of the stored created_at", e.Timestamp)
	}
	if e.ResourceType != "task" {
		t.Errorf("ResourceType = %q, want task", e.ResourceType)
	}
	if !strings.Contains(e.Summary, "Chase overdue invoices") {
		t.Errorf("Summary %q should carry the resource name", e.Summary)
	}
	if strings.Contains(e.Summary, "Match receipts to the ledger") {
		t.Errorf("Summary %q must not leak the payload description", e.Summary)
	}
}

func TestSummarizePayloadGraphFallback(t *testing.T) {
	payload := map[string]any{
		"Data": map[string]any{
			"@graph": []any{
				map[string]any{"@id": "urn:task:x"},
				map[string]any{"@id": "urn:task:y", "name": "Draft Q3 invoice summary"},
			},
		},
	}
	got := summarizePayload("task", payload)
	if !strings.Contains(got, "Draft Q3 invoice summary") {
		t.Errorf("summary %q should pull the first named @graph node", got)
	}
}

func TestSummarizePayloadTruncates(t *testing.T) {
	payload := map[string]any{
		"Data": map[string]any{"name": strings.Repeat("x", 300)},
	}
	got := summarizePayload("task", payload)
	if runes := []rune(got); len(runes) > maxSummaryRunes {
		t.Errorf("summary is %d runes, want at most %d", len(runes), maxSummaryRunes)
	}
}
