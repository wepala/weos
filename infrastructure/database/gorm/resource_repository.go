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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/models"
	"weos/pkg/identity"
	"weos/pkg/utils"

	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// genericSortableColumns are allowed for sorting in the generic resources table.
var genericSortableColumns = map[string]bool{
	"id":         true,
	"created_at": true,
	"status":     true,
}

type cursorData struct {
	Value string `json:"v"`
	ID    string `json:"id"`
}

func encodeCursor(sortValue, id string) string {
	data, err := json.Marshal(cursorData{Value: sortValue, ID: id})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(cursor string) (cursorData, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return cursorData{}, err
	}
	var cd cursorData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return cursorData{}, err
	}
	return cd, nil
}

func normalizeSortOptions(sort repositories.SortOptions) (string, string) {
	sortBy := sort.SortBy
	if sortBy == "" {
		sortBy = "id"
	}
	sortOrder := sort.SortOrder
	if sortOrder != "desc" {
		sortOrder = "asc"
	}
	return sortBy, sortOrder
}

type ResourceRepository struct {
	db      *gorm.DB
	projMgr repositories.ProjectionManager
	logger  entities.Logger
}

type ResourceRepositoryResult struct {
	fx.Out
	Repository repositories.ResourceRepository
}

func ProvideResourceRepository(params struct {
	fx.In
	DB            *gorm.DB
	ProjectionMgr repositories.ProjectionManager
	Logger        entities.Logger
}) (ResourceRepositoryResult, error) {
	return ResourceRepositoryResult{
		Repository: &ResourceRepository{
			db:      params.DB,
			projMgr: params.ProjectionMgr,
			logger:  params.Logger,
		},
	}, nil
}

// Save persists a resource to the canonical table and all projection tables.
// Projection writes are not transactional by design: projections are eventually-consistent
// read models that can be rebuilt from the event store. A partial failure leaves the
// canonical resources table correct; projections self-heal on the next event replay.
func (r *ResourceRepository) Save(
	ctx context.Context, entity *entities.Resource,
) error {
	// Always save to the generic resources table (canonical store).
	model := models.FromResource(entity)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return fmt.Errorf("failed to save resource: %w", err)
	}
	// Also save to the projection table (denormalized read model) if one exists.
	if r.projMgr.HasProjectionTable(entity.TypeSlug()) {
		if err := r.saveToProjection(ctx, entity, entity.TypeSlug()); err != nil {
			return err
		}
	}
	// Dual-projection: also save to ancestor tables.
	for _, ancestorSlug := range r.projMgr.AncestorSlugs(entity.TypeSlug()) {
		if !r.projMgr.HasProjectionTable(ancestorSlug) {
			continue
		}
		if err := r.saveToProjection(ctx, entity, ancestorSlug); err != nil {
			return err
		}
	}
	return nil
}

// saveToProjection inserts a resource into the projection table identified by targetSlug.
// The targetSlug may be the entity's own type or an ancestor type for dual-projection.
// Columns extracted from data that don't exist in the target table are silently dropped.
func (r *ResourceRepository) saveToProjection(
	ctx context.Context, entity *entities.Resource, targetSlug string,
) error {
	tableName := r.projMgr.TableName(targetSlug)
	row := map[string]any{
		"id":          entity.GetID(),
		"type_slug":   entity.TypeSlug(),
		"status":      entity.Status(),
		"created_by":  entity.CreatedBy(),
		"account_id":  entity.AccountID(),
		"sequence_no": entity.GetSequenceNo(),
		"created_at":  entity.CreatedAt(),
	}
	ldCtx := r.projMgr.Context(targetSlug)
	ExtractFlatColumns(entity.Data(), ldCtx, row)
	r.dropMissingColumns(targetSlug, row)
	r.populateDisplayColumns(ctx, targetSlug, row,
		buildLookupScope(entity.AccountID(), entity.CreatedBy()))
	if err := r.db.WithContext(ctx).Table(tableName).Create(row).Error; err != nil {
		return fmt.Errorf("failed to save resource to projection %s: %w", tableName, err)
	}
	return nil
}

// updateProjectionBySlug upserts a resource's projection row in the table identified by targetSlug.
// Uses INSERT with ON CONFLICT to atomically handle both existing and missing rows.
func (r *ResourceRepository) updateProjectionBySlug(
	ctx context.Context, entity *entities.Resource, targetSlug string,
) error {
	tableName := r.projMgr.TableName(targetSlug)
	row := map[string]any{
		"id":          entity.GetID(),
		"type_slug":   entity.TypeSlug(),
		"status":      entity.Status(),
		"created_by":  entity.CreatedBy(),
		"account_id":  entity.AccountID(),
		"sequence_no": entity.GetSequenceNo(),
		"created_at":  entity.CreatedAt(),
		"updated_at":  time.Now(),
	}
	ldCtx := r.projMgr.Context(targetSlug)
	ExtractFlatColumns(entity.Data(), ldCtx, row)
	r.dropMissingColumns(targetSlug, row)
	r.populateDisplayColumns(ctx, targetSlug, row,
		buildLookupScope(entity.AccountID(), entity.CreatedBy()))

	// Build column list for ON CONFLICT UPDATE, excluding immutable fields.
	immutable := map[string]bool{"id": true, "created_at": true, "created_by": true, "account_id": true}
	updateCols := make([]string, 0, len(row))
	for col := range row {
		if !immutable[col] {
			updateCols = append(updateCols, col)
		}
	}

	err := r.db.WithContext(ctx).Table(tableName).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns(updateCols),
		}).Create(row).Error
	if err != nil {
		return fmt.Errorf("failed to upsert resource in %s: %w", tableName, err)
	}
	return nil
}

