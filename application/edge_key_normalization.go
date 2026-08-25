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
	"fmt"
	"sort"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	gormprov "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/infrastructure/logging"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/pkg/identity"
	"github.com/wepala/weos/v3/pkg/jsonld"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	pericarpinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// This file is the one-shot migration behind `weos worker normalize-edge-keys`
// (issue #523).
//
// Issue #515 changed how a resource's edges node is WRITTEN: it is keyed by
// property name, and the document's own `@context` carries the mapping. Every
// event written before that keys its edges by the predicate IRI the property
// resolved to at write time. ResourceCreated carries that graph and events are
// immutable, so a reprojection reproduces the old key no matter what the
// current context says — which is what makes a namespace change expensive: a
// renamed term needs an alias (`weos:termAliases`), forever, on every instance.
//
// A property-name key has no such coupling; it resolves through whatever the
// current context says. Rewriting the stored events once removes the whole
// problem class instead of managing it per rename. The set stopped growing the
// day #515 shipped, so the migration is as small as it will ever be.
//
// The rewrite changes how an edge is written down, not what it asserts:
// pericarp events carry aggregate id, sequence number and payload with no hash
// chain and no signature, so re-encoding a payload breaks no integrity
// guarantee. Nothing is appended, deleted or renumbered — the rollback story
// is "restore the backup", and a reprojection replays by position.
//
// Two decisions are deliberate and pinned by the contract
// (tests/e2e/features/edge_key_normalization.feature):
//
//   - An IRI key is resolved the way the READ PATH resolves it — through
//     jsonld.EdgeProperty (live term, then alias, then the `@vocab` prefix) —
//     not through BuildReverseMap alone. The reverse map has no entry for a
//     reference the preset never gave a term (#510), whose key is `@vocab` +
//     name; that is the largest legacy population and every reader already
//     resolves it, so a migration that left it behind would fail at its job.
//   - Ambiguity is detected FORWARD. BuildReverseMap is a map[string]string:
//     two properties on one predicate collapse into one entry and the winner
//     is map-iteration order, so it cannot report its own ambiguity. The
//     type's properties are grouped by resolved predicate instead, and an IRI
//     claimed by more than one property is never rewritten — it is reported
//     with both candidates for the operator to decide. Guessing from the
//     target's URN would be exactly the silent choice this epic exists to stop.

// NormalizeEdgeKeysRuntime bundles what NormalizeEdgeKeys needs; populate it
// from an fx app built with NormalizeEdgeKeysModule.
type NormalizeEdgeKeysRuntime struct {
	fx.In
	DB     *gorm.DB
	RTRepo repositories.ResourceTypeRepository
	Links  *LinkRegistry
	Logger entities.Logger
}

// NormalizeEdgeKeysModule is the narrow assembly behind the command: config,
// logging, the gorm DB, the resource-type repository and the link registry.
// Deliberately NOT application.Module, for the same reason as ReprojectModule
// (issue #443): the full module's startup reconcile appends ResourceType
// events, and a migration that rewrites the feed must not also grow it.
func NormalizeEdgeKeysModule(cfg config.Config, registry *PresetRegistry) fx.Option {
	if registry == nil {
		panic("application.NormalizeEdgeKeysModule: PresetRegistry must not be nil")
	}
	return fx.Module("normalize-edge-keys",
		fx.Provide(func() config.Config { return cfg }),
		fx.Provide(func() *PresetRegistry { return registry }),
		fx.Provide(logging.ProvideZapLogger),
		fx.Provide(logging.ProvideLogger),
		fx.Provide(gormprov.ProvideGormDB),
		// The event store is constructed for its side effect: pericarp's
		// auto-migration adds the global `position` column this command pages
		// by, so an instance upgraded straight from a build without it does
		// not fail on the first query.
		fx.Provide(gormprov.ProvideEventStore),
		fx.Invoke(func(pericarpdomain.EventStore) {}),
		fx.Provide(gormprov.ProvideResourceTypeRepository),
		fx.Provide(func(r *PresetRegistry, logger entities.Logger) *LinkRegistry {
			return buildLinkRegistry(r, logger)
		}),
	)
}

// NormalizeEdgeKeysOptions tune a run.
type NormalizeEdgeKeysOptions struct {
	// Write applies the rewrite. False — the default — is a dry run that
	// reports what would change and touches nothing.
	Write bool
	// BatchSize is event rows per read; <= 0 uses 500.
	BatchSize int
}

