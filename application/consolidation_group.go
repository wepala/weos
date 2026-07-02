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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	infraagents "github.com/wepala/weos/v3/infrastructure/agents"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"go.uber.org/fx"
)

// consolidationAttribution is the prov:wasAttributedTo value consolidation
// stamps on the facts it records.
const consolidationAttribution = "weos:consolidation"

// memoryTypeSlugs are the semantic/procedural memory types themselves. The
// consolidation policy never consolidates them — facts about facts would loop
// the subscriber back onto its own output.
var memoryTypeSlugs = map[string]bool{"fact": true, "playbook": true}

// factStore is the narrow slice of ResourceService the consolidation policy
// uses. Writes go through the full service pipeline (validation, graph
// assembly, triples, behaviors, UnitOfWork) — never direct persistence.
type factStore interface {
	Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error)
	Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
	ListByField(ctx context.Context, typeSlug, fieldName, fieldValue string) (
		repositories.PaginatedResponse[*entities.Resource], error)
}

// ProvideFactExtractor supplies the BYOK LLM port for consolidation: the
// ADK/Gemini implementation when an API key is configured, nil otherwise (the
// consolidation subscriber is then not registered — a graceful no-op).
func ProvideFactExtractor(adk *infraagents.ADKConfig, logger entities.Logger) entities.FactExtractor {
	if adk == nil {
		logger.Info(context.Background(),
			"consolidation: no LLM API key configured, fact extraction disabled")
		return nil
	}
	m, err := adk.CreateGeminiModel(context.Background())
	if err != nil {
		logger.Error(context.Background(),
			"consolidation: failed to create Gemini model, fact extraction disabled", "error", err)
		return nil
	}
	return appagents.NewGeminiFactExtractor(m)
}

// ConsolidationGroupParams bundles the consolidation group's dependencies.
type ConsolidationGroupParams struct {
	fx.In
	EventStore domain.EventStore
	Extractor  entities.FactExtractor `optional:"true"`
	Service    ResourceService
	TypeRepo   repositories.ResourceTypeRepository
	Logger     entities.Logger
}

// ProvideConsolidationGroup contributes the "consolidation" subscriber group
// when a fact extractor is available, and nothing otherwise. Like the oxigraph
// group it runs off the write path with its own checkpoint, so consolidation
// lag or failure never stalls a user's request, and a checkpoint reset replays
// history through the same handler.
func ProvideConsolidationGroup(p ConsolidationGroupParams) []SubscriberGroup {
	if p.Extractor == nil {
		p.Logger.Debug(context.Background(),
			"consolidation: no fact extractor, consolidation subscriber not registered")
		return nil
	}
	return []SubscriberGroup{{
		Name:    "consolidation",
		Handler: consolidationHandler(p.EventStore, p.Extractor, p.Service, p.TypeRepo, p.Logger),
	}}
}

// consolidationHandler distills episodic Resource.Published events into fact
// resources. Idempotence under checkpoint replay comes from dedup on the
// prov:wasDerivedFrom source event IDs already recorded on existing facts.
func consolidationHandler(
	eventStore domain.EventStore,
	extractor entities.FactExtractor,
	store factStore,
	typeRepo repositories.ResourceTypeRepository,
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
		// The transaction state's slug is only populated by Resource.Created;
		// update transactions carry it on the published signal instead. The
		// memory-type guard must hold on BOTH paths, or supersession updates
		// would loop the subscriber back onto its own output.
		typeSlug := publishedTypeSlug(event.Payload, state.TypeSlug)
		if state.IsDelete || state.Data == nil || memoryTypeSlugs[typeSlug] {
			return nil
		}
		factCtx, err := factTypeContext(ctx, typeRepo)
		if err != nil {
			return err
		}
		if factCtx == nil {
			logger.Debug(ctx, "consolidation: fact type not installed, skipping episode",
				"resource", event.AggregateID)
			return nil
		}
		related, err := relatedFacts(ctx, store, factCtx, event.AggregateID)
		if err != nil {
			return err
		}
		obs := entities.EpisodeObservation{
			EventIDs:   episodeEventURNs(txEvents, event),
			ResourceID: event.AggregateID,
			TypeSlug:   typeSlug,
			Data:       state.Data,
		}
		if alreadyConsolidated(related, obs.EventIDs) {
			logger.Debug(ctx, "consolidation: episode already consolidated",
				"resource", event.AggregateID)
			return nil
		}
		candidates, err := extractor.ExtractFacts(ctx, obs, activeFacts(related))
		if err != nil {
			return fmt.Errorf("consolidation: extract facts for %s: %w", event.AggregateID, err)
		}
		for _, c := range candidates {
			if err := recordCandidate(ctx, store, factCtx, c, obs, related, logger); err != nil {
				return err
			}
		}
		return nil
	}
}

