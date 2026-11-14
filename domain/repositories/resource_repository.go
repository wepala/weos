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

package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
)

// ErrNoProjectionTable is returned by flat-projection reads when the target
// type has no dedicated projection table. Callers use errors.Is to detect this
// sentinel and fall back to the canonical entity path.
var ErrNoProjectionTable = errors.New("no projection table for type")

// SortOptions configures sorting for list queries.
type SortOptions struct {
	SortBy    string // column name in camelCase (e.g. "submittedAt"), default "id"
	SortOrder string // "asc" or "desc", default "asc"
}

// VisibilityScope limits query results to resources the caller can access.
// When nil, no filtering is applied (system/CLI/MCP context).
type VisibilityScope struct {
	AgentID   string
	AccountID string
	IsAdmin   bool
}

// ResourceIDFinder is implemented by a ResourceRepository that reads many
// resources of one type by identifier in a single query. It is an interface of
// its own rather than a ResourceRepository method so that adding it breaks no
// repository implemented outside this module; a caller type-asserts for it.
type ResourceIDFinder interface {
	// FindAllByTypeAndIDs returns the resources of typeSlug among ids that
	// scope can read, with their stored documents, in no set order. An
	// identifier with no such resource is left out.
	FindAllByTypeAndIDs(ctx context.Context, typeSlug string, ids []string, scope *VisibilityScope) (
		[]*entities.Resource, error)
}

// OwnerlessResource names a resource with no owning account in its canonical
// record or in one of its projection rows, and when it was created.
type OwnerlessResource struct {
	ID        string
	TypeSlug  string
	CreatedAt time.Time
	// RecordAccountID is the owner the canonical record already carries. It
	// is empty when the record itself has no owner. When it is set, only a
	// projection row of the resource lacks the owner: the write that gave the
	// record its owner did not reach that row.
	RecordAccountID string
	// MissingTable names a projection table (its own type's or an
	// ancestor's) that holds no row for the resource at all, when there is
	// one. It is set only with RecordAccountID: the resource is then missing
	// from that type's listing, and only a reproject writes the row.
	MissingTable string
}

// OwnerlessResourceFinder is implemented by a ResourceRepository that can list
// the resources with no owning account that one agent created. It backs the
// operator's owner backfill. Like ResourceIDFinder it is an interface of its
// own so that adding it breaks no repository implemented outside this module;
// a caller type-asserts for it.
type OwnerlessResourceFinder interface {
	// FindOwnerlessByCreator returns every live resource, of every type, whose
	// creator is createdBy and whose owning account is empty in its canonical
	// record or in one of its projection rows (its own type's or an
	// ancestor's), ordered by type and then by identifier. A resource whose
	// record has an owner and a projection row has none, or has no row at all
	// in one of those tables, carries the record's owner in RecordAccountID,
	// and the table with no row in MissingTable.
	FindOwnerlessByCreator(ctx context.Context, createdBy string) ([]OwnerlessResource, error)
}

// ProjectionOwnerReader is implemented by a ResourceRepository that can read
// the owner of a resource's projection rows. The canonical record and the
// projection rows are written one after the other, not in one transaction, so
// a reader of the record alone cannot tell whether a write reached the rows.
// Like OwnerlessResourceFinder it is an interface of its own; a caller
// type-asserts for it.
type ProjectionOwnerReader interface {
	// OwnerlessProjectionTables returns the projection tables — the
	// resource's own type's and each ancestor's — that hold a row for id with
	// no owning account, in write order. It returns none when every row there
	// carries an owner, or when the type has no projection table.
	OwnerlessProjectionTables(ctx context.Context, id, typeSlug string) ([]string, error)
	// ProjectionRowOwners returns one entry per projection table a write of
	// the resource fills — its own type's and each ancestor's, where the
	// table exists — in write order, with the owner that table's row holds
	// for id, or Missing when the table holds no row for id. A caller
	// compares each owner with the one it expects: an ownerless row is not
	// the only way a write fails to reach a row.
	ProjectionRowOwners(ctx context.Context, id, typeSlug string) ([]ProjectionRowOwner, error)
}

// ProjectionRowOwner is the owner one projection table holds for a resource.
type ProjectionRowOwner struct {
	// Table is the projection table.
	Table string
	// AccountID is the row's owning account, empty when the row has none.
	AccountID string
	// Missing says the table holds no row for the resource at all.
	Missing bool
}

