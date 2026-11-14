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

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// The owner backfill's reads of the projection rows (wm-mttt4k). Update
// writes the canonical record and then each projection row, and the writes do
// not share a transaction, so a record can take an owner that one of its rows
// never receives. The backfill reads the rows back to count an assignment,
// and lists such rows so a later run can fill them.

var _ repositories.ProjectionOwnerReader = (*ResourceRepository)(nil)

// OwnerlessProjectionTables returns the projection tables, in the order
// Update writes them, that hold a row for id with no owning account. A table
// with no row for id is not listed.
func (r *ResourceRepository) OwnerlessProjectionTables(
	ctx context.Context, id, typeSlug string,
) ([]string, error) {
	owners, err := r.ProjectionRowOwners(ctx, id, typeSlug)
	if err != nil {
		return nil, err
	}
	var ownerless []string
	for _, o := range owners {
		if !o.Missing && o.AccountID == "" {
			ownerless = append(ownerless, o.Table)
		}
	}
	return ownerless, nil
}

// ProjectionRowOwners returns, for each projection table Update writes for a
// typeSlug resource, in that order, the owner the table's row for id holds,
// or that the table holds no row for id.
func (r *ResourceRepository) ProjectionRowOwners(
	ctx context.Context, id, typeSlug string,
) ([]repositories.ProjectionRowOwner, error) {
	tables, err := r.ownerProjectionTables(ctx, typeSlug)
	if err != nil {
		return nil, err
	}
	owners := make([]repositories.ProjectionRowOwner, 0, len(tables))
	for _, table := range tables {
		var accounts []*string
		err := r.db.WithContext(ctx).Table(table).Where("id = ?", id).
			Limit(1).Pluck("account_id", &accounts).Error
		if err != nil {
			return nil, fmt.Errorf("read the owner of %s in %s: %w", id, table, err)
		}
		o := repositories.ProjectionRowOwner{Table: table, Missing: len(accounts) == 0}
		if !o.Missing && accounts[0] != nil {
			o.AccountID = *accounts[0]
		}
		owners = append(owners, o)
	}
	return owners, nil
}

var _ repositories.ProjectionOwnerFiller = (*ResourceRepository)(nil)

// ownerlessRow is the condition a projection row the fill writes must still
// meet at the moment of the write: no owner, and a sequence number not past
// the record's. It is the WHERE of the write statement itself, so a write that
// lands after the record was read is never overwritten (wm-5o1kcm).
const ownerlessRow = "id = ? AND (account_id = '' OR account_id IS NULL) AND COALESCE(sequence_no, 0) <= ?"

// FillOwnerlessProjectionRows writes entity over each of its projection rows
// that has no owner, guarded by ownerlessRow, and never writes the canonical
// record. It needs the projection manager, which knows each table's columns.
func (r *ResourceRepository) FillOwnerlessProjectionRows(
	ctx context.Context, entity *entities.Resource,
) ([]string, error) {
	if r.projMgr == nil {
		return nil, errors.New("fill the owner of the projection rows: no projection manager")
	}
	id, seq := entity.GetID(), entity.GetSequenceNo()
	var written []string
	for _, slug := range append([]string{entity.TypeSlug()}, r.projMgr.AncestorSlugs(entity.TypeSlug())...) {
		if !r.projMgr.HasProjectionTable(slug) {
			continue
		}
		table := r.projMgr.TableName(slug)
		var n int64
		if err := r.db.WithContext(ctx).Table(table).
			Where("id = ? AND (account_id = '' OR account_id IS NULL)", id).Count(&n).Error; err != nil {
			return written, fmt.Errorf("read the owner of %s in %s: %w", id, table, err)
		}
		if n == 0 {
			continue
		}
		row := r.projectionRow(ctx, entity, slug)
		for _, col := range []string{"id", "created_at", "created_by"} {
			delete(row, col)
		}
		res := r.writeDB(ctx).Table(table).Where(ownerlessRow, id, seq).Updates(row)
		if res.Error != nil {
			return written, fmt.Errorf("fill the owner of %s in %s: %w", id, table, res.Error)
		}
		if res.RowsAffected == 0 {
			return written, fmt.Errorf("%s in %s, which took an owner or moved past sequence %d: %w",
				id, table, seq, repositories.ErrProjectionRowChanged)
		}
		written = append(written, table)
	}
	return written, nil
}