// dropMissingColumns removes keys from row that don't exist as columns in the target table.
// Uses the column set cached in ProjectionManager for fast lookup without DB queries.
func (r *ResourceRepository) dropMissingColumns(targetSlug string, row map[string]any) {
	for col := range row {
		if standardColumnNames[col] {
			continue
		}
		if !r.projMgr.HasColumn(targetSlug, col) {
			delete(row, col)
		}
	}
}

// populateDisplayColumns looks up display values for every outgoing reference
// on the target type and injects them into the row before INSERT/UPDATE. Called
// from all three projection write paths (saveToProjection, updateProjectionBySlug,
// updateDataInProjection) so that reference columns like `course_id_display` are
// populated at write time instead of relying on async propagation alone.
//
// For each ForwardReference registered for targetSlug the helper skips when:
//   - the destination display column doesn't exist on this projection table
//     (dual-projection ancestors may lack the column);
//   - the row doesn't carry the FK key at all (partial UpdateData patch);
//   - the caller has already supplied a non-empty display value.
//
// When the FK is explicitly nil or empty string, OR present but unresolvable
// (target row missing, out-of-scope, lookup error), the display column is
// explicitly set to nil so UPDATE statements clear any prior database value.
// Without this, an FK rebound to a non-existent or out-of-scope target would
// keep the old `<fk>_display` row in the database — a ghost name in the UI.
//
// **Visibility scope.** The caller passes an explicit *VisibilityScope so this
// helper can enforce the same access boundary the rest of the stack uses (see
// applyVisibilityScope and checkInstanceAccess):
//   - the row's account_id must match the target's account_id when both sides
//     are account-scoped (multi-tenant isolation);
//   - the writer (scope.AgentID) must be the target's creator OR have an
//     explicit "read" grant in resource_permissions.
//
// Passing scope explicitly (rather than reading it from the row map) is
// important because partial-update paths like updateDataInProjection don't
// carry account_id/created_by in the row map — those callers load the scope
// from the canonical row. A nil scope means "system context" — the lookup
// runs unscoped, matching checkInstanceAccess's nil-auth fail-open path.
// Note: the admin/owner role bypass is NOT enforced here (it requires the
// account membership repository, which the gorm layer can't import). Admins
// who write rows referencing other users' private resources within the same
// account will see a NULL display until the triple-propagation path fills it.
//
// **Error policy.** Display columns are denormalized read optimizations, not
// correctness invariants — a NULL display is a valid state (the frontend
// falls back to rendering the raw FK). To honor Save's eventually-consistent
// projection contract (see Save's doc), this helper *logs and tolerates*
// errors from the underlying lookup rather than aborting the write: a
// transient DB hiccup on a reference lookup must not strand the canonical
// resources row by failing the projection insert. The display will be
// re-populated on the next write or via the triple-handler propagation path
// when the parent resource changes. The helper itself never returns an error.
func (r *ResourceRepository) populateDisplayColumns(
	ctx context.Context, targetSlug string, row map[string]any,
	scope *repositories.VisibilityScope,
) {
	forwardRefs := r.projMgr.ForwardReferences(targetSlug)
	if len(forwardRefs) == 0 {
		return
	}
	for _, ref := range forwardRefs {
		// Dual-projection ancestor may not have a display column for every
		// forward ref declared on the concrete type; skip silently.
		if !r.projMgr.HasColumn(targetSlug, ref.DisplayColumn) {
			continue
		}
		rawFK, hasFK := row[ref.FKColumn]
		if !hasFK {
			continue
		}
		// Explicit FK clear — null the sibling display column atomically so a
		// stale value from an earlier write doesn't survive.
		if rawFK == nil {
			row[ref.DisplayColumn] = nil
			continue
		}
		fkVal, ok := rawFK.(string)
		if !ok {
			// Schema drift or upstream bug: FK column should always be string.
			// Log so the situation is visible without failing the write.
			r.logger.Warn(ctx, "display lookup: non-string FK value, skipping",
				"targetSlug", targetSlug, "fkColumn", ref.FKColumn,
				"valueType", fmt.Sprintf("%T", rawFK))
			continue
		}
		if fkVal == "" {
			row[ref.DisplayColumn] = nil
			continue
		}
		// Respect a display value already present on the row (e.g. a behavior
		// that provided it explicitly).
		if existing, ok := row[ref.DisplayColumn].(string); ok && existing != "" {
			continue
		}

		display, found, err := r.lookupDisplayValue(ctx, ref, fkVal, scope)
		if err != nil {
			// Log loudly but do not abort the write — persist the display as
			// NULL and let the UI fall back to the raw FK. A subsequent write
			// or triple-propagation event will re-populate it.
			r.logger.Error(ctx, "display lookup failed; persisting row with NULL display",
				"targetSlug", targetSlug, "fkColumn", ref.FKColumn,
				"targetType", ref.TargetTypeSlug, "fkValue", fkVal, "error", err)
			row[ref.DisplayColumn] = nil
			continue
		}
		if found {
			row[ref.DisplayColumn] = display
		} else {
			// FK is present but unresolved (missing target row, cross-account,
			// or missing display property). Clear any previously persisted
			// display value so an UPDATE doesn't leave a stale ghost name.
			row[ref.DisplayColumn] = nil
		}
	}
}

