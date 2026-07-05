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
	"sync"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/identity"

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
	Stores     repositories.KnowledgeGraphStores
	TypeRepo   repositories.ResourceTypeRepository
	Logger     entities.Logger
}

// ProvideOxigraphGroup contributes the "oxigraph" subscriber group when the
// knowledge-graph store is configured, and nothing otherwise (so leaving
// Oxigraph unconfigured costs no subscriber). It returns a slice so it can
// contribute zero or one group into the flattened "subscriber_groups" value
// group (see AsSubscriberGroups).
func ProvideOxigraphGroup(p OxigraphGroupParams) []SubscriberGroup {
	if !p.Stores.Active() {
		p.Logger.Debug(context.Background(),
			"knowledge graph: store inactive, oxigraph subscriber not registered")
		return nil
	}
	proj := newOxigraphProjector(p.EventStore, p.Stores, p.TypeRepo, p.Logger)
	return []SubscriberGroup{{
		Name:    "oxigraph",
		Handler: proj.handle,
		// `worker checkpoint reset oxigraph --truncate` (and OXIGRAPH_REBUILD)
		// clear the graph(s) so the replay rebuilds from empty. In per-account
		// mode this wipes every account store; we also drop the projector's
		// lazy-ontology cache so the replay re-projects each account's classes.
		Truncate: proj.truncate,
	}}
}

// oxigraphProjector projects resource/triple/type events into the knowledge
// graph, routing each event to the owning account's store in per-account mode
// (and to the one process store otherwise). One instance backs the single
// "oxigraph" subscriber; the subscriber calls handle sequentially per event, so
// the only shared mutable state — the lazy-ontology cache — is guarded by a
// mutex for the rare concurrent Truncate.
type oxigraphProjector struct {
	eventStore domain.EventStore
	stores     repositories.KnowledgeGraphStores
	typeRepo   repositories.ResourceTypeRepository
	logger     entities.Logger

	mu sync.Mutex
	// ontologySeen memoizes which (account, type) ontologies have been projected
	// into an account store, so per-account lazy ontology projection runs once
	// per type per account rather than on every instance publish. Key:
	// accountID + "\x00" + typeSlug.
	ontologySeen map[string]bool
}

func newOxigraphProjector(
	eventStore domain.EventStore,
	stores repositories.KnowledgeGraphStores,
	typeRepo repositories.ResourceTypeRepository,
	logger entities.Logger,
) *oxigraphProjector {
	return &oxigraphProjector{
		eventStore:   eventStore,
		stores:       stores,
		typeRepo:     typeRepo,
		logger:       logger,
		ontologySeen: make(map[string]bool),
	}
}

// errAccountlessWrite marks an event that, in per-account mode, has an
// identified creator but no resolvable account. Projecting it would divert the
// writer's data into the shared local graph (readable by any local caller), so
// the handler skips it loudly instead — retrying would never help since the
// event permanently carries no account. The genuinely ownerless case (no
// creator at all — a system/stdio write) is NOT this; it routes to the local
// graph as intended.
var errAccountlessWrite = errors.New("kg: per-account event has a creator but no account")

// handle is the subscriber handler. The subscriber feeds it every event in the
// feed, so it filters by type and ignores the rest. Returning an error retries
// (and eventually parks) the event; a user request is never affected because
// this runs in the background. An errAccountlessWrite is turned into a loud skip
// (logged, not parked) so an accountless authenticated write never silently
// lands in the shared local graph.
func (p *oxigraphProjector) handle(ctx context.Context, event domain.EventEnvelope[any]) error {
	err := p.dispatch(ctx, event)
	if errors.Is(err, errAccountlessWrite) {
		p.logger.Error(ctx, "kg skipping per-account event with a creator but no account "+
			"(not routed to the shared local graph)",
			"aggregateID", event.AggregateID, "eventType", event.EventType)
		return nil
	}
	return err
}

func (p *oxigraphProjector) dispatch(ctx context.Context, event domain.EventEnvelope[any]) error {
	switch event.EventType {
	case "Triple.Created":
		return p.projectTriple(ctx, event, true)
	case "Triple.Deleted":
		return p.projectTriple(ctx, event, false)
	case "Resource.Published":
		return p.projectPublished(ctx, event)
	case "Resource.Deleted":
		return p.projectDeleted(ctx, event)
	case "ResourceType.Created", "ResourceType.Updated":
		return p.projectType(ctx, event)
	default:
		return nil
	}
}

