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
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
//     is map-iteration order, so it cannot report its own ambiguity. Every
//     name the type's context and schema resolve — reference properties,
//     plain terms, recorded aliases — is grouped by predicate instead, and an
//     IRI claimed by more than one name is never rewritten: it is reported
//     with the candidates for the operator to decide. Guessing from the
//     target's URN would be exactly the silent choice this epic exists to stop.
//
// The payload is decoded with json.Number so a rewrite re-encodes every other
// value exactly: pericarp's JSONB scan goes through float64, and an integer
// above 2^53 in an intrinsic property would otherwise change on the way back.

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
// operator to find the event and act on it.
type EdgeKeyProblem struct {
	TypeSlug   string
	ResourceID string
	EventID    string
	Position   int64
	// Key is the edges-node key as stored — the predicate IRI.
	Key string
	// Candidates lists the names that claim Key: several for an ambiguous
	// edge, the one taken name for a collision, none for an unresolved edge.
	Candidates []string
	// Reason is a short operator-facing explanation.
	Reason string

	kind edgeKeyProblemKind
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
	// Skipped counts events the run could not read, by reason: a payload
	// whose Data is not an object, an edges node that is not an object, an
	// update whose aggregate names no resource type.
	Skipped map[string]int
	// Rewritten is the events with at least one edge key changed, all types.
	Rewritten int
	// Types is the per-type breakdown, keyed by type slug.
	Types map[string]*EdgeKeyTypeReport
	// Ambiguous are edges whose IRI more than one name claims.
	Ambiguous []EdgeKeyProblem
	// Unresolved are edges no term, alias or @vocab prefix names — including
	// every edge of a type the store no longer holds.
	Unresolved []EdgeKeyProblem
	// Collisions are edges that resolved to a property name the document
	// already keys another edge by, so rewriting would have to merge values.
	Collisions []EdgeKeyProblem
	// AmbiguousTotal, UnresolvedTotal and CollisionTotal count every problem
	// found; the lists above keep the first MaxReportedProblems of each so a
	// store with one missing type does not hold a struct per edge in memory.
	AmbiguousTotal, UnresolvedTotal, CollisionTotal int
}

// MaxReportedProblems bounds each problem list on a report.
const MaxReportedProblems = 500

