// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/identity"
)

const (
	defaultEpisodicLimit = 20
	maxEpisodicLimit     = 100
	maxSummaryRunes      = 120
)

// relativeRange matches the supported relative time grammar: "last 7 days"
// and "7 days ago". Anything fancier belongs to the calling agent, not here —
// retrieval stays deterministic.
var relativeRange = regexp.MustCompile(`^(?:last\s+(\d+)\s+days?|(\d+)\s+days?\s+ago)$`)

// EpisodicQuery is a structured recall request over the event log. From and
// Until accept RFC3339 timestamps or the relative forms "last N days" /
// "N days ago"; empty bounds leave that side of the window open. Anchors are
// resource URNs; an event matches when any anchor is its aggregate or is
// referenced in its payload.
type EpisodicQuery struct {
	From         string
	Until        string
	Anchors      []string
	EventType    string
	ResourceType string
	// Limit caps results (default 20, max 100 — over-asks are capped, not errors).
	Limit  int
	Cursor string
}

// RecalledEvent is the compact episodic result shape: enough to know what
// happened and fetch more, never the full raw payload.
type RecalledEvent struct {
	// ID is the event URN (urn:event:<id>), consistent with the
	// prov:wasDerivedFrom identifiers in the memory preset.
	ID           string `json:"id"`
	EventType    string `json:"eventType"`
	Timestamp    string `json:"timestamp"`
	AggregateID  string `json:"aggregateId"`
	ResourceType string `json:"resourceType,omitempty"`
	Summary      string `json:"summary,omitempty"`
	// ReferencedResources lists the resource URNs the event references — the
	// aggregate itself plus resource URNs in the payload — from the
	// event-reference projection (story #411). Empty until the projection has
	// caught up with the event.
	ReferencedResources []string `json:"referencedResources,omitempty"`
}

// EpisodicRecallResult is one page of recalled events.
type EpisodicRecallResult struct {
	Events  []RecalledEvent
	Cursor  string
	HasMore bool
}

// EpisodicRecall answers scoped, deterministic queries over the event store's
// episodic record: time-windowed, filterable, time-ordered, paginated recall,
// plus structural similar-event search from a seed event. Tools retrieve;
// agents interpret — no learned ranking, no LLM calls.
type EpisodicRecall interface {
	Recall(ctx context.Context, q EpisodicQuery) (*EpisodicRecallResult, error)
	// Similar ranks events by deterministic structural similarity to the
	// seed event (see the weight constants in episodic_similar.go).
	Similar(ctx context.Context, q SimilarQuery) (*SimilarResult, error)
	// EventByURN returns one event's full stored payload — the explicit
	// drill-in complement to the compact shapes above.
	EventByURN(ctx context.Context, urn string) (*FetchedEvent, error)
}

type episodicRecall struct {
	events     repositories.EventLogRepository
	references repositories.EventReferenceRepository
	// now is injectable for tests; production uses time.Now.
	now func() time.Time
}

// NewEpisodicRecall builds the episodic recall service over the event-log
// read surface and the event-reference projection.
func NewEpisodicRecall(
	events repositories.EventLogRepository,
	references repositories.EventReferenceRepository,
) EpisodicRecall {
	return &episodicRecall{events: events, references: references, now: time.Now}
}

func (r *episodicRecall) Recall(ctx context.Context, q EpisodicQuery) (*EpisodicRecallResult, error) {
	filter, err := r.buildFilter(q)
	if err != nil {
		return nil, err
	}
	page, err := r.events.Query(ctx, filter)
	if err != nil {
		return nil, err
	}
	referenced, err := r.referencesFor(ctx, page.Data)
	if err != nil {
		return nil, err
	}
	result := &EpisodicRecallResult{
		Events:  make([]RecalledEvent, 0, len(page.Data)),
		Cursor:  page.Cursor,
		HasMore: page.HasMore,
	}
	for _, e := range page.Data {
		recalled := compactEvent(e)
		recalled.ReferencedResources = referenced[e.ID]
		result.Events = append(result.Events, recalled)
	}
	return result, nil
}

// referencesFor batch-loads the referenced-resource URNs for one page of
// events (one query, no per-event lookups).
func (r *episodicRecall) referencesFor(
	ctx context.Context, events []repositories.EventLogEntry,
) (map[string][]string, error) {
	if r.references == nil || len(events) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return r.references.ForEvents(ctx, ids)
}