// EdgeKeyProblem is one edge the run declined to rewrite, with enough for the
// operator to act on it.
type EdgeKeyProblem struct {
	TypeSlug   string
	ResourceID string
	EventID    string
	Position   int64
	// Key is the edges-node key as stored — the predicate IRI.
	Key string
	// Candidates lists the properties that claim Key, for an ambiguous edge.
	Candidates []string
	// Reason is a short operator-facing explanation.
	Reason string
}

// EdgeKeyTypeReport counts one resource type's share of a run.
type EdgeKeyTypeReport struct {
	// Scanned is the Resource.Created/Updated events examined for the type.
	Scanned int
	// Rewritten is the events with at least one edge key changed — what a
	// dry run WOULD rewrite, what a write DID.
	Rewritten int
}

// NormalizeEdgeKeysReport is what a run found and did.
type NormalizeEdgeKeysReport struct {
	DryRun bool
	// Scanned is every Resource.Created/Updated event examined.
	Scanned int
	// Rewritten is the events with at least one edge key changed, all types.
	Rewritten int
	// Types is the per-type breakdown, keyed by type slug.
	Types map[string]*EdgeKeyTypeReport
	// Ambiguous are edges whose IRI more than one property claims.
	Ambiguous []EdgeKeyProblem
	// Unresolved are edges no term, alias or @vocab prefix names.
	Unresolved []EdgeKeyProblem
}

