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
	"sort"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"go.uber.org/fx"
)

// The event-reference projection answers "which events reference resource X"
// (epic #409, story #411). Decision record: a dedicated projection table was
// chosen over queryable JSON payload indexing because payload indexing is
// engine-dependent (json_extract on SQLite vs jsonb operators on Postgres) —
// an epic anti-pattern. The projection is a behavior like any other: derived
// from the event log, replay-idempotent (insert-or-ignore keyed by event ID +
// URN), and rebuildable via `weos worker checkpoint reset event-references
// --truncate`. Later events never remove rows — episodic memory is history,
// so references to since-deleted resources survive.

// EventReferenceGroupParams bundles the reference projection's dependencies.
type EventReferenceGroupParams struct {
	fx.In
	References repositories.EventReferenceRepository
	Logger     entities.Logger
}

// ProvideEventReferenceGroup contributes the "event-references" subscriber
// group. No StartAtHead: the projection's value is completeness over history,
// so a fresh install replays the full log.
func ProvideEventReferenceGroup(p EventReferenceGroupParams) []SubscriberGroup {
	return []SubscriberGroup{{
		Name:     "event-references",
		Handler:  eventReferenceHandler(p.References, p.Logger),
		Truncate: p.References.Clear,
	}}
}

// eventReferenceHandler records, for every event, the resources it references.
func eventReferenceHandler(
	references repositories.EventReferenceRepository, logger entities.Logger,
) subscriptions.Handler {
	return func(ctx context.Context, event domain.EventEnvelope[any]) error {
		if _, ok := event.Payload.(map[string]any); !ok && referenceBearingEvent(event.EventType) {
			// Degrading to aggregate-only must be observable: a payload-shape
			// change would otherwise quietly hollow out the projection.
			logger.Error(ctx, "event references: unexpected event payload type; projecting aggregate only",
				"event", event.ID, "eventType", event.EventType)
		}
		refs := ExtractEventReferences(event)
		if len(refs) == 0 {
			return nil
		}
		return references.SaveForEvent(ctx, event.ID, refs)
	}
}

// referenceBearingEvent reports whether an event type's payload is expected
// to carry resource references.
func referenceBearingEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "Resource.") || strings.HasPrefix(eventType, "Triple.")
}

// ExtractEventReferences returns the resource URNs an event references, in
// deterministic (sorted) order: the aggregate itself plus every resource URN
// in the payload. Extraction is purely structural:
//   - Resource.* events: every URN-shaped string anywhere under the payload's
//     Data document (flat properties, @graph edge nodes, arrays alike);
//   - Triple.* events: the subject and object terms;
//   - anything else: just the aggregate.
//
// A "resource URN" follows the isGatedResourceURN precedent — plain resources
// (urn:<slug>:<ksuid>) plus person and organization URNs. Event URNs
// (urn:event:...) are excluded so provenance fields like wasDerivedFrom do not
// read as resource references; type/theme/site URNs are configuration, not
// data, and never match.
func ExtractEventReferences(event domain.EventEnvelope[any]) []string {
	seen := map[string]bool{}
	addReference(seen, event.AggregateID)
	if payload, ok := event.Payload.(map[string]any); ok {
		if data, ok := payload["Data"]; ok {
			collectReferenceURNs(data, seen)
		}
		for _, key := range []string{"subject", "object"} {
			if term, ok := payload[key].(string); ok {
				addReference(seen, term)
			}
		}
	}
	refs := make([]string, 0, len(seen))
	for urn := range seen {
		refs = append(refs, urn)
	}
	sort.Strings(refs)
	return refs
}

// collectReferenceURNs walks an arbitrary JSON value collecting resource URNs.
func collectReferenceURNs(value any, seen map[string]bool) {
	switch v := value.(type) {
	case string:
		addReference(seen, v)
	case map[string]any:
		for _, nested := range v {
			collectReferenceURNs(nested, seen)
		}
	case []any:
		for _, item := range v {
			collectReferenceURNs(item, seen)
		}
	}
}

func addReference(seen map[string]bool, candidate string) {
	if strings.HasPrefix(candidate, "urn:event:") {
		return
	}
	if isGatedResourceURN(candidate) {
		seen[candidate] = true
	}
}
