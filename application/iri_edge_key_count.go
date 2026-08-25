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
	"sort"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	gormprov "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/infrastructure/logging"
	"github.com/wepala/weos/v3/infrastructure/models"
	"github.com/wepala/weos/v3/internal/config"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// This file is the read-only check behind `weos worker count-iri-edge-keys`
// (issue #519): how many stored resources still key an edge by predicate IRI,
// the pre-#515 form the normalization (#523) rewrites.
//
// Neither #515 nor #523 forces the old shape out on its own — every reader
// accepts both key forms indefinitely — so an instance can sit half-migrated
// (a type the migration declined, a restored backup, an import from an old
// instance) and nothing says so. This check says so, and keeps saying so: it
// PASSES only when both surfaces count zero, and the command exits non-zero
// otherwise, so a runbook step or a cron can gate on it.
//
// Two surfaces are counted separately because they disagree for a real
// window of the migration: after `normalize-edge-keys --write` the EVENTS are
// clean while the CANONICAL RECORDS — what every reader serves, held in the
// generic `resources` table — stay on the old shape until `worker reproject`
// replays them. An operator who declared victory in that window would be
// wrong, and the report is built to show the window rather than average it
// away.
//
// The unit of the headline number is a RESOURCE: a resource with a Created
// and an Updated event counts once, as does one with three IRI-keyed edges.
// The classification (resolvable / ambiguous / unmapped) is per edge key,
// taken from the events surface with the very resolver #523 uses, so the
// count predicts exactly what the migration will and will not rewrite.

// IRIEdgeKeyCountRuntime bundles what CountIRIEdgeKeys needs; populate it
// from an fx app built with IRIEdgeKeyCountModule.
type IRIEdgeKeyCountRuntime struct {
	fx.In
	DB     *gorm.DB
	RTRepo repositories.ResourceTypeRepository
	Links  *LinkRegistry
	Logger entities.Logger
}

// IRIEdgeKeyCountModule is the narrow, read-only assembly behind the command.
// Like NormalizeEdgeKeysModule it stays clear of application.Module, whose
// startup reconcile appends ResourceType events — a check that writes even
// one event is not read-only.
func IRIEdgeKeyCountModule(cfg config.Config, registry *PresetRegistry) fx.Option {
	if registry == nil {
		panic("application.IRIEdgeKeyCountModule: PresetRegistry must not be nil")
	}
	return fx.Module("count-iri-edge-keys",
		fx.Provide(func() config.Config { return cfg }),
		fx.Provide(func() *PresetRegistry { return registry }),
		fx.Provide(logging.ProvideZapLogger),
		fx.Provide(logging.ProvideLogger),
		fx.Provide(gormprov.ProvideGormDB),
		fx.Provide(gormprov.ProvideEventStore),
		fx.Invoke(func(pericarpdomain.EventStore) {}),
		fx.Provide(gormprov.ProvideResourceTypeRepository),
		fx.Provide(func(r *PresetRegistry, logger entities.Logger) *LinkRegistry {
			return buildLinkRegistry(r, logger)
		}),
	)
}

// IRIEdgeKeyCountOptions tune a run.
type IRIEdgeKeyCountOptions struct {
	// BatchSize is rows per read on either surface; <= 0 uses 500.
	BatchSize int
	// RecordsOnly skips the events surface. The events matter until the
	// normalization has run; afterwards the canonical records are the cheap,
	// steady-state gate, and a scheduled check need not re-read the whole
	// history every time.
	RecordsOnly bool
}

// ErrNothingToCheck is returned when the store holds no resource type and no
// event at all. An empty store cannot pass a check — it is far more likely
// the command opened the wrong database (a mistyped DSN creates an empty
// SQLite file) than that an instance has nothing in it.
var ErrNothingToCheck = errors.New("count-iri-edge-keys: the store holds no resource type and no event; " +
	"is DATABASE_DSN pointing at the right database?")