// truncate backs SubscriberGroup.Truncate: clear the graph(s) and forget the
// lazy-ontology cache so a rebuild re-projects each account's classes into its
// freshly-emptied store.
func (p *oxigraphProjector) truncate(ctx context.Context) error {
	p.mu.Lock()
	p.ontologySeen = make(map[string]bool)
	p.mu.Unlock()
	return p.stores.Truncate(ctx)
}

func (p *oxigraphProjector) projectTriple(
	ctx context.Context, event domain.EventEnvelope[any], add bool,
) error {
	tr, ok := tripleFromPayload(event.Payload)
	if !ok {
		verb := "Triple.Created"
		if !add {
			verb = "Triple.Deleted"
		}
		p.logger.Error(ctx, "kg dropping unprojectable "+verb+" payload",
			"aggregateID", event.AggregateID, "payloadType", fmt.Sprintf("%T", event.Payload))
		return nil
	}
	store, err := p.storeForEvent(ctx, event)
	if err != nil {
		return err
	}
	if add {
		return store.AddTriples(ctx, []repositories.Triple{tr})
	}
	return store.RemoveTriples(ctx, []repositories.Triple{tr})
}

func (p *oxigraphProjector) projectPublished(
	ctx context.Context, event domain.EventEnvelope[any],
) error {
	account, err := p.accountForEvent(ctx, event)
	if err != nil {
		return err
	}
	store, err := p.stores.ForAccount(ctx, account)
	if err != nil {
		return err
	}
	// Per-account lazy ontology: ensure this resource's type classes are present
	// in the account store before its instance, so kg_list_classes/describe_class
	// answer from the account's own graph. Single-tenant mode projects ontology
	// eagerly on ResourceType events (see projectType), so this is a no-op there.
	if p.stores.PerAccount() {
		if err := p.ensureAccountOntology(ctx, store, account, event.AggregateID); err != nil {
			return err
		}
	}
	return projectResourcePublished(ctx, event, p.eventStore, store, p.logger)
}

func (p *oxigraphProjector) projectDeleted(
	ctx context.Context, event domain.EventEnvelope[any],
) error {
	store, err := p.storeForEvent(ctx, event)
	if err != nil {
		return err
	}
	return store.RemoveSubject(ctx, event.AggregateID)
}

// projectType projects a resource type's ontology. Type events are global (a
// ResourceType has no account), so in per-account mode there is nothing to route
// them to — the ontology lands in an account store lazily when that account
// first publishes an instance (see projectPublished). Single-tenant mode
// projects it into the one store, as before.
func (p *oxigraphProjector) projectType(
	ctx context.Context, event domain.EventEnvelope[any],
) error {
	if p.stores.PerAccount() {
		return nil
	}
	rt, err := p.typeRepo.FindByID(ctx, event.AggregateID)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil // type since deleted; nothing to project
		}
		return err
	}
	store, err := p.stores.ForAccount(ctx, "")
	if err != nil {
		return err
	}
	return projectResourceTypeOntology(ctx, rt.Name(), rt.Slug(), rt.Context(), store, p.logger)
}

// storeForEvent resolves the store an event should be projected into. In
// single-tenant mode it's the one store (account ignored). In per-account mode
// it's the owning account's store, resolved by accountForEvent.
func (p *oxigraphProjector) storeForEvent(
	ctx context.Context, event domain.EventEnvelope[any],
) (repositories.KnowledgeGraphStore, error) {
	account, err := p.accountForEvent(ctx, event)
	if err != nil {
		return nil, err
	}
	return p.stores.ForAccount(ctx, account)
}