// ErrProjectionRowChanged means a projection row changed after the canonical
// record was read, so a write of that record would put older data over it.
var ErrProjectionRowChanged = errors.New("the projection row changed after the record was read")

// ProjectionOwnerFiller is implemented by a ResourceRepository that can fill
// the empty owner of a resource's projection rows from its canonical record.
// Like ProjectionOwnerReader it is an interface of its own; a caller
// type-asserts for it.
type ProjectionOwnerFiller interface {
	// FillOwnerlessProjectionRows writes entity, the canonical record as the
	// caller just read it, over each of the resource's projection rows (its
	// own type's and each ancestor's) that has no owner, and returns the
	// tables it wrote, in write order. The canonical record is not written.
	//
	// Each row is written whole, as Update writes it, and only while it still
	// has no owner and its sequence number is not past the entity's. That
	// check is part of the write statement itself, so a write that lands
	// between the caller's read and this write is never overwritten: the row
	// is left as it is, and the call returns ErrProjectionRowChanged naming
	// the table. Rows written before that table stay written.
	FillOwnerlessProjectionRows(ctx context.Context, entity *entities.Resource) ([]string, error)
}

type ResourceRepository interface {
	Save(ctx context.Context, entity *entities.Resource) error
	FindByID(ctx context.Context, id string) (*entities.Resource, error)
	FindAllByType(ctx context.Context, typeSlug string, cursor string, limit int,
		sort SortOptions, scope *VisibilityScope) (PaginatedResponse[*entities.Resource], error)
	FindAllByTypeAndField(ctx context.Context, typeSlug, fieldName, fieldValue string) (
		[]*entities.Resource, error)
	FindAllByTypeWithFilters(ctx context.Context, typeSlug string, filters []FilterCondition,
		cursor string, limit int, sort SortOptions, scope *VisibilityScope) (
		PaginatedResponse[*entities.Resource], error)
	Update(ctx context.Context, entity *entities.Resource) error
	// UpdateData updates the JSON-LD data for a resource, and conditionally updates
	// its sequence number (skipped when sequenceNo is 0). This is a projection-level
	// operation used by triple handlers to update the materialized @graph and keep the
	// aggregate version in sync — it does NOT emit any events.
	UpdateData(ctx context.Context, id string, data json.RawMessage, sequenceNo int) error
	Delete(ctx context.Context, id string) error

	// FindAllByTypeFlat returns flat rows from the projection table directly (no JSON-LD).
	// Used for list views where denormalized columns (including _display) are needed.
	FindAllByTypeFlat(ctx context.Context, typeSlug, cursor string, limit int,
		sort SortOptions, scope *VisibilityScope) (PaginatedResponse[map[string]any], error)
	// FindAllByTypeFlatWithFilters returns filtered flat rows from the projection table.
	FindAllByTypeFlatWithFilters(ctx context.Context, typeSlug string, filters []FilterCondition,
		cursor string, limit int, sort SortOptions, scope *VisibilityScope) (
		PaginatedResponse[map[string]any], error)
	// FindFlatByID returns a single flat projection row by ID, used for detail views.
	// Returns an error wrapping ErrNoProjectionTable when no projection table exists
	// for the type (caller should fall back to FindByID + JSON-LD simplification).
	// Returns an error wrapping ErrNotFound when the projection table exists but the
	// row is missing — detectable via errors.Is.
	FindFlatByID(ctx context.Context, typeSlug, id string) (map[string]any, error)

	// FindAccessibleIDs returns the subset of `ids` that the caller is
	// allowed to read under `scope`: the creator (`created_by` = agent), rows
	// with an explicit read grant, AND pre-migration ownerless rows
	// (`created_by` = ''), which are treated as readable so the KG filter
	// doesn't hide legacy data that predates ownership tracking.
	// Implementations MUST include the ownerless case. When scope is nil
	// (system context) or admin, every input id is returned unchanged. Used by
	// the knowledge-graph permission filter to drop forbidden subjects from
	// query results in a single bulk DB call.
	FindAccessibleIDs(ctx context.Context, ids []string, scope *VisibilityScope) ([]string, error)
}
