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
	"strings"

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
		fx.Provide(buildLinkRegistry),
	)
}

// NormalizeEdgeKeysOptions tune a run.
type NormalizeEdgeKeysOptions struct {
	// Write applies the rewrite. False — the default — is a dry run that
	// reports what would change and touches nothing.
	Write bool
	// BatchSize is event rows per read; <= 0 uses 500.
	BatchSize int
	// Types, when set, limits the run to these resource type slugs. Events
	// of other types are neither read into the report nor rewritten.
	Types []string
	// Restamp also rewrites every document's embedded @context — and the
	// entity node's @type — to what a fresh write embeds today, even when
	// the edges are already keyed by property name. A reprojection replays
	// payloads verbatim, so this is the only way an already-written resource
	// takes a class or predicate its type's context has since moved to
	// (issue #520; #521 gives Person and Organization a class the same way).
	Restamp bool
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
	// Restamped is the events whose embedded @context or entity @type was
	// brought up to date by --restamp (an event can be both rewritten and
	// re-stamped; it is counted in each).
	Restamped int
	// TriplesMoved is the Triple.Created/Deleted events whose predicate
	// --restamp moved to follow a re-stamped document.
	TriplesMoved int
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
	// Restamped is the events --restamp brought up to date, all types.
	Restamped int
	// TriplesMoved is the Triple.* events --restamp moved, all types.
	TriplesMoved int
	// Restamp records that the run re-stamped as well as rewrote.
	Restamp bool
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

// tripleEventTypes carry one edge each as a predicate IRI frozen at write
// time. --restamp moves them alongside the document they belong to, because
// a reprojection folds them back into the document through AddEdgeToGraph:
// left on the old IRI they would re-add the edge under a key the re-stamped
// context no longer names.
var tripleEventTypes = []string{"Triple.Created", "Triple.Deleted"}

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
	ID            string
	AggregateID   string
	EventType     string
	SequenceNo    int
	TransactionID string
	Position      int64
	Payload       json.RawMessage
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
	return readEventsAfter(ctx, db, after, batch, edgeCarryingEventTypes)
}

func readEventsAfter(ctx context.Context, db *gorm.DB, after int64, batch int, types []string) ([]eventRow, error) {
	q := db.WithContext(ctx).Model(&eventRow{}).
		Where("position > ? AND event_type IN ?", after, types)
	if db.Name() == "postgres" {
		q = q.Where("xact_id < pg_snapshot_xmin(pg_current_snapshot())")
	}
	var rows []eventRow
	if err := q.Order("position ASC").Limit(batch).Find(&rows).Error; err != nil {
		return nil, err
	}
	// A batch never ends inside a transaction: a document and the Triple.*
	// events written with it must land in one commit, or a run interrupted
	// between two batches would leave the triples stranded on the old
	// predicate with nothing left to say they moved.
	if len(rows) == batch && rows[len(rows)-1].TransactionID != "" {
		last := rows[len(rows)-1]
		tailQ := db.WithContext(ctx).Model(&eventRow{}).
			Where("position > ? AND transaction_id = ? AND event_type IN ?", last.Position, last.TransactionID, types)
		if db.Name() == "postgres" {
			tailQ = tailQ.Where("xact_id < pg_snapshot_xmin(pg_current_snapshot())")
		}
		var tail []eventRow
		if err := tailQ.Order("position ASC").Find(&tail).Error; err != nil {
			return nil, err
		}
		rows = append(rows, tail...)
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
		rt:      rt,
		write:   opts.Write,
		restamp: opts.Restamp,
		report: NormalizeEdgeKeysReport{DryRun: !opts.Write, Restamp: opts.Restamp,
			Types: map[string]*EdgeKeyTypeReport{}},
		types:          newResourceTypeIndex(rt.RTRepo, rt.Links, rt.Logger),
		predicateMoves: map[string]map[string]string{},
		only:           map[string]bool{},
	}
	for _, slug := range opts.Types {
		run.only[slug] = true
	}

	types := edgeCarryingEventTypes
	if opts.Restamp {
		types = append(append([]string{}, edgeCarryingEventTypes...), tripleEventTypes...)
	}
	var last int64
	for {
		rows, err := readEventsAfter(ctx, rt.DB, last, batch, types)
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
			"scanned", run.report.Scanned, "rewritten", run.report.Rewritten,
			"restamped", run.report.Restamped, "dryRun", !opts.Write)
	}
	return run.report, nil
}