// accountForEvent returns the account an event should be routed to. Single-tenant
// mode needs no account (""). Per-account mode reads AccountID off the event
// payload (now carried on every resource/triple event); when it's empty it
// recovers the owner from the aggregate's Resource.Created (events persisted
// before the per-event AccountID field) and distinguishes three cases:
//
//   - a recovered account -> that account;
//   - a creator but no account (an authenticated write whose caller has no active
//     account) -> errAccountlessWrite, so the handler skips it rather than
//     leaking it into the shared local graph;
//   - no creator at all (a system/stdio write) -> the local graph.
//
// A transient failure loading history is returned as an error so the subscriber
// retries/parks the event instead of misrouting it.
func (p *oxigraphProjector) accountForEvent(
	ctx context.Context, event domain.EventEnvelope[any],
) (string, error) {
	if !p.stores.PerAccount() {
		return "", nil
	}
	if account := accountFromPayload(event.Payload); account != "" {
		return account, nil
	}
	account, createdBy, err := p.ownerFromHistory(ctx, event.AggregateID)
	if err != nil {
		return "", err
	}
	switch {
	case account != "":
		return account, nil
	case createdBy != "":
		return "", errAccountlessWrite
	default:
		return LocalAccountID, nil
	}
}

// ownerFromHistory recovers an aggregate's owning account and creator from its
// original Resource.Created event (which has always carried both). Used only
// when an event payload predates the per-event AccountID field, or to tell an
// ownerless system/stdio write from an accountless authenticated one. A load
// failure is returned as an error (not swallowed as "no owner") so the caller
// can retry rather than misroute.
func (p *oxigraphProjector) ownerFromHistory(
	ctx context.Context, aggregateID string,
) (accountID, createdBy string, err error) {
	events, err := p.eventStore.GetEvents(ctx, aggregateID)
	if err != nil {
		return "", "", fmt.Errorf("kg account resolution: load history for %q: %w", aggregateID, err)
	}
	for _, e := range events {
		if e.EventType == "Resource.Created" {
			return accountFromPayload(e.Payload), createdByFromPayload(e.Payload), nil
		}
	}
	return "", "", nil
}

// ensureAccountOntology projects a type's ontology into an account store the
// first time that account publishes an instance of the type (memoized). Idempotent
// on the store side; the cache just avoids repeating the work per instance.
func (p *oxigraphProjector) ensureAccountOntology(
	ctx context.Context, store repositories.KnowledgeGraphStore, accountID, aggregateID string,
) error {
	typeSlug := identity.ExtractResourceTypeSlug(aggregateID)
	if typeSlug == "" {
		return nil
	}
	key := accountID + "\x00" + typeSlug
	p.mu.Lock()
	seen := p.ontologySeen[key]
	p.mu.Unlock()
	if seen {
		return nil
	}
	rt, err := p.typeRepo.FindBySlug(ctx, typeSlug)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil // type unknown; the instance still projects, just without class triples
		}
		return err
	}
	if err := projectResourceTypeOntology(ctx, rt.Name(), rt.Slug(), rt.Context(), store, p.logger); err != nil {
		return err
	}
	p.mu.Lock()
	p.ontologySeen[key] = true
	p.mu.Unlock()
	return nil
}

// accountFromPayload extracts the owning account from a resource/triple event
// payload. Background subscribers receive payloads deserialized as
// map[string]any (JSON keys match the exported field name, "AccountID"); the
// typed cases cover callers that dispatch the original struct (e.g. tests).
func accountFromPayload(payload any) string {
	switch p := payload.(type) {
	case map[string]any:
		account, _ := p["AccountID"].(string)
		return account
	case entities.ResourceCreated:
		return p.AccountID
	case entities.ResourcePublished:
		return p.AccountID
	case entities.ResourceUpdated:
		return p.AccountID
	case entities.ResourceDeleted:
		return p.AccountID
	case entities.TripleCreated:
		return p.AccountID
	case entities.TripleDeleted:
		return p.AccountID
	default:
		return ""
	}
}

// createdByFromPayload extracts the creator agent id from a Resource.Created
// payload (the only event that carries it), matching accountFromPayload's
// map-vs-typed handling. An empty result means the resource was created without
// an identity — a system/stdio write.
func createdByFromPayload(payload any) string {
	switch p := payload.(type) {
	case map[string]any:
		createdBy, _ := p["CreatedBy"].(string)
		return createdBy
	case entities.ResourceCreated:
		return p.CreatedBy
	default:
		return ""
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