// buildLookupScope packages the row's account/agent identity into a
// VisibilityScope for display lookups. Returns nil when both fields are
// empty, signaling "system context" — the same fail-open convention
// checkInstanceAccess uses for nil auth.
func buildLookupScope(accountID, createdBy string) *repositories.VisibilityScope {
	if accountID == "" && createdBy == "" {
		return nil
	}
	return &repositories.VisibilityScope{
		AgentID:   createdBy,
		AccountID: accountID,
		IsAdmin:   false,
	}
}

// loadRowOwnerScope returns the visibility scope for an existing canonical
// resources row. Used by partial-update paths (updateDataInProjection) where
// the row map doesn't carry account_id/created_by — we have to fetch the
// owner from the canonical store before invoking populateDisplayColumns,
// otherwise the lookup would run unscoped and reintroduce the data leak.
//
// Lightweight by design: SELECTs only the two ownership columns from the
// resources table.
//
// Error semantics (fail closed):
//   - Row missing (gorm.ErrRecordNotFound) → returns (nil, nil). The partial
//     update is going to UPDATE 0 rows anyway, so display population is moot
//     and the caller can proceed harmlessly with a nil scope.
//   - Any other error (driver, schema, context cancellation) → returns
//     (nil, err). The caller MUST propagate the error and abort the write.
//     Falling open here would let a transient DB error silently turn the
//     scoped lookup into an unscoped one and reintroduce the cross-agent
//     display leak we just fixed.
func (r *ResourceRepository) loadRowOwnerScope(
	ctx context.Context, id string,
) (*repositories.VisibilityScope, error) {
	var owner struct {
		AccountID string
		CreatedBy string
	}
	err := r.db.WithContext(ctx).
		Table("resources").
		Select("account_id", "created_by").
		Where("id = ?", id).
		Take(&owner).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load row owner scope for %s: %w", id, err)
	}
	return buildLookupScope(owner.AccountID, owner.CreatedBy), nil
}

// lookupDisplayValue resolves a single forward-reference display value.
//
// Returns (value, true, nil) on success. Returns ("", false, nil) for a
// legitimate not-found (target row missing, or out of the writer's visibility
// scope). Returns ("", false, err) for real infrastructure or decode failures
// — those must never be silently dropped.
//
// scope, when non-nil, restricts lookups to the writer's account AND the
// per-agent visibility set (creator + explicit "read" grants in
// resource_permissions). A nil scope means "system context" — the lookup
// runs unscoped, matching checkInstanceAccess's nil-auth fail-open path.
//
// Resolution order:
//  1. Projection table of the target type (single indexed lookup).
//  2. Canonical resources table via FindByID + JSON-LD @graph extraction.
//     Covers event-replay ordering and any case where the referenced projection
//     row hasn't been written yet.
//
// The JSON-LD extraction is duplicated from application.ExtractEntityNode
// because importing application from this package would create a cycle.
// TODO: hoist ExtractEntityNode into a shared pkg to remove the duplication.
func (r *ResourceRepository) lookupDisplayValue(
	ctx context.Context, ref repositories.ForwardReference, fkVal string,
	scope *repositories.VisibilityScope,
) (string, bool, error) {
	if v, found, err := r.lookupDisplayFromProjection(ctx, ref, fkVal, scope); err != nil || found {
		return v, found, err
	}
	return r.lookupDisplayFromCanonical(ctx, ref, fkVal, scope)
}