// edgeKeyRun carries one run's state across batches.
type edgeKeyRun struct {
	rt      NormalizeEdgeKeysRuntime
	write   bool
	restamp bool
	only    map[string]bool // type slugs to process; empty = all
	report  NormalizeEdgeKeysReport
	types   *resourceTypeIndex
	// predicateMoves records, per aggregate, each predicate a re-stamp moved
	// (old IRI → new IRI), so the aggregate's Triple.* events — which come
	// after the document in position order — move with it.
	predicateMoves map[string]map[string]string
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
		// An orphan event whose aggregate id names no resource type (a
		// page's five-part URN, say) with no Created event in the feed to
		// say what it is. Counted, so the operator sees it.
		if strings.HasPrefix(row.EventType, "Triple.") {
			run.skip("triple event names no resource type")
		} else {
			run.skip("event names no resource type")
		}
		return nil, nil
	}
	if len(run.only) > 0 && !run.only[slug] {
		return nil, nil // out of the operator's scope; not read, not counted
	}
	if strings.HasPrefix(row.EventType, "Triple.") {
		return run.restampTriple(row, payload, slug), nil
	}
	run.report.Scanned++
	tr := run.typeCount(slug)
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
	// The document's own context is read BEFORE anything rewrites it: the
	// predicate a stored edge carries today is what its Triple.* events
	// carry, and it is the old side of every move.
	oldEmbedded, _ := json.Marshal(data["@context"])
	changed, problems, names := normalizeEdgeKeys(data, resolver)
	restamped := false
	if run.restamp && resolver != nil && len(problems) == 0 {
		// A document with an edge the run declined keeps its own context:
		// re-stamping it would erase the only mapping that still names the
		// declined IRI, and with it the operator's chance to fix and re-run.
		moves := predicateMovesOf(names, oldEmbedded, resolver)
		// A key rewrite already replaced the context; that IS a re-stamp,
		// and the operator reading "re-stamped N" must see it counted.
		restamped = restampDocument(data, resolver) || changed
		if len(moves) > 0 {
			// Merged, not replaced: a Triple.Deleted for an edge the Created
			// carried can follow an Updated that no longer does, and it must
			// still move.
			all := run.predicateMoves[row.AggregateID]
			if all == nil {
				all = map[string]string{}
				run.predicateMoves[row.AggregateID] = all
			}
			for old, next := range moves {
				all[old] = next
			}
		}
	}
	for i := range problems {
		problems[i].TypeSlug = slug
		problems[i].ResourceID = row.AggregateID
		problems[i].EventID = row.ID
		problems[i].Position = row.Position
		run.record(problems[i])
	}
	if changed {
		tr.Rewritten++
		run.report.Rewritten++
	}
	if restamped {
		tr.Restamped++
		run.report.Restamped++
	}
	if !changed && !restamped {
		return nil, nil
	}
	payload["Data"] = data
	return pericarpinfra.JSONB(payload), nil
}

// restampDocument brings a stored document's embedded @context and its
// entity node's @type up to what BuildResourceGraph writes for the type
// today, and reports whether anything moved. It touches nothing else: the
// entity's properties and the edges node are exactly as they were.
//
// This exists because a reprojection cannot do it. worker reproject replays
// each Resource.Created/Updated payload verbatim, and the class and the
// predicates the knowledge graph derives come from the payload's OWN
// @context, stamped at write time — so a term or prefix the type's context
// has since moved stays where it was for every resource written before the
// move, no matter how often the feed is replayed.
func restampDocument(data map[string]any, resolver *edgeKeyResolver) bool {
	moved := false
	if resolver.storable != nil && !jsonEquivalentAny(data["@context"], resolver.storable) {
		data["@context"] = resolver.storable
		moved = true
	}
	graph, _ := data["@graph"].([]any)
	if len(graph) > 0 {
		if entity, ok := graph[0].(map[string]any); ok {
			typ, isString := entity["@type"].(string)
			_, present := entity["@type"]
			// Compared as the CLASS the document derives, not as text: an
			// entity written with the class spelled out in full names the
			// same class as one written with the compact form. A @type that
			// is not a single string (an array of classes) is left alone —
			// collapsing it would drop classes the document asserts.
			if (isString || !present) && typ != resolver.schemaType &&
				expandClass(typ, resolver.storable) != expandClass(resolver.schemaType, resolver.storable) {
				entity["@type"] = resolver.schemaType
				moved = true
			}
		}
	}
	return moved
}