// Problems reports whether the run declined to rewrite anything.
func (r NormalizeEdgeKeysReport) Problems() int {
	return r.AmbiguousTotal + r.UnresolvedTotal + r.CollisionTotal
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
// the stored record when it handles Resource.Published, so they are not
// rewritten here.
var edgeCarryingEventTypes = []string{"Resource.Created", "Resource.Updated"}

// edgeKeyProblemKind separates the three reasons an edge is left alone.
type edgeKeyProblemKind int

const (
	problemUnresolved edgeKeyProblemKind = iota
	problemAmbiguous
	problemCollision
)

// eventRow reads the columns the run needs from the events table with the
// payload as raw bytes, so it can be decoded with json.Number rather than
// through pericarp's float64 scan.
type eventRow struct {
	ID          string
	AggregateID string
	EventType   string
	SequenceNo  int
	Position    int64
	Payload     json.RawMessage
}

func (eventRow) TableName() string { return pericarpinfra.GormEventModel{}.TableName() }

// readEdgeEventsAfter pages the edge-carrying events in global position
// order. On Postgres it withholds rows whose inserting transaction may still
// be in flight, exactly as pericarp's own reader does: a position is taken at
// INSERT and becomes visible at COMMIT, so without the guard a row committed
// late would fall below the cursor and never be read — never rewritten, never
// counted. SQLite assigns positions inside the write transaction and needs no
// guard.
func readEdgeEventsAfter(ctx context.Context, db *gorm.DB, after int64, batch int) ([]eventRow, error) {
	q := db.WithContext(ctx).Model(&eventRow{}).
		Where("position > ? AND event_type IN ?", after, edgeCarryingEventTypes)
	if db.Name() == "postgres" {
		q = q.Where("xact_id < pg_snapshot_xmin(pg_current_snapshot())")
	}
	var rows []eventRow
	if err := q.Order("position ASC").Limit(batch).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// edgesNodeShape reports a stored document's edges node and whether the
// document is corrupt — a @graph whose second member is not an object. The
// two are told apart so a corrupt document is counted as skipped rather than
// read as "no edges".
func edgesNodeShape(data map[string]any) (edges map[string]any, corrupt bool) {
	graph, _ := data["@graph"].([]any)
	if len(graph) < 2 {
		return nil, false
	}
	edges, ok := graph[1].(map[string]any)
	return edges, !ok
}

// decodePayload decodes an event payload keeping numbers as json.Number.
func decodePayload(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

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
		rt:     rt,
		write:  opts.Write,
		report: NormalizeEdgeKeysReport{DryRun: !opts.Write, Types: map[string]*EdgeKeyTypeReport{}},
		types:  newResourceTypeIndex(rt.RTRepo, rt.Links, rt.Logger),
	}

	var last int64
	for {
		rows, err := readEdgeEventsAfter(ctx, rt.DB, last, batch)
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
	rt     NormalizeEdgeKeysRuntime
	write  bool
	report NormalizeEdgeKeysReport
	types  *resourceTypeIndex
}

// resourceTypeIndex learns each event's resource type and holds one resolver
// per type for the length of a scan. It is shared by the normalization and
// the count (#519) so the two can never classify an edge differently.
type resourceTypeIndex struct {
	rtRepo    repositories.ResourceTypeRepository
	links     *LinkRegistry
	logger    entities.Logger
	slugOf    map[string]string           // aggregate id → type slug, from Resource.Created
	resolvers map[string]*edgeKeyResolver // type slug → resolver; nil entry = type not stored
}

func newResourceTypeIndex(
	rtRepo repositories.ResourceTypeRepository, links *LinkRegistry, logger entities.Logger,
) *resourceTypeIndex {
	return &resourceTypeIndex{rtRepo: rtRepo, links: links, logger: logger,
		slugOf: map[string]string{}, resolvers: map[string]*edgeKeyResolver{}}
}

// processBatch examines one page of events and, when writing, applies every
// changed payload in a single transaction so a batch lands whole or not at
// all. Only the payload column is touched: id, aggregate, sequence, type,
// transaction and position stay exactly as they were.
func (run *edgeKeyRun) processBatch(ctx context.Context, rows []eventRow) error {
	type change struct {
		id      string
		payload pericarpinfra.JSONB
	}
	var changes []change
	for i := range rows {
		payload, err := run.rewriteEvent(ctx, &rows[i])
		if err != nil {
			return err
		}
		if payload != nil && run.write {
			changes = append(changes, change{id: rows[i].ID, payload: payload})
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

// rewriteEvent normalizes one event's payload and returns the rewritten
// payload, or nil when nothing changed. Problems are recorded on the report;
// they never stop the run, because an ambiguous type must not quarantine the
// clean ones.
func (run *edgeKeyRun) rewriteEvent(ctx context.Context, row *eventRow) (pericarpinfra.JSONB, error) {
	payload, err := decodePayload(row.Payload)
	if err != nil {
		return nil, fmt.Errorf("normalize-edge-keys: event %s at position %d: payload is not a JSON object: %w",
			row.ID, row.Position, err)
	}
	slug := run.types.slugFor(row, payload)
	if slug == "" {
		// An orphan Resource.Updated whose aggregate id names no resource
		// type (a page's five-part URN, say) with no Created event in the
		// feed to say what it is. Counted, so the operator sees it.
		run.skip("event names no resource type")
		return nil, nil
	}
	run.report.Scanned++
	tr := run.report.Types[slug]
	if tr == nil {
		tr = &EdgeKeyTypeReport{}
		run.report.Types[slug] = tr
	}
	tr.Scanned++

	data, ok := payload["Data"].(map[string]any)
	if !ok {
		run.skip("Data is not a JSON object")
		return nil, nil
	}
	if _, corrupt := edgesNodeShape(data); corrupt {
		run.skip("@graph[1] is not an edges node")
		return nil, nil
	}
	resolver, err := run.types.resolverFor(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("normalize-edge-keys: %w", err)
	}
	changed, problems := normalizeEdgeKeys(data, resolver)
	for i := range problems {
		problems[i].TypeSlug = slug
		problems[i].ResourceID = row.AggregateID
		problems[i].EventID = row.ID
		problems[i].Position = row.Position
		run.record(problems[i])
	}
	if !changed {
		return nil, nil
	}
	payload["Data"] = data
	tr.Rewritten++
	run.report.Rewritten++
	return pericarpinfra.JSONB(payload), nil
}

// skip counts an event whose payload the run could not read, by reason, so
// the summary never claims a feed is clean on the strength of rows it never
// looked inside.
func (run *edgeKeyRun) skip(reason string) {
	if run.report.Skipped == nil {
		run.report.Skipped = map[string]int{}
	}
	run.report.Skipped[reason]++
}

// record files a problem in its bucket, keeping the list bounded.
func (run *edgeKeyRun) record(p EdgeKeyProblem) {
	r := &run.report
	var list *[]EdgeKeyProblem
	var total *int
	switch p.kind {
	case problemAmbiguous:
		list, total = &r.Ambiguous, &r.AmbiguousTotal
	case problemCollision:
		list, total = &r.Collisions, &r.CollisionTotal
	default:
		list, total = &r.Unresolved, &r.UnresolvedTotal
	}
	*total++
	if len(*list) < MaxReportedProblems {
		*list = append(*list, p)
	}
}

// slugFor learns an event's resource type: the Created payload names it, and
// a later Updated event for the same aggregate reuses that answer. The URN is
// the fallback for an aggregate whose Created event the feed lacks.
func (ix *resourceTypeIndex) slugFor(row *eventRow, payload map[string]any) string {
	if slug, ok := payload["TypeSlug"].(string); ok && slug != "" {
		ix.slugOf[row.AggregateID] = slug
		return slug
	}
	if slug, ok := ix.slugOf[row.AggregateID]; ok {
		return slug
	}
	return identity.ExtractResourceTypeSlug(row.AggregateID)
}

// resolverFor loads a type's context once per run. A type the store no longer
// holds yields a nil resolver: every IRI key on its events is then reported
// unresolved rather than guessed at.
func (ix *resourceTypeIndex) resolverFor(ctx context.Context, slug string) (*edgeKeyResolver, error) {
	if r, seen := ix.resolvers[slug]; seen {
		return r, nil
	}
	rtype, err := ix.rtRepo.FindBySlug(ctx, slug)
	var resolver *edgeKeyResolver
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		// Deleted types keep their events forever. Their edges are reported,
		// not rewritten, and the run carries on with the types that exist.
		ix.logger.Warn(ctx, "resource type not stored; its IRI-keyed edges are reported, not resolved",
			"slug", slug)
	case err != nil:
		return nil, fmt.Errorf("load resource type %q: %w", slug, err)
	default:
		resolver = newEdgeKeyResolver(rtype.Context(), rtype.Schema(), ix.links.BySource(slug))
	}
	ix.resolvers[slug] = resolver
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
	// owner uncertain. A name with a colon (a control key such as
	// rdfs:subClassOf, or a compact IRI) is never a property name; a prefix
	// definition passes, harmlessly, since a namespace IRI is never an edge
	// key.
	_, forward := jsonld.ParseContext(ldContext)
	for name, iri := range forward {
		if jsonld.IsIRIKey(name) {
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
	switch names := r.claims[key]; len(names) {
	case 0:
		// Nothing declares it; only the @vocab prefix can still name it.
		if name, found := jsonld.EdgeProperty(key, r.ldContext); found {
			return name, nil, true
		}
		return "", nil, false
	case 1:
		return names[0], nil, true
	default:
		return "", names, false
	}
}

// normalizeEdgeKeys rewrites the edges node of one stored document in place.
// It returns whether any key changed and the edges it declined to rewrite.
//
// The entity node (@graph[0]) is never touched. When at least one key moves,
// the document's `@context` becomes what a fresh write embeds, so the compact
// keys still expand to predicates for the knowledge graph; a type whose
// context has nothing storable leaves the document's own `@context` as it
// was. A resource with one unresolvable edge keeps its resolvable ones
// rewritten: the problem is reported per edge, not per document.
func normalizeEdgeKeys(data map[string]any, resolver *edgeKeyResolver) (bool, []EdgeKeyProblem) {
	edges := storedEdgesNode(data)
	if edges == nil {
		return false, nil
	}
	keys := iriEdgeKeys(edges)

	var problems []EdgeKeyProblem
	changed := false
	for _, key := range keys {
		name, candidates, resolved := resolver.resolve(key)
		switch {
		case len(candidates) > 1:
			problems = append(problems, EdgeKeyProblem{Key: key, Candidates: candidates, kind: problemAmbiguous,
				Reason: "more than one name resolves to this predicate; decide which property owns the edge"})
		case !resolved:
			problems = append(problems, EdgeKeyProblem{Key: key, kind: problemUnresolved,
				Reason: "no term, alias or @vocab prefix in the type's context names this predicate"})
		default:
			if _, taken := edges[name]; taken {
				// Both forms present for one property — an edge added after
				// the write through AddEdgeToGraph, say. Merging would choose
				// which value wins; report instead.
				problems = append(problems, EdgeKeyProblem{Key: key, Candidates: []string{name}, kind: problemCollision,
					Reason: fmt.Sprintf("the document already keys an edge by %q; merging would pick a value", name)})
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
	if resolver.storable != nil {
		data["@context"] = resolver.storable
	}
	return true, problems
}

// storedEdgesNode returns a stored document's edges node, or nil when the
// document has none or it is not an object. Callers that must tell those two
// apart use edgesNodeShape.
func storedEdgesNode(data map[string]any) map[string]any {
	edges, _ := edgesNodeShape(data)
	return edges
}

// iriEdgeKeys lists an edges node's keys that are predicate IRIs, sorted.
// `@id` is the node's own subject, not an edge, and is never one of them.
func iriEdgeKeys(edges map[string]any) []string {
	var keys []string
	for key := range edges {
		if key != "@id" && jsonld.IsIRIKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
