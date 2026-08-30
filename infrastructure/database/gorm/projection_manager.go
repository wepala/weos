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

package gorm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
	"github.com/wepala/weos/v3/pkg/jsonld"
	"github.com/wepala/weos/v3/pkg/utils"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"github.com/jinzhu/inflection"
	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type columnDef struct {
	Name    string
	SQLType string
	// Derived marks a column the projection writes but no document declares —
	// today only the `<fk>_display` sibling of a reference property. Absence of
	// a derived column from a write means "nothing to denormalize here", never
	// "the client cleared it", so nullClearedColumns must not touch one.
	Derived bool
}

// standardColumnNames lists column names already part of the projection table DDL
// and should be skipped when extracting columns from JSON Schema.
var standardColumnNames = map[string]bool{
	"id":          true,
	"type_slug":   true,
	"data":        true,
	"status":      true,
	"created_by":  true,
	"account_id":  true,
	"sequence_no": true,
	"created_at":  true,
	"updated_at":  true,
}

// jsonLDKeys are skipped when extracting columns from JSON Schema.
var jsonLDKeys = map[string]bool{
	"@id":      true,
	"@type":    true,
	"@context": true,
}

type tableInfo struct {
	name    string
	context json.RawMessage
	columns map[string]bool // cached column names for fast lookup
	// declared holds the subset of columns that come from a declared property —
	// every schema property plus every activated link FK, and never a derived
	// `<fk>_display` sibling or a standard column. See DeclaredColumns.
	declared map[string]bool
}

type projectionManager struct {
	db          *gorm.DB
	logger      entities.Logger
	tables      sync.Map   // slug → tableInfo
	reverseRe   sync.Map   // targetTypeSlug → []repositories.ReverseReference
	forwardRe   sync.Map   // referencingTypeSlug → []repositories.ForwardReference
	reverseReMu sync.Mutex // guards reverseRe AND forwardRe writes (symmetric)
	parentOf    sync.Map   // slug → parentSlug (from rdfs:subClassOf, for ancestor chain)
	// linkSource replays link-declared refs after registerReverseReferences
	// clears a slug's entries. Optional — nil means link refs are only set by
	// RegisterLink and will be wiped on schema re-parse.
	linkSource repositories.LinkSource
}

type ProjectionManagerResult struct {
	fx.Out
	ProjectionManager repositories.ProjectionManager
}

func ProvideProjectionManager(params struct {
	fx.In
	DB         *gorm.DB
	Logger     entities.Logger
	LinkSource repositories.LinkSource `optional:"true"`
}) ProjectionManagerResult {
	return ProjectionManagerResult{
		ProjectionManager: &projectionManager{
			db:         params.DB,
			logger:     params.Logger,
			linkSource: params.LinkSource,
		},
	}
}