// lookupDisplayFromProjection reads the display property from the referenced
// type's projection table. Returns (_, false, nil) when the row is simply
// missing so the caller can fall back to the canonical path; all other errors
// are propagated.
//
// When scope is non-nil, the query enforces:
//   - account_id match (when the target table has an account_id column);
//   - applyVisibilityScope: created_by match OR an explicit "read" grant in
//     resource_permissions for the writing agent.
//
// Both filters are AND-composed so cross-account references and within-account
// private references both resolve to a miss (the display column stays NULL,
// the UI falls back to the raw FK).
//
// Note on Scan semantics: GORM v2's .Scan() does NOT return ErrRecordNotFound
// for zero-row results (only First/Take/Last do). A missing row appears here
// as (err==nil, value==nil), handled by the default case. Genuine driver
// errors arrive via the err != nil branch and are propagated.
func (r *ResourceRepository) lookupDisplayFromProjection(
	ctx context.Context, ref repositories.ForwardReference, fkVal string,
	scope *repositories.VisibilityScope,
) (string, bool, error) {
	if !r.projMgr.HasProjectionTable(ref.TargetTypeSlug) {
		return "", false, nil
	}
	displayCol := utils.CamelToSnake(ref.DisplayProperty)
	if !r.projMgr.HasColumn(ref.TargetTypeSlug, displayCol) {
		return "", false, nil
	}
	tableName := r.projMgr.TableName(ref.TargetTypeSlug)
	query := r.db.WithContext(ctx).Table(tableName).
		Select(displayCol).
		Where("id = ?", fkVal)
	if scope != nil {
		// Account isolation when both sides are account-scoped.
		if scope.AccountID != "" && r.projMgr.HasColumn(ref.TargetTypeSlug, "account_id") {
			query = query.Where("account_id = ?", scope.AccountID)
		}
		// Per-agent visibility: created_by match OR explicit read grant. This
		// is the same helper applyVisibilityScope uses for list/get queries,
		// so display lookups follow the same access boundary.
		if scope.AgentID != "" {
			query = applyVisibilityScope(query, scope, "")
		}
	}
	var value *string
	if err := query.Scan(&value).Error; err != nil {
		return "", false, fmt.Errorf("projection lookup %s.%s: %w", tableName, displayCol, err)
	}
	if value != nil && *value != "" {
		return *value, true, nil
	}
	return "", false, nil
}

// lookupDisplayFromCanonical loads the referenced entity's canonical JSON-LD
// and extracts the display property. A missing row is a legitimate miss
// (returns false, nil); unmarshal failures indicate corrupt data and are
// propagated so the caller can surface them.
//
// When scope is non-nil, the canonical entity is post-checked against the
// same boundary the projection path enforces:
//   - account_id must match (when both sides have one);
//   - the writing agent must be the entity's creator (the explicit-permission
//     half of applyVisibilityScope is approximated here by accepting matches
//     on created_by only — the canonical fallback is rare enough that the
//     extra resource_permissions query is not justified).
//
// Resources with no account_id and no created_by (legacy/system data) are
// allowed through, matching the pre-migration backward-compat path in
// checkInstanceAccess.
//
// FindByID's contract: returns (entity, nil) on success or (nil, err) on
// any failure. It never returns (nil, nil), so a non-error return guarantees
// entity is non-nil.
func (r *ResourceRepository) lookupDisplayFromCanonical(
	ctx context.Context, ref repositories.ForwardReference, fkVal string,
	scope *repositories.VisibilityScope,
) (string, bool, error) {
	entity, err := r.FindByID(ctx, fkVal)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("canonical lookup for %s: %w", fkVal, err)
	}
	// Type-slug guard: a malformed or stale FK could point at a row of a
	// different type (e.g. courseId="urn:product:xyz"). The projection-path
	// lookup is implicitly type-safe — it queries the target type's table —
	// but the canonical resources table holds every type, so we have to
	// reject the mismatch explicitly. Otherwise we'd extract the wrong
	// entity's display property and persist it under the wrong column.
	if entity.TypeSlug() != ref.TargetTypeSlug {
		return "", false, nil
	}
	if !canonicalLookupVisible(entity, scope) {
		return "", false, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(entity.Data(), &doc); err != nil {
		return "", false, fmt.Errorf("parse JSON-LD for %s: %w", fkVal, err)
	}
	node := doc
	if graphArr, ok := doc["@graph"].([]any); ok && len(graphArr) > 0 {
		if first, ok := graphArr[0].(map[string]any); ok {
			node = first
		}
	}
	if v, ok := node[ref.DisplayProperty]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s, true, nil
		}
	}
	return "", false, nil
}

// canonicalLookupVisible reports whether the writer (described by scope) can
// read the referenced entity, using a conservative subset of the rules in
// checkInstanceAccess: account match AND created_by match. Legacy resources
// with no creator/account are allowed through. A nil scope (system context)
// always passes.
func canonicalLookupVisible(entity *entities.Resource, scope *repositories.VisibilityScope) bool {
	if scope == nil {
		return true
	}
	if scope.AccountID != "" && entity.AccountID() != "" && entity.AccountID() != scope.AccountID {
		return false
	}
	// Pre-migration / system rows with no creator are visible (matching
	// checkInstanceAccess's backward-compat clause).
	if entity.CreatedBy() == "" {
		return true
	}
	// The strict per-agent path: writer must be the entity's creator.
	// Explicit resource_permissions grants are not honored here — that case
	// would require a second query and the canonical fallback is rare.
	return scope.AgentID != "" && entity.CreatedBy() == scope.AgentID
}