// TypeSlugs returns the report's type slugs in a stable order.
func (r NormalizeEdgeKeysReport) TypeSlugs() []string {
	slugs := make([]string, 0, len(r.Types))
	for slug := range r.Types {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

// Event types whose payload carries a resource graph. Triple.* events carry a
// predicate and an object, not an edges node; the projection folds them into
// the graph at read time, so they are not rewritten.
var edgeCarryingEventTypes = []string{"Resource.Created", "Resource.Updated"}

// NormalizeEdgeKeys walks every Resource.Created and Resource.Updated event in
// global position order and rewrites each edges-node key that is a predicate
// IRI to the property name it resolves to under the type's CURRENT context.
//
// A rewritten document also takes the same `@context` a fresh write embeds
// (buildStorableContext of the type's context), so it is indistinguishable
// from a post-#515 write and still expands to a graph for the knowledge graph
// store. An edge that resolved through an alias therefore moves, in the
// graph, to the term's current IRI — that is the rename the operator already
// adopted, and it is why no alias is needed once the feed is normalized.
//
// A single pass suffices to learn each event's type: an aggregate's
// Resource.Created precedes its Resource.Updated in position order, and the
// Created payload names the TypeSlug. An aggregate whose Created event is not
// in the feed falls back to the `urn:<slug>:…` id.
func NormalizeEdgeKeys(
	ctx context.Context, rt NormalizeEdgeKeysRuntime, opts NormalizeEdgeKeysOptions,
) (NormalizeEdgeKeysReport, error) {
	batch := opts.BatchSize
	if batch <= 0 {
		batch = 500
	}
	run := &edgeKeyRun{
		rt:        rt,
		write:     opts.Write,
		report:    NormalizeEdgeKeysReport{DryRun: !opts.Write, Types: map[string]*EdgeKeyTypeReport{}},
		slugOf:    map[string]string{},
		resolvers: map[string]*edgeKeyResolver{},
	}

	var last int64
	for {
		var rows []pericarpinfra.GormEventModel
		err := rt.DB.WithContext(ctx).Model(&pericarpinfra.GormEventModel{}).
			Where("position > ? AND event_type IN ?", last, edgeCarryingEventTypes).
			Order("position ASC").Limit(batch).Find(&rows).Error
		if err != nil {
			return run.report, fmt.Errorf("normalize-edge-keys: read after position %d: %w", last, err)
		}
		if len(rows) == 0 {
			break
		}
		if err := run.processBatch(ctx, rows); err != nil {
			return run.report, err
		}
		last = rows[len(rows)-1].Position
		rt.Logger.Info(ctx, "normalize-edge-keys: progress", "position", last,
			"scanned", run.report.Scanned, "rewritten", run.report.Rewritten, "dryRun", !opts.Write)
	}
	return run.report, nil
}

// edgeKeyRun carries one run's state across batches.
type edgeKeyRun struct {
	rt        NormalizeEdgeKeysRuntime
	write     bool
	report    NormalizeEdgeKeysReport
	slugOf    map[string]string           // aggregate id → type slug, from Resource.Created
	resolvers map[string]*edgeKeyResolver // type slug → resolver; nil entry = type not stored
}

// processBatch examines one page of events and, when writing, applies every
// changed payload in a single transaction so a batch lands whole or not at
// all. Only the payload column is touched: id, aggregate, sequence, type,
// transaction and position stay exactly as they were.
func (run *edgeKeyRun) processBatch(ctx context.Context, rows []pericarpinfra.GormEventModel) error {
	type change struct {
		id      string
		payload pericarpinfra.JSONB
	}
	var changes []change
	for i := range rows {
		row := rows[i]
		changed, err := run.rewriteEvent(ctx, &row)
		if err != nil {
			return err
		}
		if changed && run.write {
			changes = append(changes, change{id: row.ID, payload: row.Payload})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return run.rt.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := tx.Model(&pericarpinfra.GormEventModel{}).
				Where("id = ?", c.id).Update("payload", c.payload).Error; err != nil {
				return fmt.Errorf("normalize-edge-keys: write event %s: %w", c.id, err)
			}
		}
		return nil
	})
}

// rewriteEvent normalizes one event's payload in place and reports whether
// anything changed. Problems are recorded on the report; they never stop the
// run, because an ambiguous type must not quarantine the clean ones.
func (run *edgeKeyRun) rewriteEvent(ctx context.Context, row *pericarpinfra.GormEventModel) (bool, error) {
	slug := run.typeSlugFor(row)
	if slug == "" {
		return false, nil // not a resource aggregate at all; nothing to key
	}
	run.report.Scanned++
	tr := run.report.Types[slug]
	if tr == nil {
		tr = &EdgeKeyTypeReport{}
		run.report.Types[slug] = tr
	}
	tr.Scanned++

	data, ok := row.Payload["Data"].(map[string]any)
	if !ok {
		return false, nil
	}
	resolver, err := run.resolverFor(ctx, slug)
	if err != nil {
		return false, err
	}
	changed, problems := normalizeEdgeKeys(data, resolver)
	for i := range problems {
		problems[i].TypeSlug = slug
		problems[i].ResourceID = row.AggregateID
		problems[i].EventID = row.ID
		problems[i].Position = row.Position
		if len(problems[i].Candidates) > 1 {
			run.report.Ambiguous = append(run.report.Ambiguous, problems[i])
		} else {
			run.report.Unresolved = append(run.report.Unresolved, problems[i])
		}
	}
	if !changed {
		return false, nil
	}
	row.Payload["Data"] = data
	tr.Rewritten++
	run.report.Rewritten++
	return true, nil
}

// typeSlugFor learns an event's resource type: the Created payload names it,
// and a later Updated event for the same aggregate reuses that answer. The
// URN is the fallback for an aggregate whose Created event the feed lacks.
func (run *edgeKeyRun) typeSlugFor(row *pericarpinfra.GormEventModel) string {
	if slug, ok := row.Payload["TypeSlug"].(string); ok && slug != "" {
		run.slugOf[row.AggregateID] = slug
		return slug
	}
	if slug, ok := run.slugOf[row.AggregateID]; ok {
		return slug
	}
	return identity.ExtractResourceTypeSlug(row.AggregateID)
}

// resolverFor loads a type's context once per run. A type the store no longer
// holds yields a nil resolver: every IRI key on its events is then reported
// unresolved rather than guessed at.
func (run *edgeKeyRun) resolverFor(ctx context.Context, slug string) (*edgeKeyResolver, error) {
	if r, seen := run.resolvers[slug]; seen {
		return r, nil
	}
	rtype, err := run.rt.RTRepo.FindBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("normalize-edge-keys: load resource type %q: %w", slug, err)
	}
	var resolver *edgeKeyResolver
	if rtype != nil {
		resolver = newEdgeKeyResolver(rtype.Context(), rtype.Schema(), run.rt.Links.BySource(slug))
	} else {
		run.rt.Logger.Warn(ctx, "normalize-edge-keys: resource type not stored; its edges are reported, not rewritten",
			"slug", slug)
	}
	run.resolvers[slug] = resolver
	return resolver, nil
}

// edgeKeyResolver answers "which property owns this edge key" for one type.
type edgeKeyResolver struct {
	ldContext json.RawMessage
	// claims maps a predicate IRI to the DISTINCT property names that resolve
	// to it under the current context — the forward direction, which is the
	// only one that can see a collision.
	claims map[string][]string
	// storable is the `@context` a fresh write of this type embeds today.
	storable any
}