func (pm *projectionManager) EnsureTable(
	ctx context.Context, slug string, schema, ldContext json.RawMessage,
) error {
	tableName := slugToTableName(slug)

	columns := schemaToColumns(schema)

	if err := pm.createTableIfNotExists(ctx, tableName, columns); err != nil {
		return fmt.Errorf("failed to ensure projection table %q: %w", tableName, err)
	}

	// Reconcile the base columns even when the table already existed. The
	// CREATE above lists them, but CREATE ... IF NOT EXISTS is a no-op on a
	// table another owner made first — e.g. pericarp auth's `agents` table,
	// which the ValueFlows `agent` type's projection shares by name — so
	// without this a projection write to that table fails on the first
	// missing base column (account_id). addMissingColumns is idempotent
	// (HasColumn-guarded), so this is a cheap ALTER only when needed.
	if err := pm.addMissingColumns(ctx, tableName, pm.baseColumnDefs()); err != nil {
		return fmt.Errorf("failed to ensure base columns on %q: %w", tableName, err)
	}

	if err := pm.addMissingColumns(ctx, tableName, columns); err != nil {
		return fmt.Errorf("failed to add columns to %q: %w", tableName, err)
	}

	colSet := make(map[string]bool, len(columns)+len(standardColumnNames))
	for col := range standardColumnNames {
		if col != "data" { // data lives in resources table, not projection
			colSet[col] = true
		}
	}
	declaredSet := make(map[string]bool, len(columns))
	for _, col := range columns {
		colSet[col.Name] = true
		if !col.Derived {
			declaredSet[col.Name] = true
		}
	}
	// Re-add previously activated link columns so a schema re-parse doesn't
	// silently drop them from the cache. Schema-derived columns alone won't
	// include RegisterLink-added FK/_display columns, but they still exist in
	// the DB — only add what the migrator confirms, so we don't claim cached
	// presence for a link that hasn't been activated yet.
	if pm.linkSource != nil {
		migrator := pm.db.Migrator()
		for _, ref := range pm.linkSource.LinkReferencesForSource(slug) {
			colName := utils.CamelToSnake(ref.PropertyName)
			displayCol := colName + "_display"
			if migrator.HasColumn(tableName, colName) {
				colSet[colName] = true
				// A link FK is declared, just outside the type's own schema: a
				// document carries it in its edges node exactly as it carries an
				// x-resource-type reference, so clearing it must work the same way.
				declaredSet[colName] = true
			}
			if migrator.HasColumn(tableName, displayCol) {
				colSet[displayCol] = true
			}
		}
	}
	// Cache the table as healthy BEFORE backfilling — backfillFromCanonical
	// reads this entry (via HasColumn) to decide which extracted keys map to
	// real columns. If the backfill then errors we invalidate the entry below,
	// so the lazy HasProjectionTable path re-runs EnsureTable next call rather
	// than trusting a table that never got its pre-existing rows filled.
	pm.tables.Store(slug, tableInfo{
		name: tableName, context: ldContext, columns: colSet, declared: declaredSet,
	})
	if parentSlug := jsonld.SubClassOf(ldContext); parentSlug != "" {
		pm.parentOf.Store(slug, parentSlug)
	} else {
		pm.parentOf.Delete(slug)
	}
	pm.registerReverseReferences(slug, schema)

	// Backfill any canonical rows that predate this projection table. In steady
	// state the anti-join returns nothing, so this is a cheap no-op; it only does
	// work when a type's projection table is created after resources of that type
	// already exist (e.g. a preset installed, or a lazily-created table for a type
	// whose rows were written before EnsureTable first ran for it).
	skipped, err := pm.backfillFromCanonical(ctx, slug, tableName, ldContext)
	if err != nil {
		// A backfill query failure (not a per-row skip — those are tolerated
		// below) means we can't vouch for this table's completeness. Drop the
		// cache entry so the lazy HasProjectionTable path retries instead of
		// caching a half-filled table as healthy for the process lifetime.
		pm.tables.Delete(slug)
		return fmt.Errorf("failed to backfill projection table %q (skipped %d rows before failure): %w",
			tableName, skipped, err)
	}
	if skipped > 0 {
		// Partial backfill: the healthy rows are in, but some legacy rows could
		// not be projected. Surface the count so an operator can find and repair
		// them; the per-row cause was logged as each was skipped.
		pm.logger.Warn(ctx, "projection backfill completed with skipped rows",
			"table", tableName, "slug", slug, "skipped", skipped)
	}
	return nil
}

// backfillBatchSize bounds each backfill scan so a type with a large canonical
// backlog is filled in bounded rounds rather than one unbounded query.
const backfillBatchSize = 500

// backfillFromCanonical populates the projection table with any canonical
// `resources` rows of type slug that have no matching projection row yet. It
// walks the canonical rows in keyset order (ORDER BY id, cursor id > lastID)
// using a NOT EXISTS anti-join — supported identically by SQLite and Postgres.
// In steady state the first round's anti-join returns nothing and the function
// is a no-op.
//
// Keyset (not shrink-by-anti-join) pagination is deliberate: a row that fails
// to insert is skipped rather than aborting the batch (see below), but a
// skipped row still has no projection row, so a shrink-by-anti-join scan would
// hand it back on every subsequent round and spin forever on a full batch of
// poisoned rows. Advancing a cursor past each batch's last id guarantees every
// canonical row is visited exactly once and the walk terminates.
//
// Per-row inserts that fail (e.g. a legacy payload whose value violates a
// column constraint) are logged and skipped, not fatal, so one poisoned row
// can't starve every healthy row behind it. The count of skipped rows is
// returned so EnsureTable can surface it to operators. A returned error means
// a batch-level query failure (the scan itself), which is not tolerated.
//
// Rows are inserted with ON CONFLICT (id) DO NOTHING so a row written
// concurrently by the synchronous save path is never clobbered. Display columns
// are deliberately left unpopulated — a NULL display is a documented-tolerated
// state (see populateDisplayColumns) that self-heals on the next write or
// triple-propagation event — which also keeps this path free of the display
// lookup's account-scope dependencies.
func (pm *projectionManager) backfillFromCanonical(
	ctx context.Context, slug, tableName string, ldContext json.RawMessage,
) (int, error) {
	// No canonical store means nothing to backfill. In production the resources
	// table is always migrated before any type is ensured; this guard keeps the
	// path a clean no-op for callers (and tests) that ensure tables in isolation.
	if !pm.db.Migrator().HasTable("resources") {
		return 0, nil
	}

	antiJoin := fmt.Sprintf(
		"NOT EXISTS (SELECT 1 FROM %s p WHERE p.id = resources.id)", tableName)

	skipped := 0
	// Backfill never clears a column — it only inserts rows that have none —
	// so a partly-read document costs a stale column, not a lost value. Count
	// them and report once per run: a per-row warning over a drifted type
	// would print one line per record and bury the signal it exists to give.
	unreadable := 0
	reportUnreadable := func() {
		if unreadable > 0 {
			pm.logger.Warn(ctx, "projection backfill read some documents only in part",
				"slug", slug, "table", tableName, "rows", unreadable)
		}
	}
	lastID := ""
	for {
		q := pm.db.WithContext(ctx).
			Where("type_slug = ? AND deleted_at IS NULL", slug).
			Where(antiJoin)
		if lastID != "" {
			q = q.Where("resources.id > ?", lastID)
		}
		var batch []models.Resource
		if err := q.Order("resources.id").Limit(backfillBatchSize).Find(&batch).Error; err != nil {
			reportUnreadable()
			return skipped, err
		}
		if len(batch) == 0 {
			reportUnreadable()
			return skipped, nil
		}

		for i := range batch {
			res := &batch[i]
			row := map[string]any{
				"id":          res.ID,
				"type_slug":   res.TypeSlug,
				"status":      res.Status,
				"created_by":  res.CreatedBy,
				"account_id":  res.AccountID,
				"sequence_no": res.SequenceNo,
				"created_at":  res.CreatedAt,
			}
			// ExtractFlatColumns understands both legacy flat and @graph payloads.
			if report := ExtractFlatColumns(
				json.RawMessage(res.Data), ldContext, row,
			); !report.Complete() {
				unreadable++
			}
			// Drop keys with no column on this table (mirrors dropMissingColumns).
			for col := range row {
				if standardColumnNames[col] {
					continue
				}
				if !pm.HasColumn(slug, col) {
					delete(row, col)
				}
			}
			if err := pm.db.WithContext(ctx).Table(tableName).
				Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "id"}},
					DoNothing: true,
				}).Create(row).Error; err != nil {
				// Skip the poisoned row, keep going. The keyset cursor advances
				// past it below so it won't be re-scanned into an infinite loop.
				pm.logger.Warn(ctx, "projection backfill skipping row after insert failure",
					"id", res.ID, "table", tableName, "error", err)
				skipped++
				continue
			}
		}

		lastID = batch[len(batch)-1].ID
		if len(batch) < backfillBatchSize {
			reportUnreadable()
			return skipped, nil
		}
	}
}