func (r *episodicRecall) buildFilter(q EpisodicQuery) (repositories.EventLogFilter, error) {
	now := r.now().UTC()
	from, err := parseTimeBound(q.From, now)
	if err != nil {
		return repositories.EventLogFilter{}, err
	}
	until, err := parseTimeBound(q.Until, now)
	if err != nil {
		return repositories.EventLogFilter{}, err
	}
	if !from.IsZero() && !until.IsZero() && from.After(until) {
		return repositories.EventLogFilter{}, fmt.Errorf(
			"validation error: the time range is invalid: from (%s) must be before until (%s)",
			q.From, q.Until)
	}
	anchors, err := normalizeAnchors(q.Anchors)
	if err != nil {
		return repositories.EventLogFilter{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultEpisodicLimit
	}
	if limit > maxEpisodicLimit {
		limit = maxEpisodicLimit
	}
	return repositories.EventLogFilter{
		From:      from,
		Until:     until,
		Anchors:   anchors,
		EventType: q.EventType,
		TypeSlug:  q.ResourceType,
		Limit:     limit,
		Cursor:    q.Cursor,
	}, nil
}

// normalizeAnchors trims and validates anchor URNs. Anchors must at least
// look like URNs — a malformed anchor is a caller mistake worth surfacing
// loudly, where an unknown-but-well-formed URN is simply an empty result.
func normalizeAnchors(anchors []string) ([]string, error) {
	out := make([]string, 0, len(anchors))
	for _, a := range anchors {
		a = strings.TrimSpace(a)
		if !strings.HasPrefix(a, "urn:") || strings.ContainsFunc(a, unicode.IsSpace) {
			return nil, fmt.Errorf(
				"validation error: the resource URN %q is invalid — anchors must be URNs like urn:task:<id>", a)
		}
		out = append(out, a)
	}
	return out, nil
}

// parseTimeBound resolves one window bound: empty stays open, RFC3339 is
// absolute, and the relative grammar ("last 7 days", "7 days ago") counts
// back from now.
func parseTimeBound(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if m := relativeRange.FindStringSubmatch(strings.ToLower(s)); m != nil {
		digits := m[1]
		if digits == "" {
			digits = m[2]
		}
		days, err := strconv.Atoi(digits)
		if err == nil {
			return now.AddDate(0, 0, -days), nil
		}
	}
	return time.Time{}, fmt.Errorf(
		"validation error: the time range is invalid: %q is neither an RFC3339 timestamp "+
			`nor a relative range like "last 7 days"`, s)
}

// compactEvent maps a stored event to the compact shape. The summary carries
// just the resource type and name pulled from the payload — never the payload
// itself.
func compactEvent(e repositories.EventLogEntry) RecalledEvent {
	slug := payloadString(e.Payload, "TypeSlug")
	if slug == "" {
		slug = identity.ExtractResourceTypeSlug(e.AggregateID)
	}
	return RecalledEvent{
		ID:           "urn:event:" + e.ID,
		EventType:    e.EventType,
		Timestamp:    e.CreatedAt.UTC().Format(time.RFC3339),
		AggregateID:  e.AggregateID,
		ResourceType: slug,
		Summary:      summarizePayload(slug, e.Payload),
	}
}

// summarizePayload builds the deterministic one-line summary: the resource
// type plus the resource's name when the payload data carries one.
func summarizePayload(slug string, payload map[string]any) string {
	name := payloadName(payload)
	summary := slug
	if name != "" {
		summary = strings.TrimSpace(summary + " " + strconv.Quote(name))
	}
	if runes := []rune(summary); len(runes) > maxSummaryRunes {
		summary = string(runes[:maxSummaryRunes])
	}
	return summary
}

// payloadName digs the resource name out of the event payload's Data document
// (top-level "name" first, then the first named @graph node).
func payloadName(payload map[string]any) string {
	data, ok := payload["Data"].(map[string]any)
	if !ok {
		return ""
	}
	if name, ok := data["name"].(string); ok {
		return name
	}
	graph, ok := data["@graph"].([]any)
	if !ok {
		return ""
	}
	for _, node := range graph {
		if m, ok := node.(map[string]any); ok {
			if name, ok := m["name"].(string); ok && name != "" {
				return name
			}
		}
	}
	return ""
}

func payloadString(payload map[string]any, key string) string {
	v, _ := payload[key].(string)
	return v
}