func newEdgeKeyResolver(ldContext, schema json.RawMessage, links []PresetLinkDefinition) *edgeKeyResolver {
	claims := map[string]map[string]struct{}{}
	claim := func(iri, name string) {
		if claims[iri] == nil {
			claims[iri] = map[string]struct{}{}
		}
		claims[iri][name] = struct{}{}
	}
	for _, ref := range ExtractReferencePropertiesWithLinks(schema, ldContext, links) {
		claim(ref.PredicateIRI, ref.PropertyName)
	}
	// A term the context declares on the same predicate as a reference is a
	// second claimant too — it need not be a reference to make the edge's
	// owner uncertain. Prefix definitions and control keys are not properties.
	_, forward := jsonld.ParseContext(ldContext)
	for name, iri := range forward {
		if jsonld.ControlKeywords[name] || jsonld.IsIRIKey(name) {
			continue
		}
		claim(iri, name)
	}
	// A historical IRI two properties both recorded is just as ambiguous as a
	// live one: BuildReverseMap would hand it to whichever alias it met first.
	for name, iris := range jsonld.TermAliases(ldContext) {
		for _, iri := range iris {
			claim(iri, name)
		}
	}
	out := &edgeKeyResolver{ldContext: ldContext, claims: map[string][]string{}, storable: nil}
	for iri, names := range claims {
		list := make([]string, 0, len(names))
		for name := range names {
			list = append(list, name)
		}
		sort.Strings(list)
		out.claims[iri] = list
	}
	if len(ldContext) > 0 {
		out.storable = buildStorableContext(ldContext)
	}
	return out
}

// resolve returns the property name for an edges-node key, or the candidates
// when more than one property claims it, or nothing when no term, alias or
// @vocab prefix accounts for it. A key that is not an IRI is already a
// property name and needs no work.
func (r *edgeKeyResolver) resolve(key string) (name string, candidates []string, ok bool) {
	if !jsonld.IsIRIKey(key) {
		return key, nil, true
	}
	if r == nil {
		return "", nil, false
	}
	if names := r.claims[key]; len(names) > 1 {
		return "", names, false
	}
	if name, found := jsonld.EdgeProperty(key, r.ldContext); found {
		return name, nil, true
	}
	return "", nil, false
}

// normalizeEdgeKeys rewrites the edges node of one stored document in place.
// It returns whether any key changed and the edges it declined to rewrite.
//
// The entity node (@graph[0]) is never touched. When at least one key moves,
// the document's `@context` becomes what a fresh write embeds, so the compact
// keys still expand to predicates for the knowledge graph. A resource with
// one unresolvable edge keeps its resolvable ones rewritten: the problem is
// reported per edge, not per document.
func normalizeEdgeKeys(data map[string]any, resolver *edgeKeyResolver) (bool, []EdgeKeyProblem) {
	graph, _ := data["@graph"].([]any)
	if len(graph) < 2 {
		return false, nil
	}
	edges, ok := graph[1].(map[string]any)
	if !ok {
		return false, nil
	}
	keys := make([]string, 0, len(edges))
	for key := range edges {
		if key != "@id" && jsonld.IsIRIKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var problems []EdgeKeyProblem
	changed := false
	for _, key := range keys {
		name, candidates, resolved := resolver.resolve(key)
		switch {
		case len(candidates) > 1:
			problems = append(problems, EdgeKeyProblem{Key: key, Candidates: candidates,
				Reason: "more than one property resolves to this predicate; decide which owns the edge"})
		case !resolved:
			problems = append(problems, EdgeKeyProblem{Key: key,
				Reason: "no term, alias or @vocab prefix in the type's context names this predicate"})
		default:
			if _, taken := edges[name]; taken {
				// Both forms present for one property — an edge added after
				// the write through AddEdgeToGraph, say. Merging would choose
				// which value wins; report instead.
				problems = append(problems, EdgeKeyProblem{Key: key, Candidates: []string{name},
					Reason: fmt.Sprintf("the document already holds a %q key; merging would pick a value", name)})
				continue
			}
			edges[name] = edges[key]
			delete(edges, key)
			changed = true
		}
	}
	if !changed {
		return false, problems
	}
	graph[1] = edges
	data["@graph"] = graph
	if resolver.storable != nil {
		data["@context"] = resolver.storable
	} else {
		delete(data, "@context")
	}
	return true, problems
}