// HasProjectionTable reports whether a projection table exists. Cached entries are not
// invalidated on type deletion; this is acceptable because type deletion is rare and
// projection tables are retained even after the type is soft-deleted (for data access).
func (pm *projectionManager) HasProjectionTable(slug string) bool {
	if _, ok := pm.tables.Load(slug); ok {
		return true
	}
	// Lazy creation: another process may have created the type after startup.
	var rt models.ResourceType
	if err := pm.db.Where("slug = ? AND deleted_at IS NULL", slug).First(&rt).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			pm.logger.Warn(context.Background(), "failed to look up resource type for projection",
				"slug", slug, "error", err)
		}
		return false
	}
	var schema json.RawMessage
	if rt.Schema != "" {
		schema = json.RawMessage(rt.Schema)
	}
	var ldContext json.RawMessage
	if rt.Context != "" {
		ldContext = json.RawMessage(rt.Context)
	}
	if err := pm.EnsureTable(context.Background(), slug, schema, ldContext); err != nil {
		pm.logger.Warn(context.Background(), "failed to lazily create projection table",
			"slug", slug, "error", err)
		return false
	}
	return true
}

func (pm *projectionManager) TableName(slug string) string {
	if v, ok := pm.tables.Load(slug); ok {
		if info, ok := v.(tableInfo); ok {
			return info.name
		}
	}
	return slugToTableName(slug)
}

func (pm *projectionManager) Context(slug string) json.RawMessage {
	if v, ok := pm.tables.Load(slug); ok {
		if info, ok := v.(tableInfo); ok {
			return info.context
		}
	}
	return nil
}

func (pm *projectionManager) HasColumn(slug, column string) bool {
	if v, ok := pm.tables.Load(slug); ok {
		if info, ok := v.(tableInfo); ok {
			return info.columns[column]
		}
	}
	return false
}

// DeclaredColumns returns the projection columns a document can carry for a
// slug — see the ProjectionManager interface for the contract. A fresh slice
// is returned on every call so a caller cannot mutate the cached set.
func (pm *projectionManager) DeclaredColumns(slug string) []string {
	v, ok := pm.tables.Load(slug)
	if !ok {
		return nil
	}
	info, ok := v.(tableInfo)
	if !ok {
		return nil
	}
	cols := make([]string, 0, len(info.declared))
	for col := range info.declared {
		cols = append(cols, col)
	}
	return cols
}

// writeDB returns the gorm handle a projection write must use. When a
// subscriber's batch transaction is in the context (pericarp injects it via
// HandlerContext), the write joins that transaction so it runs on the same
// connection — otherwise, under SQLite, a handler write on a pooled
// connection deadlocks against the batch's own write lock. Outside a batch
// (request-path writes, DDL, startup) TxFromContext is nil and we fall back
// to the pooled handle.
func (pm *projectionManager) writeDB(ctx context.Context) *gorm.DB {
	if tx := subscriptions.TxFromContext(ctx); tx != nil {
		return tx.WithContext(ctx)
	}
	return pm.db.WithContext(ctx)
}

