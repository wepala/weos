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
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

func TestProvideDisplayValuesGroup_ReturnsGroup(t *testing.T) {
	t.Parallel()
	groups := ProvideDisplayValuesGroup(DisplayValuesGroupParams{
		EventStore: &fakeEventStore{},
		ProjMgr:    &stubProjMgr{},
		Logger:     noopLogger{},
	})
	if len(groups) != 1 || groups[0].Name != "display-values" || groups[0].Handler == nil {
		t.Fatalf("expected one 'display-values' group with a handler, got %+v", groups)
	}
}

// TestDisplayValuesHandler_PropagatesOnPublished proves the moved
// reverse-reference propagation runs from the subscriber: a published company
// updates the denormalized display column on every type that references it.
func TestDisplayValuesHandler_PropagatesOnPublished(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{reverseRefs: map[string][]repositories.ReverseReference{
		"company": {{
			ReferencingTypeSlug: "employee",
			FKColumn:            "company_id",
			DisplayColumn:       "company_id_display",
			DisplayProperty:     "name",
		}},
	}}
	es := &fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{
		"tx-1": {{
			AggregateID: "urn:company:123",
			EventType:   "Resource.Created",
			SequenceNo:  1,
			Payload: map[string]any{
				"TypeSlug": "company",
				"Data":     map[string]any{"@id": "urn:company:123", "name": "Acme"},
			},
		}},
	}}
	h := displayValuesHandler(es, pm, noopLogger{})

	env := domain.EventEnvelope[any]{
		AggregateID:   "urn:company:123",
		EventType:     "Resource.Published",
		TransactionID: "tx-1",
		SequenceNo:    1,
	}
	if err := h(context.Background(), env); err != nil {
		t.Fatalf("display handler: %v", err)
	}

	if len(pm.fkUpdates) != 1 {
		t.Fatalf("expected one display-column update, got %d", len(pm.fkUpdates))
	}
	got := pm.fkUpdates[0]
	if got.typeSlug != "employee" || got.fkColumn != "company_id" ||
		got.fkValue != "urn:company:123" || got.displayCol != "company_id_display" || got.displayVal != "Acme" {
		t.Fatalf("unexpected display update: %+v", got)
	}
}

// TestDisplayValuesHandler_PropagatesUpdateError proves a propagation failure
// surfaces from the handler so the subscriber retries/parks it, rather than
// silently advancing the checkpoint and leaving display columns stale.
func TestDisplayValuesHandler_PropagatesUpdateError(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{
		reverseRefs: map[string][]repositories.ReverseReference{
			"company": {{ReferencingTypeSlug: "employee", FKColumn: "company_id", DisplayColumn: "company_id_display", DisplayProperty: "name"}},
		},
		updateErr: context.DeadlineExceeded,
	}
	es := &fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{
		"tx-1": {{
			AggregateID: "urn:company:123",
			EventType:   "Resource.Created",
			SequenceNo:  1,
			Payload:     map[string]any{"TypeSlug": "company", "Data": map[string]any{"@id": "urn:company:123", "name": "Acme"}},
		}},
	}}
	h := displayValuesHandler(es, pm, noopLogger{})

	env := domain.EventEnvelope[any]{
		AggregateID: "urn:company:123", EventType: "Resource.Published", TransactionID: "tx-1", SequenceNo: 1,
	}
	if err := h(context.Background(), env); err == nil {
		t.Fatalf("expected display propagation failure to surface for retry/parking")
	}
}

func TestDisplayValuesHandler_IgnoresNonPublishedAndDeletes(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{reverseRefs: map[string][]repositories.ReverseReference{
		"company": {{ReferencingTypeSlug: "employee", FKColumn: "company_id", DisplayColumn: "company_id_display", DisplayProperty: "name"}},
	}}
	es := &fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{
		"tx-del": {{AggregateID: "urn:company:123", EventType: "Resource.Deleted", SequenceNo: 2, Payload: map[string]any{}}},
	}}
	h := displayValuesHandler(es, pm, noopLogger{})

	// A non-Published event is ignored entirely.
	if err := h(context.Background(), domain.EventEnvelope[any]{EventType: "Triple.Created"}); err != nil {
		t.Fatalf("non-published: %v", err)
	}
	// A published delete propagates nothing.
	del := domain.EventEnvelope[any]{
		AggregateID: "urn:company:123", EventType: "Resource.Published", TransactionID: "tx-del", SequenceNo: 2,
	}
	if err := h(context.Background(), del); err != nil {
		t.Fatalf("published delete: %v", err)
	}
	if len(pm.fkUpdates) != 0 {
		t.Fatalf("expected no display updates for ignored/deleted events, got %+v", pm.fkUpdates)
	}
}