// EdgeKeyClass is how an IRI-keyed edge would fare under normalization.
type EdgeKeyClass string

const (
	// EdgeKeyResolvable — exactly one property claims the predicate; #523
	// rewrites it.
	EdgeKeyResolvable EdgeKeyClass = "resolvable"
	// EdgeKeyAmbiguous — more than one name claims the predicate; #523
	// declines it and names the candidates.
	EdgeKeyAmbiguous EdgeKeyClass = "ambiguous"
	// EdgeKeyUnmapped — no term, alias or @vocab prefix names it, including
	// every edge of a type the store no longer holds; #523 declines it.
	EdgeKeyUnmapped EdgeKeyClass = "unmapped"
)

// EdgeKeyClassification is one IRI-keyed edge on the events surface, counted
// once per resource and key.
type EdgeKeyClassification struct {
	TypeSlug   string
	ResourceID string
	Key        string
	Class      EdgeKeyClass
	Candidates []string
}

// IRIEdgeKeyTypeCount is one resource type's share of the count.
type IRIEdgeKeyTypeCount struct {
	// Events is the resources with at least one IRI-keyed edge in a
	// Resource.Created or Resource.Updated event.
	Events int
	// Records is the resources whose canonical record keys an edge by IRI.
	Records int
	// Resolvable, Ambiguous and Unmapped count distinct (resource, key)
	// pairs on the events surface by class.
	Resolvable, Ambiguous, Unmapped int
	// Residue is the resources holding at least one ambiguous or unmapped
	// key — what normalization will leave IRI-keyed.
	Residue int
	// Orphaned is the resources of this type whose events still key an edge
	// by IRI while the type itself is no longer stored. Nothing can rewrite
	// them and nothing reads them; they are reported, not gated on.
	Orphaned int
}

// IRIEdgeKeyCountReport is what a run found.
type IRIEdgeKeyCountReport struct {
	Types map[string]*IRIEdgeKeyTypeCount
	// EventsTotal, RecordsTotal, ResidueTotal and OrphanedTotal sum the
	// per-type counts. Orphaned resources are excluded from EventsTotal.
	EventsTotal, RecordsTotal, ResidueTotal, OrphanedTotal int
	// RecordsOnly records that the events surface was not scanned.
	RecordsOnly bool
	// Classified lists the first MaxReportedProblems non-resolvable edges,
	// so the operator can find them; ClassifiedTotal counts them all.
	Classified      []EdgeKeyClassification
	ClassifiedTotal int
	// Skipped counts events or records the run could not read, by reason.
	Skipped map[string]int
}

// Passes reports whether both scanned surfaces are clean. Orphaned resources
// — IRI-keyed events of a type the store no longer holds — do not fail the
// check: nothing can rewrite them, nothing serves them, and a gate that can
// never go green is one nobody keeps.
func (r IRIEdgeKeyCountReport) Passes() bool { return r.EventsTotal == 0 && r.RecordsTotal == 0 }