func (r *ResourceRepository) FindByID(
	ctx context.Context, id string,
) (*entities.Resource, error) {
	// Always read from the canonical resources table (has the JSON-LD data).
	var model models.Resource
	err := r.db.WithContext(ctx).
		Where("id = ? ", id).First(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("resource %q: %w", id, repositories.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to find resource: %w", err)
	}
	return model.ToResource()
}

// applyVisibilityScope adds ownership filtering to a query when a non-nil scope
// is provided and the caller is not an admin.
func applyVisibilityScope(query *gorm.DB, scope *repositories.VisibilityScope, tablePrefix string) *gorm.DB {
	if scope == nil || scope.IsAdmin {
		return query
	}
	col := "created_by"
	idCol := "id"
	if tablePrefix != "" {
		col = tablePrefix + "." + col
		idCol = tablePrefix + "." + idCol
	}
	return query.Where(
		col+" = ? OR "+idCol+" IN (SELECT resource_id FROM resource_permissions WHERE agent_id = ? AND actions LIKE ?)",
		scope.AgentID, scope.AgentID, `%"read"%`,
	)
}

func (r *ResourceRepository) FindAllByType(
	ctx context.Context, typeSlug string, cursor string, limit int,
	sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	if limit <= 0 {
		limit = 20
	}

	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllFromProjection(ctx, typeSlug, cursor, limit, sort, scope)
	}

	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)
	if !genericSortableColumns[colName] {
		colName = "id"
	}

	query := r.db.WithContext(ctx).
		Where("type_slug = ? ", typeSlug)
	query = applyVisibilityScope(query, scope, "")
	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}

	var dbModels []models.Resource
	if err := query.Order(orderClause).Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list resources: %w", err)
	}

	return buildResourcePageWithCursor(dbModels, limit, colName, sortOrder, func(m models.Resource) string {
		if colName == "id" {
			return m.ID
		}
		if colName == "created_at" {
			return m.CreatedAt.Format(time.RFC3339Nano)
		}
		return m.Status
	})
}

//nolint:dupl // raw SQL row processing differs in sort/filter logic
func (r *ResourceRepository) findAllFromProjection(
	ctx context.Context, typeSlug, cursor string, limit int,
	sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)

	// Validate that the column exists in the projection table; fall back to id.
	// "data" is in standardColumnNames but lives in the resources table, not projection.
	if colName == "data" {
		colName = "id"
	} else if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	// Join with the resources table to get the JSON-LD data (not stored in projection).
	tbl := tableName
	selectCols := fmt.Sprintf(
		"%s.id, %s.type_slug, resources.data, %s.status, %s.sequence_no, %s.created_at, "+
			"%s.created_by, %s.account_id",
		tbl, tbl, tbl, tbl, tbl, tbl, tbl)
	if !standardColumnNames[colName] && colName != "id" {
		selectCols += fmt.Sprintf(", %s.%s", tbl, colName)
	}

	qualifiedCol := tbl + "." + colName
	query := r.db.WithContext(ctx).Table(tableName).
		Select(selectCols).
		Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", tbl))
	query = applyVisibilityScope(query, scope, tbl)
	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, qualifiedCol, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, %s.id %s", qualifiedCol, sortOrder, tbl, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("%s.id %s", tbl, sortOrder)
	}

	var rows []map[string]any
	if err := query.Order(orderClause).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list resources from %s: %w", tableName, err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	result := make([]*entities.Resource, 0, len(rows))
	var nextCursor string
	for _, row := range rows {
		id := fmt.Sprint(row["id"])
		e := &entities.Resource{}
		if err := e.Restore(
			id,
			fmt.Sprint(row["type_slug"]),
			fmt.Sprint(row["status"]),
			json.RawMessage(fmt.Sprint(row["data"])),
			toString(row["created_by"]), toString(row["account_id"]),
			parseTime(row["created_at"]),
			toInt(row["sequence_no"]),
		); err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)

		// Build cursor from the sort column value.
		sortVal := row[colName]
		if sortVal == nil {
			sortVal = row["id"]
		}
		nextCursor = encodeCursor(fmt.Sprint(sortVal), id)
	}
	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Resource]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func applyCursorCondition(query *gorm.DB, colName, sortOrder string, cd cursorData) *gorm.DB {
	// colName may be table-qualified (e.g. "products.id") when used in JOIN queries.
	// Derive idCol from colName to maintain consistent qualification.
	idCol := "id"
	if i := strings.LastIndex(colName, "."); i >= 0 {
		idCol = colName[:i+1] + "id"
	}
	if colName == "id" || strings.HasSuffix(colName, ".id") {
		if sortOrder == "desc" {
			return query.Where(idCol+" < ?", cd.ID)
		}
		return query.Where(idCol+" > ?", cd.ID)
	}
	if sortOrder == "desc" {
		return query.Where(
			fmt.Sprintf("(%s < ?) OR (%s = ? AND %s < ?)", colName, colName, idCol),
			cd.Value, cd.Value, cd.ID,
		)
	}
	return query.Where(
		fmt.Sprintf("(%s > ?) OR (%s = ? AND %s > ?)", colName, colName, idCol),
		cd.Value, cd.Value, cd.ID,
	)
}