// findOwnerlessProjectionRows lists the live resources createdBy created
// whose canonical record has an owner while a projection row of theirs has
// none, or does not exist (wm-zm4sbz), with the record's owner in
// RecordAccountID and the first table with no row in MissingTable. Only the
// types of the creator's owned records are searched, and a resource is listed
// once however many of its rows are ownerless or missing.
func (r *ResourceRepository) findOwnerlessProjectionRows(
	ctx context.Context, createdBy string,
) ([]repositories.OwnerlessResource, error) {
	const owned = "resources.account_id <> '' AND resources.account_id IS NOT NULL " +
		"AND resources.created_by = ? AND resources.deleted_at IS NULL"
	var slugs []string
	if err := r.db.WithContext(ctx).Table("resources").Distinct("type_slug").
		Where(owned, createdBy).Order("type_slug").Pluck("type_slug", &slugs).Error; err != nil {
		return nil, err
	}
	var out []repositories.OwnerlessResource
	listed := map[string]int{} // id → index in out
	add := func(found []repositories.OwnerlessResource, missingTable string) {
		for _, f := range found {
			if i, ok := listed[f.ID]; ok {
				if out[i].MissingTable == "" {
					out[i].MissingTable = missingTable
				}
				continue
			}
			f.MissingTable = missingTable
			listed[f.ID] = len(out)
			out = append(out, f)
		}
	}
	const columns = "resources.id AS id, resources.type_slug AS type_slug, " +
		"resources.created_at AS created_at, resources.account_id AS record_account_id"
	for _, slug := range slugs {
		tables, err := r.ownerProjectionTables(ctx, slug)
		if err != nil {
			return nil, err
		}
		for _, table := range tables {
			var found []repositories.OwnerlessResource
			err := r.db.WithContext(ctx).Table(table).
				Select(columns).
				Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", table)).
				Where(owned, createdBy).
				Where("resources.type_slug = ?", slug).
				Where(fmt.Sprintf("(%s.account_id = '' OR %s.account_id IS NULL)", table, table)).
				Order("resources.id").
				Scan(&found).Error
			if err != nil {
				return nil, fmt.Errorf("read the owners in %s: %w", table, err)
			}
			add(found, "")
			// A row that does not exist is ownerless as far as the type's
			// listing goes, and no join finds it.
			var missing []repositories.OwnerlessResource
			err = r.db.WithContext(ctx).Table("resources").
				Select(columns).
				Where(owned, createdBy).
				Where("resources.type_slug = ?", slug).
				Where(fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s WHERE %s.id = resources.id)", table, table)).
				Order("resources.id").
				Scan(&missing).Error
			if err != nil {
				return nil, fmt.Errorf("find the rows missing from %s: %w", table, err)
			}
			add(missing, table)
		}
	}
	return out, nil
}

// ownerProjectionTables returns the projection tables a write of a typeSlug
// resource fills: its own type's, then each ancestor's, where the table
// exists. With a projection manager these are exactly the tables Update
// writes. Without one — the read-only connection a preview opens — they are
// found from the stored resource types, the same rdfs:subClassOf chain the
// projection manager caches, without creating anything.
func (r *ResourceRepository) ownerProjectionTables(ctx context.Context, typeSlug string) ([]string, error) {
	if r.projMgr != nil {
		var tables []string
		for _, slug := range append([]string{typeSlug}, r.projMgr.AncestorSlugs(typeSlug)...) {
			if r.projMgr.HasProjectionTable(slug) {
				tables = append(tables, r.projMgr.TableName(slug))
			}
		}
		return tables, nil
	}
	db := r.db.WithContext(ctx)
	migrator := db.Migrator()
	var tables []string
	visited := map[string]bool{}
	for slug := typeSlug; slug != "" && !visited[slug]; {
		visited[slug] = true
		if table := slugToTableName(slug); migrator.HasTable(table) && migrator.HasColumn(table, "account_id") {
			tables = append(tables, table)
		}
		var contexts []string
		if err := db.Table("resource_types").Where("slug = ? AND deleted_at IS NULL", slug).
			Limit(1).Pluck("context", &contexts).Error; err != nil {
			return nil, fmt.Errorf("read the resource type %s: %w", slug, err)
		}
		if len(contexts) == 0 {
			break
		}
		slug = jsonld.SubClassOf(json.RawMessage(contexts[0]))
	}
	return tables, nil
}