func (pm *projectionManager) UpdateColumn(ctx context.Context, typeSlug, resourceID, column string, value any) error {
	if !pm.HasProjectionTable(typeSlug) {
		return nil
	}
	tableName := pm.TableName(typeSlug)
	if err := pm.writeDB(ctx).Table(tableName).
		Where("id = ?", resourceID).Update(column, value).Error; err != nil {
		return err
	}
	// Propagate to ancestor tables if the column exists there.
	// If the ancestor row doesn't exist (e.g., ancestor table added after resource creation),
	// the update is a no-op (0 rows affected), which is acceptable for display value propagation.
	for _, ancestorSlug := range pm.AncestorSlugs(typeSlug) {
		if !pm.HasProjectionTable(ancestorSlug) {
			continue
		}
		if !pm.HasColumn(ancestorSlug, column) {
			continue
		}
		aTable := pm.TableName(ancestorSlug)
		if err := pm.writeDB(ctx).Table(aTable).
			Where("id = ?", resourceID).Update(column, value).Error; err != nil {
			return err
		}
	}
	return nil
}

func (pm *projectionManager) UpdateColumnByFK(
	ctx context.Context, typeSlug, fkColumn, fkValue, targetColumn string, targetValue any,
) error {
	if !pm.HasProjectionTable(typeSlug) {
		return nil
	}
	tableName := pm.TableName(typeSlug)
	// A list reference stores its targets as a JSON array in the FK column
	// (issue #513), so plain equality never matches one and the denormalized
	// display value would go stale the moment the target was renamed — worse
	// than the empty column this replaced, because a stale name still reads as
	// current. The LIKE arm matches an array whose FIRST element is this
	// target, which is exactly the one the display column carries.
	// The pattern includes the CLOSING quote, so the quote is the token
	// boundary and one ID cannot match inside a longer one that starts with it.
	// Wildcards in the value itself are escaped: unescaped, a `%` would update
	// every row in the table.
	pattern := `["` + escapeLikeLiteral(fkValue) + `"%`
	return pm.writeDB(ctx).Table(tableName).
		Where(fkColumn+" = ? OR "+fkColumn+" LIKE ? ESCAPE '\\'", fkValue, pattern).
		Update(targetColumn, targetValue).Error
}

func (pm *projectionManager) ReverseReferences(targetTypeSlug string) []repositories.ReverseReference {
	if v, ok := pm.reverseRe.Load(targetTypeSlug); ok {
		if refs, ok := v.([]repositories.ReverseReference); ok {
			cp := make([]repositories.ReverseReference, len(refs))
			copy(cp, refs)
			return cp
		}
	}
	return nil
}

func (pm *projectionManager) ForwardReferences(typeSlug string) []repositories.ForwardReference {
	if v, ok := pm.forwardRe.Load(typeSlug); ok {
		if refs, ok := v.([]repositories.ForwardReference); ok {
			cp := make([]repositories.ForwardReference, len(refs))
			copy(cp, refs)
			return cp
		}
	}
	return nil
}

// AncestorSlugs returns the ordered chain of ancestor type slugs by walking
// rdfs:subClassOf relationships cached during EnsureTable.
func (pm *projectionManager) AncestorSlugs(slug string) []string {
	var chain []string
	visited := map[string]bool{slug: true}
	current := slug
	for {
		v, ok := pm.parentOf.Load(current)
		if !ok {
			break
		}
		parent, ok := v.(string)
		if !ok || parent == "" || visited[parent] {
			break
		}
		visited[parent] = true
		chain = append(chain, parent)
		current = parent
	}
	return chain
}