func buildResourcePageWithCursor(
	dbModels []models.Resource, limit int, colName, sortOrder string,
	sortValFn func(models.Resource) string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Resource, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToResource()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)
		nextCursor = encodeCursor(sortValFn(m), m.ID)
	}

	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Resource]{
		Data:    result,
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func parseTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, t)
		if err != nil {
			return time.Time{}
		}
		return parsed
	default:
		return time.Time{}
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func (r *ResourceRepository) FindAllByTypeAndField(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) ([]*entities.Resource, error) {
	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllByFieldFromProjection(ctx, typeSlug, fieldName, fieldValue)
	}
	return r.findAllByFieldFromGeneric(ctx, typeSlug, fieldName, fieldValue)
}

func (r *ResourceRepository) findAllByFieldFromProjection(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) ([]*entities.Resource, error) {
	tableName := r.projMgr.TableName(typeSlug)
	colName := utils.CamelToSnake(fieldName)
	if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			return nil, fmt.Errorf("invalid field: %s", fieldName)
		}
	}
	tbl := tableName

	var rows []struct {
		ID         string
		TypeSlug   string
		Data       string
		Status     string
		CreatedBy  string
		AccountID  string
		SequenceNo int
		CreatedAt  time.Time
	}
	err := r.db.WithContext(ctx).Table(tableName).
		Select(fmt.Sprintf("%s.id, %s.type_slug, resources.data, %s.status, resources.created_by, resources.account_id, %s.sequence_no, %s.created_at",
			tbl, tbl, tbl, tbl, tbl)).
		Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", tbl)).
		Where(tbl+"."+colName+" = ? ", fieldValue).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query %s by %s: %w", tableName, colName, err)
	}

	result := make([]*entities.Resource, 0, len(rows))
	for _, row := range rows {
		e := &entities.Resource{}
		if err := e.Restore(
			row.ID, row.TypeSlug, row.Status,
			json.RawMessage(row.Data),
			row.CreatedBy, row.AccountID,
			row.CreatedAt, row.SequenceNo,
		); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (r *ResourceRepository) findAllByFieldFromGeneric(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) ([]*entities.Resource, error) {
	var dbModels []models.Resource
	err := r.db.WithContext(ctx).
		Where("type_slug = ? ", typeSlug).
		Where("json_extract(data, ?) = ?", "$."+fieldName, fieldValue).
		Find(&dbModels).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query resources by %s: %w", fieldName, err)
	}

	result := make([]*entities.Resource, 0, len(dbModels))
	for _, m := range dbModels {
		e, err := m.ToResource()
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

var operatorMap = map[string]string{
	"eq": "=", "ne": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=",
}

func (r *ResourceRepository) FindAllByTypeWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	if r.projMgr.HasProjectionTable(typeSlug) {
		return r.findAllFromProjectionWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
	}
	return r.findAllFromGenericWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

//nolint:dupl // raw SQL row processing differs in sort/filter logic
func (r *ResourceRepository) findAllFromProjectionWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)

	if colName == "data" {
		colName = "id"
	} else if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	tbl := tableName
	selectCols := fmt.Sprintf(
		"%s.id, %s.type_slug, resources.data, %s.status, %s.sequence_no, %s.created_at, "+
			"%s.created_by, %s.account_id",
		tbl, tbl, tbl, tbl, tbl, tbl, tbl)
	if !standardColumnNames[colName] && colName != "id" {
		selectCols += fmt.Sprintf(", %s.%s", tbl, colName)
	}

	qualifiedCol := tbl + "." + colName
	query := r.db.WithContext(ctx).Table(tableName).Select(selectCols).
		Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", tbl))
	query = applyVisibilityScope(query, scope, tbl)

	for _, f := range filters {
		sqlOp, ok := operatorMap[f.Operator]
		if !ok {
			continue
		}
		fc := utils.CamelToSnake(f.Field)
		if !standardColumnNames[fc] && fc != "id" {
			if !r.db.Migrator().HasColumn(tableName, fc) {
				continue
			}
		}
		query = query.Where(tbl+"."+fc+" "+sqlOp+" ?", f.Value)
	}

	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, qualifiedCol, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, %s.id %s", qualifiedCol, sortOrder, tbl, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("%s.id %s", tbl, sortOrder)
	}

	var rows []map[string]any
	if err := query.Order(orderClause).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list filtered resources from %s: %w", tableName, err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	result := make([]*entities.Resource, 0, len(rows))
	var nextCursor string
	for _, row := range rows {
		id := fmt.Sprint(row["id"])
		e := &entities.Resource{}
		if err := e.Restore(
			id, fmt.Sprint(row["type_slug"]), fmt.Sprint(row["status"]),
			json.RawMessage(fmt.Sprint(row["data"])),
			toString(row["created_by"]), toString(row["account_id"]),
			parseTime(row["created_at"]), toInt(row["sequence_no"]),
		); err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)
		sortVal := row[colName]
		if sortVal == nil {
			sortVal = row["id"]
		}
		nextCursor = encodeCursor(fmt.Sprint(sortVal), id)
	}
	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Resource]{
		Data: result, Cursor: nextCursor, Limit: limit, HasMore: hasMore,
	}, nil
}

func (r *ResourceRepository) FindAllByTypeFlat(
	ctx context.Context, typeSlug, cursor string, limit int,
	sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	if !r.projMgr.HasProjectionTable(typeSlug) {
		return repositories.PaginatedResponse[map[string]any]{},
			fmt.Errorf("%w: %q", repositories.ErrNoProjectionTable, typeSlug)
	}
	return r.findAllFlatFromProjection(ctx, typeSlug, nil, cursor, limit, sort, scope)
}

