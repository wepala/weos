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
	"fmt"

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
// Update writes them, that hold a row for id with no owning account.
func (r *ResourceRepository) OwnerlessProjectionTables(
	ctx context.Context, id, typeSlug string,
) ([]string, error) {
	tables, err := r.ownerProjectionTables(ctx, typeSlug)
	if err != nil {
		return nil, err
	}
	var ownerless []string
	for _, table := range tables {
		var n int64
		err := r.db.WithContext(ctx).Table(table).
			Where("id = ? AND (account_id = '' OR account_id IS NULL)", id).
			Count(&n).Error
		if err != nil {
			return nil, fmt.Errorf("read the owner of %s in %s: %w", id, table, err)
		}
		if n > 0 {
			ownerless = append(ownerless, table)
		}
	}
	return ownerless, nil
}

// findOwnerlessProjectionRows lists the live resources createdBy created
// whose canonical record has an owner while a projection row of theirs has
// none, with the record's owner in RecordAccountID. Only the types of the
// creator's owned records are searched, and a resource is listed once however
// many of its rows are ownerless.
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
	listed := map[string]bool{}
	for _, slug := range slugs {
		tables, err := r.ownerProjectionTables(ctx, slug)
		if err != nil {
			return nil, err
		}
		for _, table := range tables {
			var found []repositories.OwnerlessResource
			err := r.db.WithContext(ctx).Table(table).
				Select("resources.id AS id, resources.type_slug AS type_slug, "+
					"resources.created_at AS created_at, resources.account_id AS record_account_id").
				Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", table)).
				Where(owned, createdBy).
				Where("resources.type_slug = ?", slug).
				Where(fmt.Sprintf("(%s.account_id = '' OR %s.account_id IS NULL)", table, table)).
				Order("resources.id").
				Scan(&found).Error
			if err != nil {
				return nil, fmt.Errorf("read the owners in %s: %w", table, err)
			}
			for _, f := range found {
				if !listed[f.ID] {
					listed[f.ID] = true
					out = append(out, f)
				}
			}
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