// registerReverseReferences parses a schema for x-resource-type properties and
// registers reverse-reference entries so that display value propagation can find
// which projection tables need updating when a target resource changes, and the
// symmetric forward-reference entries used to populate display columns on the
// referencing type's own projection row at write time.
//
// Schema-edit safety: stale entries from a previous registration of the same
// slug are *cleared* before new entries are added. Without this, removing or
// repointing an x-resource-type property would leave dangling refs in both
// maps — the old target's reverseRe bucket and the slug's forwardRe bucket
// would still claim the property exists. The clear+rebuild pass also makes
// the per-property dedup in the append helpers redundant for re-registrations
// (they now operate on a fresh state for this slug), but the helpers still
// dedup defensively against duplicate properties within a single schema.
func (pm *projectionManager) registerReverseReferences(slug string, schema json.RawMessage) {
	pm.reverseReMu.Lock()
	defer pm.reverseReMu.Unlock()

	// Clear any prior entries that name this slug — schema may have changed.
	pm.clearReferencesForSlugLocked(slug)

	// Track schema-declared refs by both PropertyName and derived FK column
	// name so the link replay can skip any entry that would override either
	// (schema wins on conflict). Keying on both catches a link that spells
	// the property differently but derives to the same column.
	schemaProps := make(map[string]bool)
	schemaCols := make(map[string]bool)

	if len(schema) > 0 {
		var s struct {
			Properties map[string]struct {
				XResourceType    string `json:"x-resource-type"`
				XDisplayProperty string `json:"x-display-property"`
			} `json:"properties"`
		}
		if json.Unmarshal(schema, &s) == nil {
			for propName, prop := range s.Properties {
				if prop.XResourceType == "" {
					continue
				}
				displayProp := prop.XDisplayProperty
				if displayProp == "" {
					displayProp = "name"
				}
				pm.registerRefLocked(slug, propName, prop.XResourceType, displayProp)
				schemaProps[propName] = true
				schemaCols[utils.CamelToSnake(propName)] = true
			}
		}
	}

	// Replay link-declared refs unconditionally — a re-parse with empty or
	// unparseable schema must not silently wipe RegisterLink-declared refs,
	// since they come from a separate source of truth. Any schema re-parse
	// (EnsureTable via ResourceType.Updated, or the lazy HasProjectionTable
	// path) would otherwise leave display propagation silently broken until
	// the next Reconcile. Conflicting entries (same property or same derived
	// column) are skipped — schema wins, matching the documented merge rule
	// in ExtractReferencePropertiesWithLinks.
	if pm.linkSource != nil {
		for _, ref := range pm.linkSource.LinkReferencesForSource(slug) {
			if schemaProps[ref.PropertyName] || schemaCols[utils.CamelToSnake(ref.PropertyName)] {
				continue
			}
			displayProp := ref.DisplayProperty
			if displayProp == "" {
				displayProp = "name"
			}
			pm.registerRefLocked(slug, ref.PropertyName, ref.TargetSlug, displayProp)
		}
	}
}

// registerRefLocked records one forward + one reverse reference entry for a
// reference from sourceSlug.<propertyName> → targetSlug. Shared by
// registerReverseReferences (schema-derived x-resource-type) and RegisterLink
// (link-registry-derived) so both paths funnel through the same dedup
// semantics. Caller must hold reverseReMu.
func (pm *projectionManager) registerRefLocked(
	sourceSlug, propertyName, targetSlug, displayProperty string,
) {
	colName := utils.CamelToSnake(propertyName)
	reverseRef := repositories.ReverseReference{
		ReferencingTypeSlug: sourceSlug,
		FKColumn:            colName,
		DisplayColumn:       colName + "_display",
		DisplayProperty:     displayProperty,
	}
	forwardRef := repositories.ForwardReference{
		FKColumn:        colName,
		DisplayColumn:   colName + "_display",
		TargetTypeSlug:  targetSlug,
		DisplayProperty: displayProperty,
	}
	pm.appendReverseRefLocked(targetSlug, reverseRef)
	pm.appendForwardRefLocked(sourceSlug, forwardRef)
}

// clearReferencesForSlugLocked removes every reference entry that names slug
// from both the forward and reverse maps. The forward bucket for slug is
// dropped wholesale; reverse buckets are walked and any entry whose
// ReferencingTypeSlug matches slug is filtered out (using copy-on-write so
// concurrent readers of the previous slice are unaffected). Caller must hold
// reverseReMu.
func (pm *projectionManager) clearReferencesForSlugLocked(slug string) {
	// Drop the forward bucket entirely — all forward refs for slug come from
	// its own schema, so they're all stale by definition on re-registration.
	pm.forwardRe.Delete(slug)

	// Walk reverse buckets and filter out any entries that name slug as the
	// referencer. Buckets keyed on different target types may contain refs
	// from many referencing types, so we can't drop them wholesale.
	pm.reverseRe.Range(func(key, value any) bool {
		refs, ok := value.([]repositories.ReverseReference)
		if !ok {
			return true
		}
		filtered := make([]repositories.ReverseReference, 0, len(refs))
		removed := false
		for _, r := range refs {
			if r.ReferencingTypeSlug == slug {
				removed = true
				continue
			}
			filtered = append(filtered, r)
		}
		if !removed {
			return true
		}
		if len(filtered) == 0 {
			pm.reverseRe.Delete(key)
		} else {
			pm.reverseRe.Store(key, filtered)
		}
		return true
	})
}

// appendReverseRefLocked appends a ReverseReference to the targetSlug bucket,
// replacing any existing entry keyed on (ReferencingTypeSlug, FKColumn). This
// lets a schema edit (e.g. a new x-display-property) take effect on the next
// EnsureTable instead of being silently dropped as a "duplicate". Caller must
// hold reverseReMu.
func (pm *projectionManager) appendReverseRefLocked(
	targetSlug string, ref repositories.ReverseReference,
) {
	existing, _ := pm.reverseRe.Load(targetSlug)
	var old []repositories.ReverseReference
	if existing != nil {
		old = existing.([]repositories.ReverseReference)
	}
	updated := make([]repositories.ReverseReference, 0, len(old)+1)
	for _, r := range old {
		if r.ReferencingTypeSlug == ref.ReferencingTypeSlug && r.FKColumn == ref.FKColumn {
			continue // drop stale entry — overwrite with the new one
		}
		updated = append(updated, r)
	}
	updated = append(updated, ref)
	pm.reverseRe.Store(targetSlug, updated)
}