func (r *ResourceRepository) FindAllByTypeFlatWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	if !r.projMgr.HasProjectionTable(typeSlug) {
		return repositories.PaginatedResponse[map[string]any]{},
			fmt.Errorf("%w: %q", repositories.ErrNoProjectionTable, typeSlug)
	}
	return r.findAllFlatFromProjection(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

// FindFlatByID returns a single flat projection row by ID with snake_case→camelCase
// key conversion (matching FindAllByTypeFlat's output shape).
//
// Error contract (detectable via errors.Is):
//   - repositories.ErrNoProjectionTable — type has no dedicated projection table;
//     caller should fall back to FindByID.
//   - repositories.ErrNotFound — projection table exists but the row is missing.
//
// All other errors indicate a real database failure and should surface to the
// caller so the handler can return 500 rather than silently falling through.
func (r *ResourceRepository) FindFlatByID(
	ctx context.Context, typeSlug, id string,
) (map[string]any, error) {
	if !r.projMgr.HasProjectionTable(typeSlug) {
		return nil, fmt.Errorf("%w: %q", repositories.ErrNoProjectionTable, typeSlug)
	}
	tableName := r.projMgr.TableName(typeSlug)
	var row map[string]any
	if err := r.db.WithContext(ctx).Table(tableName).
		Where("id = ?", id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("flat resource %s in %s: %w", id, tableName, repositories.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to load flat resource %s from %s: %w", id, tableName, err)
	}
	camelRow := make(map[string]any, len(row))
	for k, v := range row {
		camelRow[utils.SnakeToCamel(k)] = v
	}
	return camelRow, nil
}

// findAllFlatFromProjection queries the projection table directly and returns flat rows
// with all columns (including _display). No JOIN with the resources table.
// Column names are converted from snake_case to camelCase for JSON API responses.
func (r *ResourceRepository) findAllFlatFromProjection(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	tableName := r.projMgr.TableName(typeSlug)
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := utils.CamelToSnake(sortBy)

	if colName == "data" {
		colName = "id"
	} else if colName != "id" && !standardColumnNames[colName] {
		if !r.db.Migrator().HasColumn(tableName, colName) {
			colName = "id"
		}
	}

	query := r.db.WithContext(ctx).Table(tableName)
	query = applyVisibilityScope(query, scope, "")

	for _, f := range filters {
		sqlOp, ok := operatorMap[f.Operator]
		if !ok {
			continue
		}
		fc := utils.CamelToSnake(f.Field)
		if !standardColumnNames[fc] && fc != "id" {
			if !r.db.Migrator().HasColumn(tableName, fc) {
				continue
			}
		}
		query = query.Where(fc+" "+sqlOp+" ?", f.Value)
	}

	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}

	var rows []map[string]any
	if err := query.Order(orderClause).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repositories.PaginatedResponse[map[string]any]{},
			fmt.Errorf("failed to list flat resources from %s: %w", tableName, err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	// Convert snake_case keys to camelCase and build cursor.
	result := make([]map[string]any, 0, len(rows))
	var nextCursor string
	for _, row := range rows {
		camelRow := make(map[string]any, len(row))
		for k, v := range row {
			camelRow[utils.SnakeToCamel(k)] = v
		}
		result = append(result, camelRow)
		sortVal := row[colName]
		if sortVal == nil {
			sortVal = row["id"]
		}
		nextCursor = encodeCursor(fmt.Sprint(sortVal), fmt.Sprint(row["id"]))
	}
	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[map[string]any]{
		Data: result, Cursor: nextCursor, Limit: limit, HasMore: hasMore,
	}, nil
}

func (r *ResourceRepository) findAllFromGenericWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions, scope *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	sortBy, sortOrder := normalizeSortOptions(sort)
	colName := sortBy
	if !genericSortableColumns[colName] {
		colName = "id"
	}

	query := r.db.WithContext(ctx).Model(&models.Resource{}).
		Where("type_slug = ?", typeSlug)
	query = applyVisibilityScope(query, scope, "")

	for _, f := range filters {
		sqlOp, ok := operatorMap[f.Operator]
		if !ok {
			continue
		}
		query = query.Where("json_extract(data, ?) "+sqlOp+" ?", "$."+f.Field, f.Value)
	}

	if cursor != "" {
		cd, err := decodeCursor(cursor)
		if err == nil {
			query = applyCursorCondition(query, colName, sortOrder, cd)
		}
	}

	var dbModels []models.Resource
	orderClause := fmt.Sprintf("%s %s, id %s", colName, sortOrder, sortOrder)
	if colName == "id" {
		orderClause = fmt.Sprintf("id %s", sortOrder)
	}
	if err := query.Order(orderClause).Limit(limit + 1).Find(&dbModels).Error; err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{},
			fmt.Errorf("failed to list filtered resources: %w", err)
	}

	hasMore := len(dbModels) > limit
	if hasMore {
		dbModels = dbModels[:limit]
	}

	result := make([]*entities.Resource, 0, len(dbModels))
	var nextCursor string
	for _, m := range dbModels {
		e, err := m.ToResource()
		if err != nil {
			return repositories.PaginatedResponse[*entities.Resource]{}, err
		}
		result = append(result, e)
		nextCursor = encodeCursor(m.ID, m.ID)
	}
	if !hasMore {
		nextCursor = ""
	}

	return repositories.PaginatedResponse[*entities.Resource]{
		Data: result, Cursor: nextCursor, Limit: limit, HasMore: hasMore,
	}, nil
}