// publishedTypeSlug reads the type slug from a Resource.Published payload —
// present on both create and update transactions, unlike the transaction
// state's slug, which only Resource.Created populates. Background subscribers
// see store-deserialized map payloads; the typed case covers direct dispatch.
func publishedTypeSlug(payload any, fallback string) string {
	switch p := payload.(type) {
	case map[string]any:
		if s, ok := p["TypeSlug"].(string); ok && s != "" {
			return s
		}
	case entities.ResourcePublished:
		if p.TypeSlug != "" {
			return p.TypeSlug
		}
	}
	return fallback
}

// factView is a parsed snapshot of an existing fact resource.
type factView struct {
	ID            string
	Statement     string
	About         string
	DerivedFrom   []string
	InvalidatedAt string
	// flat holds the fact's schema-shaped properties for update round-trips
	// (ResourceService.Update is full-replace, so partial data drops fields).
	flat map[string]any
}

// factTypeContext loads the fact type's JSON-LD context, or nil when the
// memory preset is not installed on this instance.
func factTypeContext(ctx context.Context, typeRepo repositories.ResourceTypeRepository) (json.RawMessage, error) {
	rt, err := typeRepo.FindBySlug(ctx, "fact")
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if rt == nil {
		return nil, nil
	}
	return rt.Context(), nil
}

// relatedFacts lists the facts already recorded about the resource.
func relatedFacts(
	ctx context.Context, store factStore, factCtx json.RawMessage, resourceID string,
) ([]factView, error) {
	page, err := store.ListByField(ctx, "fact", "about", resourceID)
	if err != nil {
		if errors.Is(err, repositories.ErrNoProjectionTable) {
			return nil, nil
		}
		return nil, fmt.Errorf("consolidation: list facts about %s: %w", resourceID, err)
	}
	views := make([]factView, 0, len(page.Data))
	for _, res := range page.Data {
		views = append(views, parseFact(res, factCtx))
	}
	return views, nil
}

// parseFact extracts the fields the policy needs from a fact resource's
// JSON-LD graph: intrinsic properties live on the entity node, the
// wasRevisionOf reference on the edges node.
func parseFact(res *entities.Resource, factCtx json.RawMessage) factView {
	v := factView{ID: res.GetID(), flat: map[string]any{}}
	var node map[string]any
	if err := json.Unmarshal(ExtractEntityNode(res.Data()), &node); err != nil {
		return v
	}
	for k, val := range node {
		if strings.HasPrefix(k, "@") {
			continue
		}
		v.flat[k] = val
	}
	v.Statement, _ = node["statement"].(string)             //nolint:errcheck // absent → ""
	v.About, _ = node["about"].(string)                     //nolint:errcheck // absent → ""
	v.InvalidatedAt, _ = node["invalidatedAtTime"].(string) //nolint:errcheck // absent → ""
	if arr, ok := node["wasDerivedFrom"].([]any); ok {
		for _, x := range arr {
			if s, ok := x.(string); ok {
				v.DerivedFrom = append(v.DerivedFrom, s)
			}
		}
	}
	if prior := EdgeValue(res.Data(), factCtx, "wasRevisionOf"); prior != "" {
		v.flat["wasRevisionOf"] = prior
	}
	return v
}

// alreadyConsolidated reports whether any existing fact was derived from one
// of the episode's events — the replay-idempotence check.
func alreadyConsolidated(related []factView, eventIDs []string) bool {
	for _, f := range related {
		for _, d := range f.DerivedFrom {
			if slices.Contains(eventIDs, d) {
				return true
			}
		}
	}
	return false
}