// appendForwardRefLocked appends a ForwardReference to the referencingSlug
// bucket, replacing any existing entry keyed on (FKColumn, TargetTypeSlug).
// Overwrite-on-conflict is important: a schema edit that changes
// x-display-property from "name" to "title" must take effect on the next
// EnsureTable — otherwise the stale DisplayProperty would silently win and
// populateDisplayColumns would keep reading from the wrong field. Caller must
// hold reverseReMu.
func (pm *projectionManager) appendForwardRefLocked(
	referencingSlug string, ref repositories.ForwardReference,
) {
	existing, _ := pm.forwardRe.Load(referencingSlug)
	var old []repositories.ForwardReference
	if existing != nil {
		old = existing.([]repositories.ForwardReference)
	}
	updated := make([]repositories.ForwardReference, 0, len(old)+1)
	for _, r := range old {
		if r.FKColumn == ref.FKColumn && r.TargetTypeSlug == ref.TargetTypeSlug {
			continue // drop stale entry — overwrite with the new one
		}
		updated = append(updated, r)
	}
	updated = append(updated, ref)
	pm.forwardRe.Store(referencingSlug, updated)
}

// RegisterLink activates a cross-type link declared outside the source type's
// schema. See the ProjectionManager interface docstring for semantics.
//
// Implementation: the method adds the FK + display columns via the existing
// addMissingColumns path (idempotent — skips columns that already exist),
// then records symmetric forward/reverse reference entries through the same
// helpers used by x-resource-type schema parsing. This keeps schema-declared
// and link-declared references indistinguishable to the rest of the system
// (display propagation, triple extraction, UI rendering).
//
// If the source type has no projection table yet (RegisterLink called before
// EnsureTable), the method returns nil and skips silently — the caller
// (LinkActivator) re-runs the pass after each preset install, and the link
// will activate the next time around when the source table exists.
func (pm *projectionManager) RegisterLink(ctx context.Context, ref repositories.LinkReference) error {
	if ref.SourceSlug == "" || ref.PropertyName == "" || ref.TargetSlug == "" {
		return fmt.Errorf("RegisterLink: SourceSlug, PropertyName, TargetSlug are required")
	}
	displayProperty := ref.DisplayProperty
	if displayProperty == "" {
		displayProperty = "name"
	}
	if !pm.HasProjectionTable(ref.SourceSlug) {
		// Source type not yet installed — activation deferred to next reconcile.
		return nil
	}
	tableName := pm.TableName(ref.SourceSlug)
	colName := utils.CamelToSnake(ref.PropertyName)
	cols := []columnDef{
		{Name: colName, SQLType: "TEXT"},
		{Name: colName + "_display", SQLType: "VARCHAR(512)", Derived: true},
	}
	if err := pm.addMissingColumns(ctx, tableName, cols); err != nil {
		return fmt.Errorf("RegisterLink: add columns to %q: %w", tableName, err)
	}
	// Refresh cached column set so HasColumn reflects the ALTER TABLE. The
	// columns map is read without a lock by HasColumn, so we copy-on-write
	// into a fresh map and Store the new tableInfo rather than mutating the
	// existing map in place (maps are not safe for concurrent read/write).
	if v, ok := pm.tables.Load(ref.SourceSlug); ok {
		if info, ok := v.(tableInfo); ok {
			updatedColumns := make(map[string]bool, len(info.columns)+len(cols))
			for name, exists := range info.columns {
				updatedColumns[name] = exists
			}
			for _, col := range cols {
				updatedColumns[col.Name] = true
			}
			info.columns = updatedColumns
			// Same copy-on-write for the declared set, which DeclaredColumns
			// reads without a lock. Only the FK is declared — its `_display`
			// sibling is derived.
			updatedDeclared := make(map[string]bool, len(info.declared)+1)
			for name, exists := range info.declared {
				updatedDeclared[name] = exists
			}
			updatedDeclared[colName] = true
			info.declared = updatedDeclared
			pm.tables.Store(ref.SourceSlug, info)
		}
	}
	pm.reverseReMu.Lock()
	pm.registerRefLocked(ref.SourceSlug, ref.PropertyName, ref.TargetSlug, displayProperty)
	pm.reverseReMu.Unlock()
	return nil
}