// TypeSlugs returns the report's type slugs in a stable order.
func (r IRIEdgeKeyCountReport) TypeSlugs() []string {
	slugs := make([]string, 0, len(r.Types))
	for slug := range r.Types {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

// CountIRIEdgeKeys scans the events surface and the canonical-record surface
// and reports, per type and in total, how many resources still key an edge
// by predicate IRI. It writes nothing.
func CountIRIEdgeKeys(
	ctx context.Context, rt IRIEdgeKeyCountRuntime, opts IRIEdgeKeyCountOptions,
) (IRIEdgeKeyCountReport, error) {
	batch := opts.BatchSize
	if batch <= 0 {
		batch = 500
	}
	run := &iriEdgeKeyCount{
		rt:     rt,
		report: IRIEdgeKeyCountReport{Types: map[string]*IRIEdgeKeyTypeCount{}, RecordsOnly: opts.RecordsOnly},
		types:  newResourceTypeIndex(rt.RTRepo, rt.Links, rt.Logger),
		seen:   map[string]*seenResource{},
	}
	if err := run.refuseEmptyStore(ctx); err != nil {
		return run.report, err
	}
	if !opts.RecordsOnly {
		if err := run.scanEvents(ctx, batch); err != nil {
			return run.report, err
		}
	}
	if err := run.scanRecords(ctx, batch); err != nil {
		return run.report, err
	}
	run.total()
	return run.report, nil
}

// refuseEmptyStore guards the gate against passing on a database that is not
// the instance's — see ErrNothingToCheck.
func (run *iriEdgeKeyCount) refuseEmptyStore(ctx context.Context) error {
	var types, events int64
	if err := run.rt.DB.WithContext(ctx).Model(&models.ResourceType{}).Count(&types).Error; err != nil {
		return fmt.Errorf("count-iri-edge-keys: count resource types: %w", err)
	}
	if err := run.rt.DB.WithContext(ctx).Model(&eventRow{}).Count(&events).Error; err != nil {
		return fmt.Errorf("count-iri-edge-keys: count events: %w", err)
	}
	if types == 0 && events == 0 {
		return ErrNothingToCheck
	}
	return nil
}

// seenResource is one resource met on the events surface: its type, and
// each IRI key classified once.
type seenResource struct {
	slug     string
	orphaned bool
	byKey    map[string]EdgeKeyClass
}

type iriEdgeKeyCount struct {
	rt     IRIEdgeKeyCountRuntime
	report IRIEdgeKeyCountReport
	types  *resourceTypeIndex
	// seen maps resource id → what the events surface learned about it, so
	// a key met on a Created and again on an Updated event is classified and
	// counted once.
	seen map[string]*seenResource
}

func (run *iriEdgeKeyCount) typeCount(slug string) *IRIEdgeKeyTypeCount {
	t := run.report.Types[slug]
	if t == nil {
		t = &IRIEdgeKeyTypeCount{}
		run.report.Types[slug] = t
	}
	return t
}

func (run *iriEdgeKeyCount) skip(reason string) {
	if run.report.Skipped == nil {
		run.report.Skipped = map[string]int{}
	}
	run.report.Skipped[reason]++
}

// scanEvents walks Resource.Created/Updated in position order, exactly as the
// normalization does, classifying each IRI key once per resource.
func (run *iriEdgeKeyCount) scanEvents(ctx context.Context, batch int) error {
	var last int64
	for {
		var rows []eventRow
		err := run.rt.DB.WithContext(ctx).Model(&eventRow{}).
			Where("position > ? AND event_type IN ?", last, edgeCarryingEventTypes).
			Order("position ASC").Limit(batch).Find(&rows).Error
		if err != nil {
			return fmt.Errorf("count-iri-edge-keys: read events after position %d: %w", last, err)
		}
		if len(rows) == 0 {
			return nil
		}
		for i := range rows {
			if err := run.countEvent(ctx, &rows[i]); err != nil {
				return err
			}
		}
		last = rows[len(rows)-1].Position
	}
}

func (run *iriEdgeKeyCount) countEvent(ctx context.Context, row *eventRow) error {
	payload, err := decodePayload(row.Payload)
	if err != nil {
		run.skip("event payload is not a JSON object")
		return nil
	}
	slug := run.types.slugFor(row, payload)
	if slug == "" {
		run.skip("event names no resource type")
		return nil
	}
	data, ok := payload["Data"].(map[string]any)
	if !ok {
		run.skip("event Data is not a JSON object")
		return nil
	}
	keys := iriEdgeKeys(storedEdgesNode(data))
	if len(keys) == 0 {
		return nil
	}
	resolver, err := run.types.resolverFor(ctx, slug)
	if err != nil {
		return fmt.Errorf("count-iri-edge-keys: %w", err)
	}
	t := run.typeCount(slug)
	seen := run.seen[row.AggregateID]
	if seen == nil {
		seen = &seenResource{slug: slug, orphaned: resolver == nil, byKey: map[string]EdgeKeyClass{}}
		run.seen[row.AggregateID] = seen
		if seen.orphaned {
			t.Orphaned++
		} else {
			t.Events++
		}
	}
	if seen.orphaned {
		return nil // nothing can classify, rewrite or read these; counted once above
	}
	for _, key := range keys {
		if _, done := seen.byKey[key]; done {
			continue
		}
		class, candidates := classifyEdgeKey(resolver, key)
		seen.byKey[key] = class
		run.recordClass(t, slug, row.AggregateID, key, class, candidates)
	}
	return nil
}

// classifyEdgeKey answers how one IRI key would fare under normalization,
// through the same resolver #523 rewrites with.
func classifyEdgeKey(resolver *edgeKeyResolver, key string) (EdgeKeyClass, []string) {
	_, candidates, ok := resolver.resolve(key)
	switch {
	case len(candidates) > 1:
		return EdgeKeyAmbiguous, candidates
	case ok:
		return EdgeKeyResolvable, nil
	default:
		return EdgeKeyUnmapped, nil
	}
}

func (run *iriEdgeKeyCount) recordClass(
	t *IRIEdgeKeyTypeCount, slug, resourceID, key string, class EdgeKeyClass, candidates []string,
) {
	switch class {
	case EdgeKeyResolvable:
		t.Resolvable++
		return
	case EdgeKeyAmbiguous:
		t.Ambiguous++
	default:
		t.Unmapped++
	}
	run.report.ClassifiedTotal++
	if len(run.report.Classified) < MaxReportedProblems {
		run.report.Classified = append(run.report.Classified, EdgeKeyClassification{
			TypeSlug: slug, ResourceID: resourceID, Key: key, Class: class, Candidates: candidates})
	}
}

// canonicalRow reads the columns the records surface needs from the generic
// resources table — the canonical store every reader serves from.
type canonicalRow struct {
	ID       string
	TypeSlug string
	Data     string
}

// scanRecords walks the canonical records, live rows only, and counts those
// whose stored document keys an edge by IRI.
func (run *iriEdgeKeyCount) scanRecords(ctx context.Context, batch int) error {
	last := ""
	for {
		var rows []canonicalRow
		err := run.rt.DB.WithContext(ctx).Model(&models.Resource{}).
			Select("id", "type_slug", "data").
			Where("id > ? AND deleted_at IS NULL", last).
			Order("id ASC").Limit(batch).Scan(&rows).Error
		if err != nil {
			return fmt.Errorf("count-iri-edge-keys: read canonical records after %q: %w", last, err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			run.countRecord(row)
		}
		last = rows[len(rows)-1].ID
	}
}

func (run *iriEdgeKeyCount) countRecord(row canonicalRow) {
	var data map[string]any
	if err := json.Unmarshal([]byte(row.Data), &data); err != nil {
		run.skip("canonical record is not a JSON object")
		return
	}
	if len(iriEdgeKeys(storedEdgesNode(data))) == 0 {
		return
	}
	run.typeCount(row.TypeSlug).Records++
}

func (run *iriEdgeKeyCount) total() {
	residueOf := map[string]map[string]struct{}{}
	for resourceID, seen := range run.seen {
		for _, class := range seen.byKey {
			if class == EdgeKeyResolvable {
				continue
			}
			if residueOf[seen.slug] == nil {
				residueOf[seen.slug] = map[string]struct{}{}
			}
			residueOf[seen.slug][resourceID] = struct{}{}
		}
	}
	for slug, t := range run.report.Types {
		t.Residue = len(residueOf[slug])
		run.report.EventsTotal += t.Events
		run.report.RecordsTotal += t.Records
		run.report.ResidueTotal += t.Residue
		run.report.OrphanedTotal += t.Orphaned
	}
}