// activeFacts converts the non-superseded views into the extractor's input.
func activeFacts(related []factView) []entities.ExistingFact {
	out := make([]entities.ExistingFact, 0, len(related))
	for _, f := range related {
		if f.InvalidatedAt != "" {
			continue
		}
		out = append(out, entities.ExistingFact{ID: f.ID, Statement: f.Statement, About: f.About})
	}
	return out
}

// episodeEventURNs collects urn:event: identifiers for the episodic events in
// the transaction (the Created/Updated events on the published aggregate),
// falling back to the published signal's own event ID.
func episodeEventURNs(txEvents []domain.EventEnvelope[any], published domain.EventEnvelope[any]) []string {
	var ids []string
	for _, e := range txEvents {
		if e.AggregateID != published.AggregateID {
			continue
		}
		if e.EventType == "Resource.Created" || e.EventType == "Resource.Updated" {
			ids = append(ids, "urn:event:"+e.ID)
		}
	}
	if len(ids) == 0 {
		ids = []string{"urn:event:" + published.ID}
	}
	return ids
}

// recordCandidate writes one extracted fact through the service pipeline. When
// the candidate supersedes an existing fact, the predecessor is invalidated
// FIRST: if the process dies before the new fact commits, the retry re-runs
// extraction (dedup hasn't tripped yet) and the invalidation is a no-op the
// second time. The superseding fact then carries wasRevisionOf, and the fact
// behavior records Fact.Recorded / Fact.Superseded in the same commit.
func recordCandidate(
	ctx context.Context,
	store factStore,
	factCtx json.RawMessage,
	c entities.FactCandidate,
	obs entities.EpisodeObservation,
	related []factView,
	logger entities.Logger,
) error {
	if strings.TrimSpace(c.Statement) == "" {
		return nil
	}
	about := c.About
	if about == "" {
		about = obs.ResourceID
	}
	if about != obs.ResourceID {
		// The episode-level dedup only sees facts about the observed resource.
		// A candidate about another entity needs its own replay check, or a
		// checkpoint reset would re-record it under the other about.
		other, err := relatedFacts(ctx, store, factCtx, about)
		if err != nil {
			return err
		}
		if alreadyConsolidated(other, obs.EventIDs) {
			return nil
		}
	}
	supersedes := findRelated(related, c.SupersedesID)
	if c.SupersedesID != "" && supersedes == nil {
		// Never trust a URN the model invented — only facts we showed it.
		logger.Warn(ctx, "consolidation: dropping unknown supersedesId",
			"supersedesId", c.SupersedesID)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if supersedes != nil && supersedes.InvalidatedAt == "" {
		if err := invalidateFact(ctx, store, supersedes, now); err != nil {
			return err
		}
	}
	data := map[string]any{
		"statement":       c.Statement,
		"about":           about,
		"attributedTo":    consolidationAttribution,
		"generatedAtTime": now,
		"wasDerivedFrom":  obs.EventIDs,
	}
	if c.Confidence > 0 {
		data["confidence"] = c.Confidence
	}
	if supersedes != nil {
		data["wasRevisionOf"] = supersedes.ID
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("consolidation: marshal fact data: %w", err)
	}
	if _, err := store.Create(ctx, CreateResourceCommand{TypeSlug: "fact", Data: raw}); err != nil {
		return fmt.Errorf("consolidation: record fact for %s: %w", obs.ResourceID, err)
	}
	return nil
}

// invalidateFact stamps invalidatedAtTime on a superseded fact via a full-data
// update (never a delete — history must replay intact).
func invalidateFact(ctx context.Context, store factStore, f *factView, now string) error {
	flat := make(map[string]any, len(f.flat)+1)
	maps.Copy(flat, f.flat)
	flat["invalidatedAtTime"] = now
	data, err := json.Marshal(flat)
	if err != nil {
		return fmt.Errorf("consolidation: marshal invalidation for %s: %w", f.ID, err)
	}
	if _, err := store.Update(ctx, UpdateResourceCommand{ID: f.ID, Data: data}); err != nil {
		return fmt.Errorf("consolidation: invalidate %s: %w", f.ID, err)
	}
	return nil
}

func findRelated(related []factView, id string) *factView {
	if id == "" {
		return nil
	}
	for i := range related {
		if related[i].ID == id {
			return &related[i]
		}
	}
	return nil
}