func (pm *projectionManager) EnsureExistingTables(ctx context.Context) error {
	var types []models.ResourceType
	if err := pm.db.WithContext(ctx).
		Where("deleted_at IS NULL").Find(&types).Error; err != nil {
		return fmt.Errorf("failed to load existing resource types: %w", err)
	}
	for _, rt := range types {
		var schema json.RawMessage
		if rt.Schema != "" {
			schema = json.RawMessage(rt.Schema)
		}
		var ldContext json.RawMessage
		if rt.Context != "" {
			ldContext = json.RawMessage(rt.Context)
		}
		if err := pm.EnsureTable(ctx, rt.Slug, schema, ldContext); err != nil {
			pm.logger.Error(ctx, "failed to ensure projection table",
				"slug", rt.Slug, "error", err)
		}
	}
	return nil
}

// baseColumnDefs are the system columns every projection row carries. They
// match the non-PK base columns createTableIfNotExists lists, but are typed
// nullable here on purpose: ADD COLUMN can't introduce NOT NULL without a
// default on a populated table, and the writer always supplies these values.
// (id is the primary key and can't be ALTER-added, so it is omitted — a
// table missing its primary key is not a case this reconciliation targets.)
func (pm *projectionManager) baseColumnDefs() []columnDef {
	ts := "DATETIME"
	if pm.db.Name() == "postgres" {
		ts = "TIMESTAMP WITH TIME ZONE"
	}
	return []columnDef{
		{Name: "type_slug", SQLType: "TEXT"},
		{Name: "status", SQLType: "TEXT"},
		{Name: "created_by", SQLType: "TEXT"},
		{Name: "account_id", SQLType: "TEXT"},
		{Name: "sequence_no", SQLType: "INTEGER"},
		{Name: "created_at", SQLType: ts},
		{Name: "updated_at", SQLType: ts},
	}
}

func (pm *projectionManager) createTableIfNotExists(ctx context.Context, tableName string, columns []columnDef) error {
	dialect := pm.db.Name()

	colDefs := []string{
		"id TEXT PRIMARY KEY",
		"type_slug TEXT NOT NULL",
		"status TEXT NOT NULL DEFAULT 'active'",
		"created_by TEXT",
		"account_id TEXT",
		"sequence_no INTEGER",
	}

	if dialect == "postgres" {
		colDefs = append(colDefs,
			"created_at TIMESTAMP WITH TIME ZONE",
			"updated_at TIMESTAMP WITH TIME ZONE")
	} else {
		colDefs = append(colDefs, "created_at DATETIME", "updated_at DATETIME")
	}

	for _, col := range columns {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", col.Name, col.SQLType))
	}

	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)",
		tableName, strings.Join(colDefs, ",\n  "))

	return pm.db.WithContext(ctx).Exec(ddl).Error
}

func (pm *projectionManager) addMissingColumns(ctx context.Context, tableName string, columns []columnDef) error {
	for _, col := range columns {
		if pm.db.Migrator().HasColumn(tableName, col.Name) {
			continue
		}
		ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, col.Name, col.SQLType)
		if err := pm.db.WithContext(ctx).Exec(ddl).Error; err != nil {
			return err
		}
	}
	return nil
}

// slugToTableName converts a resource type slug to a SQL table name.
// Replaces hyphens with underscores and pluralizes.
func slugToTableName(slug string) string {
	name := strings.ReplaceAll(slug, "-", "_")
	return inflection.Plural(name)
}

