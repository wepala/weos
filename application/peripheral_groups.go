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
	"errors"
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"go.uber.org/fx"
)

// This file moves the peripheral, expensive event handlers off the synchronous
// write path and onto background checkpointed subscriber groups (epic #365,
// story #370). Each group has its own checkpoint, so a failure or lag in one
// never stalls the others or the user's request. The critical resource /
// resource-type / triple GORM projections stay synchronous (see
// event_handlers.go) because the SPA depends on read-your-writes after POST.

// OxigraphGroupParams bundles the Oxigraph projector group's dependencies.
type OxigraphGroupParams struct {
	fx.In
	EventStore domain.EventStore
	Store      repositories.KnowledgeGraphStore
	TypeRepo   repositories.ResourceTypeRepository
	Logger     entities.Logger
}

// ProvideOxigraphGroup contributes the "oxigraph" subscriber group when the
// knowledge-graph store is configured, and nothing otherwise (so leaving
// Oxigraph unconfigured costs no subscriber). It returns a slice so it can
// contribute zero or one group into the flattened "subscriber_groups" value
// group (see AsSubscriberGroups).
func ProvideOxigraphGroup(p OxigraphGroupParams) []SubscriberGroup {
	if !p.Store.Active() {
		p.Logger.Debug(context.Background(),
			"knowledge graph: store inactive, oxigraph subscriber not registered")
		return nil
	}
	return []SubscriberGroup{{
		Name:    "oxigraph",
		Handler: oxigraphHandler(p.EventStore, p.Store, p.TypeRepo, p.Logger),
		// `worker checkpoint reset oxigraph --truncate` (and OXIGRAPH_REBUILD)
		// clear the whole graph so the replay rebuilds it from empty.
		Truncate: p.Store.Clear,
	}}
}

// oxigraphHandler projects resource/triple/type events into the knowledge graph.
// The subscriber feeds it every event in the feed, so it filters by type and
// ignores the rest. Returning an error retries (and eventually parks) the
// event; a user request is never affected because this runs in the background.
func oxigraphHandler(
	eventStore domain.EventStore,
	store repositories.KnowledgeGraphStore,
	typeRepo repositories.ResourceTypeRepository,
	logger entities.Logger,
) subscriptions.Handler {
	return func(ctx context.Context, event domain.EventEnvelope[any]) error {
		switch event.EventType {
		case "Triple.Created":
			tr, ok := tripleFromPayload(event.Payload)
			if !ok {
				logger.Error(ctx, "kg dropping unprojectable Triple.Created payload",
					"aggregateID", event.AggregateID, "payloadType", fmt.Sprintf("%T", event.Payload))
				return nil
			}
			return store.AddTriples(ctx, []repositories.Triple{tr})
		case "Triple.Deleted":
			tr, ok := tripleFromPayload(event.Payload)
			if !ok {
				logger.Error(ctx, "kg dropping unprojectable Triple.Deleted payload",
					"aggregateID", event.AggregateID, "payloadType", fmt.Sprintf("%T", event.Payload))
				return nil
			}
			return store.RemoveTriples(ctx, []repositories.Triple{tr})
		case "Resource.Published":
			return projectResourcePublished(ctx, event, eventStore, store, logger)
		case "Resource.Deleted":
			return store.RemoveSubject(ctx, event.AggregateID)
		case "ResourceType.Created", "ResourceType.Updated":
			// Read the type fresh rather than parsing the (untyped, map) payload:
			// the aggregate id is the type's URN, and the repository already
			// holds the canonical name/slug/context.
			rt, err := typeRepo.FindByID(ctx, event.AggregateID)
			if err != nil {
				if errors.Is(err, repositories.ErrNotFound) {
					return nil // type since deleted; nothing to project
				}
				return err
			}
			return projectResourceTypeOntology(ctx, rt.Name(), rt.Slug(), rt.Context(), store, logger)
		default:
			return nil
		}
	}
}

// tripleFromPayload extracts a triple from an event payload. Background
// subscribers receive payloads deserialized from the store as map[string]any
// (BasicTripleEvent's json tags are lowercase); the typed cases cover callers
// that dispatch the original struct (e.g. tests).
func tripleFromPayload(payload any) (repositories.Triple, bool) {
	switch p := payload.(type) {
	case map[string]any:
		subject, _ := p["subject"].(string)
		predicate, _ := p["predicate"].(string)
		object, _ := p["object"].(string)
		if subject == "" || predicate == "" {
			return repositories.Triple{}, false
		}
		return repositories.Triple{Subject: subject, Predicate: predicate, Object: object}, true
	case entities.TripleCreated:
		return repositories.Triple{Subject: p.Subject, Predicate: p.Predicate, Object: p.Object}, true
	case entities.TripleDeleted:
		return repositories.Triple{Subject: p.Subject, Predicate: p.Predicate, Object: p.Object}, true
	default:
		return repositories.Triple{}, false
	}
}

// DisplayValuesGroupParams bundles the display-value propagation group's
// dependencies.
type DisplayValuesGroupParams struct {
	fx.In
	EventStore domain.EventStore
	ProjMgr    repositories.ProjectionManager
	Logger     entities.Logger
}

// ProvideDisplayValuesGroup contributes the "display-values" subscriber group,
// which denormalizes a resource's display label into the rows of other types
// that reference it. This reverse-reference propagation used to run inside the
// synchronous Resource.Published projection; moving it to a background group
// keeps the user's write fast and decouples it from the critical read model.
// The forward direction (a new row's own display columns) stays synchronous in
// the resource repository.
func ProvideDisplayValuesGroup(p DisplayValuesGroupParams) []SubscriberGroup {
	return []SubscriberGroup{{
		Name:    "display-values",
		Handler: displayValuesHandler(p.EventStore, p.ProjMgr, p.Logger),
	}}
}

func displayValuesHandler(
	eventStore domain.EventStore,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) subscriptions.Handler {
	return func(ctx context.Context, event domain.EventEnvelope[any]) error {
		if event.EventType != "Resource.Published" || event.TransactionID == "" {
			return nil
		}
		txEvents, err := eventStore.GetEventsByTransactionID(ctx, event.TransactionID)
		if err != nil {
			return err
		}
		state := buildStateFromTransaction(ctx, txEvents, event.AggregateID, event.SequenceNo, logger)
		if state.IsDelete || state.Data == nil {
			return nil
		}
		return propagateDisplayValues(ctx, event.AggregateID, state.Data, projMgr, logger)
	}
}