func (r *ResourceRepository) UpdateData(
	ctx context.Context, id string, data json.RawMessage, sequenceNo int,
) error {
	updates := map[string]any{"data": string(data)}
	if sequenceNo > 0 {
		updates["sequence_no"] = sequenceNo
	}
	if err := r.db.WithContext(ctx).Model(&models.Resource{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update resource data: %w", err)
	}

	typeSlug := identity.ExtractResourceTypeSlug(id)
	if typeSlug == "" {
		return nil
	}
	// Update own projection table.
	if r.projMgr.HasProjectionTable(typeSlug) {
		if err := r.updateDataInProjection(ctx, id, data, sequenceNo, typeSlug); err != nil {
			return err
		}
	}
	// Dual-projection: also update ancestor tables.
	for _, ancestorSlug := range r.projMgr.AncestorSlugs(typeSlug) {
		if !r.projMgr.HasProjectionTable(ancestorSlug) {
			continue
		}
		if err := r.updateDataInProjection(ctx, id, data, sequenceNo, ancestorSlug); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResourceRepository) updateDataInProjection(
	ctx context.Context, id string, data json.RawMessage, sequenceNo int, targetSlug string,
) error {
	tableName := r.projMgr.TableName(targetSlug)
	row := map[string]any{}
	if sequenceNo > 0 {
		row["sequence_no"] = sequenceNo
	}
	ldCtx := r.projMgr.Context(targetSlug)
	ExtractFlatColumns(data, ldCtx, row)
	r.dropMissingColumns(targetSlug, row)
	// updateDataInProjection runs partial patches that don't include
	// account_id/created_by, so we have to load the row owner ourselves to
	// scope display lookups. Without this, populateDisplayColumns would run
	// in unscoped (system context) mode and reintroduce the cross-account /
	// per-agent display leak the scoped lookup logic exists to prevent.
	//
	// Fail closed: a real DB error from loadRowOwnerScope aborts the write
	// rather than silently degrading to an unscoped lookup. A row-missing
	// result (scope == nil, err == nil) is fine — the UPDATE below will
	// affect 0 rows anyway.
	scope, err := r.loadRowOwnerScope(ctx, id)
	if err != nil {
		return err
	}
	r.populateDisplayColumns(ctx, targetSlug, row, scope)
	if len(row) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Table(tableName).
		Where("id = ?", id).Updates(row)
	if result.Error != nil {
		return fmt.Errorf("failed to update projection data in %s: %w", tableName, result.Error)
	}
	// If ancestor row doesn't exist yet, skip (UpdateData is a partial update).
	return nil
}

func (r *ResourceRepository) Update(
	ctx context.Context, entity *entities.Resource,
) error {
	// Always update the generic resources table.
	model := models.FromResource(entity)
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}
	// Also update the projection table if one exists.
	if r.projMgr.HasProjectionTable(entity.TypeSlug()) {
		if err := r.updateProjectionBySlug(ctx, entity, entity.TypeSlug()); err != nil {
			return err
		}
	}
	// Dual-projection: also update ancestor tables.
	for _, ancestorSlug := range r.projMgr.AncestorSlugs(entity.TypeSlug()) {
		if !r.projMgr.HasProjectionTable(ancestorSlug) {
			continue
		}
		if err := r.updateProjectionBySlug(ctx, entity, ancestorSlug); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResourceRepository) Delete(ctx context.Context, id string) error {
	// Always delete from the generic resources table.
	err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Resource{}).Error
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}
	// Also delete from the projection table and ancestor tables.
	typeSlug := identity.ExtractResourceTypeSlug(id)
	if typeSlug == "" {
		return nil
	}
	if r.projMgr.HasProjectionTable(typeSlug) {
		if err := r.deleteFromProjectionTable(ctx, id, typeSlug); err != nil {
			return err
		}
	}
	for _, ancestorSlug := range r.projMgr.AncestorSlugs(typeSlug) {
		if !r.projMgr.HasProjectionTable(ancestorSlug) {
			continue
		}
		if err := r.deleteFromProjectionTable(ctx, id, ancestorSlug); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResourceRepository) deleteFromProjectionTable(
	ctx context.Context, id, targetSlug string,
) error {
	tableName := r.projMgr.TableName(targetSlug)
	err := r.db.WithContext(ctx).Table(tableName).
		Where("id = ?", id).Delete(map[string]any{}).Error
	if err != nil {
		return fmt.Errorf("failed to delete resource from %s: %w", tableName, err)
	}
	return nil
}