// expandClass expands an entity @type through a storable context.
func expandClass(typ string, storable any) string {
	if typ == "" {
		return ""
	}
	raw, _ := json.Marshal(storable)
	vocab, _ := jsonld.ParseContext(raw)
	var ctx map[string]any
	if json.Unmarshal(raw, &ctx) != nil {
		if s, ok := storable.(string); ok {
			vocab = s
		}
	}
	return jsonld.ExpandIRI(typ, vocab, ctx)
}

// predicateMovesOf lists the predicates a re-stamp of this document moves:
// for each edge the resolver answered with exactly one property, the
// predicate the stored key carried (an IRI key is its own predicate; a
// compact key resolves through the document's OWN context as it stood)
// against the same property resolved through the type's current context.
// Edges the resolver declined are not in names and never move.
func predicateMovesOf(
	names map[string]string, oldEmbedded json.RawMessage, resolver *edgeKeyResolver,
) map[string]string {
	if len(names) == 0 {
		return nil
	}
	oldVocab, oldTerms := jsonld.ParseContext(oldEmbedded)
	if oldVocab == "" && len(oldTerms) == 0 {
		var bare string
		if json.Unmarshal(oldEmbedded, &bare) == nil {
			oldVocab = bare
		}
	}
	newVocab, newTerms := jsonld.ParseContext(resolver.ldContext)
	moves := map[string]string{}
	for key, property := range names {
		old := key
		if !jsonld.IsIRIKey(key) {
			old = jsonld.ResolvePredicateIRI(key, oldVocab, oldTerms)
		}
		if next := jsonld.ResolvePredicateIRI(property, newVocab, newTerms); next != old {
			moves[old] = next
		}
	}
	return moves
}

// restampTriple moves a Triple.* event's predicate when the document it
// belongs to was re-stamped, and returns the rewritten payload or nil.
func (run *edgeKeyRun) restampTriple(row *eventRow, payload map[string]any, slug string) pericarpinfra.JSONB {
	moves := run.predicateMoves[row.AggregateID]
	predicate, _ := payload["predicate"].(string)
	next, moved := moves[predicate]
	if !moved {
		return nil
	}
	payload["predicate"] = next
	run.typeCount(slug).TriplesMoved++
	run.report.TriplesMoved++
	return pericarpinfra.JSONB(payload)
}

func (run *edgeKeyRun) typeCount(slug string) *EdgeKeyTypeReport {
	tr := run.report.Types[slug]
	if tr == nil {
		tr = &EdgeKeyTypeReport{}
		run.report.Types[slug] = tr
	}
	return tr
}

// jsonEquivalentAny compares two decoded JSON values by their canonical
// encoding, so a map read with json.Number and one built in Go compare
// equal when they mean the same document.
func jsonEquivalentAny(a, b any) bool {
	ea, errA := json.Marshal(a)
	eb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ea, eb)
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
		resolver = newEdgeKeyResolver(rtype.Context(), rtype.Schema(), ix.links.BySource(slug), rtype.Name())
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
	// schemaType is the entity @type a fresh write of this type carries:
	// the context's "@type" when it names one, else the type's name.
	schemaType string
}

func newEdgeKeyResolver(
	ldContext, schema json.RawMessage, links []PresetLinkDefinition, typeName string,
) *edgeKeyResolver {
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
	// An alias keyed by a control keyword is not a property's (issue #522).
	for name, iris := range jsonld.TermAliases(ldContext) {
		if jsonld.ControlKeywords[name] || jsonld.IsIRIKey(name) {
			continue
		}
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
	out.schemaType = typeName
	if len(ldContext) > 0 {
		out.storable = buildStorableContext(ldContext)
		var raw map[string]any
		if json.Unmarshal(ldContext, &raw) == nil {
			if ct, ok := raw["@type"].(string); ok && ct != "" {
				out.schemaType = ct
			}
		}
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
// It also returns, for every edge key the resolver answered with exactly one
// property name — rewritten IRI keys and compact keys alike — that name, so
// a re-stamp can move the aggregate's Triple.* predicates from the same
// answers rather than from a second, looser resolution.
func normalizeEdgeKeys(data map[string]any, resolver *edgeKeyResolver) (bool, []EdgeKeyProblem, map[string]string) {
	edges := storedEdgesNode(data)
	if edges == nil {
		return false, nil, nil
	}
	keys := iriEdgeKeys(edges)
	names := map[string]string{}
	for key := range edges {
		if key != "@id" && !jsonld.IsIRIKey(key) {
			names[key] = key
		}
	}

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
			names[key] = name
			changed = true
		}
	}
	if !changed {
		return false, problems, names
	}
	if resolver.storable != nil {
		data["@context"] = resolver.storable
	}
	return true, problems, names
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