// schemaToColumns parses a JSON Schema and returns column definitions.
// Skips JSON-LD meta-keys and standard column names.
// For properties with x-resource-type, an additional _display column is generated.
func schemaToColumns(schema json.RawMessage) []columnDef {
	if len(schema) == 0 {
		return nil
	}

	var s struct {
		Properties map[string]struct {
			Type          string `json:"type"`
			XResourceType string `json:"x-resource-type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil
	}

	var cols []columnDef
	for propName, propDef := range s.Properties {
		if jsonLDKeys[propName] {
			continue
		}

		colName := utils.CamelToSnake(propName)
		if standardColumnNames[colName] {
			continue
		}

		sqlType := jsonTypeToSQL(propDef.Type)
		cols = append(cols, columnDef{Name: colName, SQLType: sqlType})

		// Add a denormalized display column for reference properties.
		if propDef.XResourceType != "" {
			cols = append(cols, columnDef{
				Name: colName + "_display", SQLType: "VARCHAR(512)", Derived: true,
			})
		}
	}
	return cols
}

// jsonTypeToSQL maps JSON Schema types to SQL column types.
func jsonTypeToSQL(jsonType string) string {
	switch jsonType {
	case "string":
		return "TEXT"
	case "number":
		return "REAL"
	case "integer":
		return "INTEGER"
	case "boolean":
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}

// ExtractReport says how completely ExtractFlatColumns read one document.
//
// The absence of a column from the extracted row is ambiguous on its own: the
// client may have cleared the property, or the extraction may simply have
// failed to read it. Issue #550 made that ambiguity load-bearing — the
// wholesale write path nulls a declared column it does not find — so the
// extraction now says which of the two happened. A caller that acts on absence
// must check Complete first; a caller that only writes what it found may
// ignore the report, but should log it (Article XI).
type ExtractReport struct {
	// ReadError is set when the document itself could not be read: it is not
	// valid JSON, or its @graph nodes are not objects. Nothing was extracted,
	// so EVERY column is missing for a reason that is not a clear.
	ReadError error
	// UnreadableEdges lists the edges-node keys that produced no column value.
	// Two causes, one consequence: no term, alias or @vocab in the stored
	// @context names the key (the context drifted away from the record), or
	// the value carried no usable reference. Either way the document still
	// states a reference the row does not carry.
	UnreadableEdges []string
}

// Complete reports whether the whole document was read. Only a complete read
// makes a missing column mean "the client cleared this property".
func (r ExtractReport) Complete() bool {
	return r.ReadError == nil && len(r.UnreadableEdges) == 0
}

// ExtractFlatColumns extracts flat key-value pairs from JSON data into a row map.
// Supports both @graph format and legacy flat format.
// For @graph: extracts intrinsic props from entity node, FK values from edges node.
// Skips JSON-LD meta-keys and standard column names.
//
// It returns an ExtractReport rather than nothing, because a caller that treats
// a missing column as a deliberate clear needs to know the difference between
// "the document omits this" and "this extraction could not read the document".
func ExtractFlatColumns(data, ldContext json.RawMessage, row map[string]any) ExtractReport {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return ExtractReport{ReadError: fmt.Errorf("resource data is not valid JSON: %w", err)}
	}

	// Check for @graph format.
	if graphArr, ok := doc["@graph"].([]any); ok && len(graphArr) > 0 {
		// Extract intrinsic properties from entity node (first in @graph).
		entityNode, ok := graphArr[0].(map[string]any)
		if !ok {
			return ExtractReport{ReadError: errors.New("@graph entity node is not an object")}
		}
		extractNodeColumns(entityNode, row)
		// Extract FK values from edges node (second in @graph).
		if len(graphArr) > 1 {
			edgesNode, ok := graphArr[1].(map[string]any)
			if !ok {
				return ExtractReport{ReadError: errors.New("@graph edges node is not an object")}
			}
			return ExtractReport{UnreadableEdges: extractEdgeColumns(edgesNode, ldContext, row)}
		}
		return ExtractReport{}
	}

	// Legacy flat format.
	extractNodeColumns(doc, row)
	return ExtractReport{}
}

// extractNodeColumns extracts flat properties from a JSON-LD node into a row map.
func extractNodeColumns(m map[string]any, row map[string]any) {
	for key, val := range m {
		if jsonLDKeys[key] {
			continue
		}
		colName := utils.CamelToSnake(key)
		if standardColumnNames[colName] {
			continue
		}
		switch v := val.(type) {
		case map[string]any, []any:
			b, err := json.Marshal(v)
			if err == nil {
				row[colName] = string(b)
			}
		default:
			row[colName] = val
		}
	}
}

// extractEdgeColumns extracts FK values from a JSON-LD edges node into a row map.
// Uses the @context to reverse-map predicate IRIs back to property names,
// then converts property names to snake_case column names.
//
// It returns the keys it could not turn into a column value. Skipping one used
// to be silent, which made the edge indistinguishable from an edge the document
// never had — and issue #550 reads that absence as a clear. The caller decides
// what to do; this function's job is to stop hiding the difference.
func extractEdgeColumns(
	edges map[string]any, ldContext json.RawMessage, row map[string]any,
) []string {
	var unreadable []string
	for key, val := range edges {
		if key == "@id" {
			continue
		}
		// Records written before issue #515 key their edges by predicate IRI;
		// newer ones key them by property name. Both forms are stored.
		propName, ok := jsonld.EdgeProperty(key, ldContext)
		if !ok {
			// The stored @context no longer names this predicate. The reference
			// is still in the document, so report the key rather than drop it.
			unreadable = append(unreadable, key)
			continue
		}
		colName := utils.CamelToSnake(propName)
		if standardColumnNames[colName] {
			// A standard column is never a declared property, so nothing reads
			// its absence as a clear. Not a defect.
			continue
		}
		ids, isList := jsonld.EdgeIDs(val)
		if len(ids) == 0 {
			// BuildResourceGraph writes an edge key only with at least one
			// reference, so an empty one is a shape this reader does not know.
			unreadable = append(unreadable, key)
			continue
		}
		if !isList {
			row[colName] = ids[0]
			continue
		}
		// A list reference cannot fit a scalar FK column, so it is stored as a
		// JSON array in the same TEXT column and decoded on read. Leaving it
		// NULL — what happened before issue #513 — made the projection disagree
		// with the canonical record about whether the value existed at all.
		encoded, err := json.Marshal(ids)
		if err != nil {
			unreadable = append(unreadable, key)
			continue
		}
		row[colName] = string(encoded)
	}
	return unreadable
}
