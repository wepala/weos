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
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The account adoption's reads and its projection writes (wm-c7xehr).
// Update keeps an owner a projection row already has, so a
// Resource.AccountAdopted reaches the record and the rows through
// WriteAccountAdoption, and a re-run finishes the rows of a move whose record
// write landed through FinishAdoptedRows. Both move a row only while it is in
// the move's source account or has no owner.

var (
	_ repositories.AccountResourceFinder = (*ResourceRepository)(nil)
	_ repositories.ProjectionOwnerMover  = (*ResourceRepository)(nil)
	_ repositories.AccountAdoptionWriter = (*ResourceRepository)(nil)
)

// ProvideAccountResourceFinder returns the adoption's lookup over db alone,
// with no projection manager, for the preview of
// `weos maintenance adopt-account`, which opens the database read-only.
func ProvideAccountResourceFinder(db *gorm.DB) repositories.AccountResourceFinder {
	return &ResourceRepository{db: db}
}

// adoptionColumns are the columns FindForAdoption scans, from the canonical
// record.
const adoptionColumns = "resources.id AS id, resources.type_slug AS type_slug, " +
	"resources.created_at AS created_at, resources.account_id AS record_account_id"

// FindForAdoption lists every live resource whose record is in from, with
// the first projection table that has no row for it and the first whose row
// a third account holds, and every live resource whose record is already in
// to while a projection row is still in from. It also lists every live
// resource whose record is in to, whose history holds a move from from to to,
// and which has a projection row with no owner or no row at all: a run
// stopped after the record write leaves a row that had no owner, or none,
// as it was (Copilot 4580310679). It is ordered by type and then by
// identifier.
func (r *ResourceRepository) FindForAdoption(
	ctx context.Context, from, to string,
) ([]repositories.AccountResource, error) {
	if from == "" || to == "" {
		return nil, errors.New("find resources to move: the source and the target account must both be given")
	}
	db := r.db.WithContext(ctx)
	var out []repositories.AccountResource
	if err := db.Table("resources").Select(adoptionColumns).
		Where("resources.account_id = ? AND resources.deleted_at IS NULL", from).
		Order("resources.type_slug").Order("resources.id").
		Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("find the resources of %s: %w", from, err)
	}
	listed := make(map[string]int, len(out))
	for i, a := range out {
		listed[a.ID] = i
	}
	fromSlugs, err := liveTypeSlugs(db, from)
	if err != nil {
		return nil, err
	}
	for _, slug := range fromSlugs {
		tables, err := r.ownerProjectionTables(ctx, slug)
		if err != nil {
			return nil, err
		}
		for _, table := range tables {
			var missing []string
			if err := db.Table("resources").
				Where("resources.account_id = ? AND resources.deleted_at IS NULL AND resources.type_slug = ?", from, slug).
				Where(fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s WHERE %s.id = resources.id)", table, table)).
				Order("resources.id").Pluck("resources.id", &missing).Error; err != nil {
				return nil, fmt.Errorf("find the rows of %s missing from %s: %w", from, table, err)
			}
			for _, id := range missing {
				if i, ok := listed[id]; ok && out[i].MissingTable == "" {
					out[i].MissingTable = table
				}
			}
			var foreign []repositories.AccountResource
			if err := db.Table(table).
				Select(adoptionColumns+fmt.Sprintf(", %s.account_id AS foreign_row_account_id", table)).
				Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", table)).
				Where("resources.account_id = ? AND resources.deleted_at IS NULL AND resources.type_slug = ?", from, slug).
				Where(fmt.Sprintf("%s.account_id IS NOT NULL AND %s.account_id NOT IN (?, ?, '')", table, table),
					from, to).
				Order("resources.id").Scan(&foreign).Error; err != nil {
				return nil, fmt.Errorf("find the rows of %s in %s held by a third account: %w", from, table, err)
			}
			for _, f := range foreign {
				if i, ok := listed[f.ID]; ok && out[i].ForeignRowTable == "" {
					out[i].ForeignRowTable, out[i].ForeignRowAccountID = table, f.ForeignRowAccountID
				}
			}
		}
	}
	// A move whose record write landed and a row write did not: the record
	// is in the target and the row still in the source.
	toSlugs, err := liveTypeSlugs(db, to)
	if err != nil {
		return nil, err
	}
	for _, slug := range toSlugs {
		tables, err := r.ownerProjectionTables(ctx, slug)
		if err != nil {
			return nil, err
		}
		for _, table := range tables {
			var behind []repositories.AccountResource
			if err := db.Table(table).Select(adoptionColumns).
				Joins(fmt.Sprintf("JOIN resources ON %s.id = resources.id", table)).
				Where("resources.account_id = ? AND resources.deleted_at IS NULL AND resources.type_slug = ?", to, slug).
				Where(fmt.Sprintf("%s.account_id = ?", table), from).
				Order("resources.id").Scan(&behind).Error; err != nil {
				return nil, fmt.Errorf("find the rows in %s still in %s: %w", table, from, err)
			}
			unfinished, err := unfinishedAdoptedRows(db, table, slug, from, to)
			if err != nil {
				return nil, err
			}
			for _, b := range append(behind, unfinished...) {
				if i, ok := listed[b.ID]; ok {
					if out[i].MissingTable == "" {
						out[i].MissingTable = b.MissingTable
					}
					continue
				}
				listed[b.ID] = len(out)
				out = append(out, b)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TypeSlug != out[j].TypeSlug {
			return out[i].TypeSlug < out[j].TypeSlug
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// unfinishedAdoptedRows lists the live resources of slug whose record is in
// to and whose row in table has no owner or does not exist, when the
// resource's history holds a Resource.AccountAdopted from from to to. The
// history check keeps out a resource of to that no move from from explains:
// its row is assign-owner's or a reproject's to repair, not this run's.
func unfinishedAdoptedRows(db *gorm.DB, table, slug, from, to string) ([]repositories.AccountResource, error) {
	moved := db.Table("events").Select("aggregate_id").
		Where("event_type = ? AND "+payloadField(db, "FromAccountID")+" = ? AND "+payloadField(db, "AccountID")+" = ?",
			adoptedEventType, from, to)
	inTo := func() *gorm.DB {
		return db.Table("resources").
			Where("resources.account_id = ? AND resources.deleted_at IS NULL AND resources.type_slug = ?", to, slug).
			Where("resources.id IN (?)", moved).Order("resources.id")
	}
	var ownerless []repositories.AccountResource
	if err := inTo().Select(adoptionColumns).
		Joins(fmt.Sprintf("JOIN %s ON %s.id = resources.id", table, table)).
		Where(fmt.Sprintf("(%s.account_id IS NULL OR %s.account_id = '')", table, table)).
		Scan(&ownerless).Error; err != nil {
		return nil, fmt.Errorf("find the moved resources with an ownerless row in %s: %w", table, err)
	}
	var missing []repositories.AccountResource
	if err := inTo().Select(adoptionColumns).
		Where(fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s WHERE %s.id = resources.id)", table, table)).
		Scan(&missing).Error; err != nil {
		return nil, fmt.Errorf("find the moved resources with no row in %s: %w", table, err)
	}
	for i := range missing {
		missing[i].MissingTable = table
	}
	return append(ownerless, missing...), nil
}

// likeEscaper escapes the LIKE wildcards and the escape character itself, so
// a fragment matches only as written: an account id may hold "_".
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// CountResourcesReferencing counts the live resources of accountID whose
// stored data holds fragment, with LIKE and an explicit escape character,
// which SQLite and PostgreSQL both accept.
func (r *ResourceRepository) CountResourcesReferencing(
	ctx context.Context, accountID, fragment string,
) (int, error) {
	if accountID == "" || fragment == "" {
		return 0, errors.New("count the resources naming a file: the account and the fragment must both be given")
	}
	var n int64
	if err := r.db.WithContext(ctx).Table("resources").
		Where("account_id = ? AND deleted_at IS NULL", accountID).
		Where(`data LIKE ? ESCAPE '\'`, "%"+likeEscaper.Replace(fragment)+"%").
		Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count the resources of %s naming %s: %w", accountID, fragment, err)
	}
	return int(n), nil
}

// liveTypeSlugs lists the types of the live records an account owns.
func liveTypeSlugs(db *gorm.DB, account string) ([]string, error) {
	var slugs []string
	if err := db.Table("resources").Distinct("type_slug").
		Where("account_id = ? AND deleted_at IS NULL", account).
		Order("type_slug").Pluck("type_slug", &slugs).Error; err != nil {
		return nil, fmt.Errorf("list the resource types of %s: %w", account, err)
	}
	return slugs, nil
}

// FinishAdoptedRows writes the projection rows of a move whose record write
// landed (Copilot 4580310679): a missing row whole, and on each row the
// sequence number, the update time and — compare-and-set in the statement —
// entity.AccountID() as the owner of a row that is in fromAccountID or has
// none. It returns the tables whose row it created or gave an owner. The
// canonical record is not written and no event is recorded.
func (r *ResourceRepository) FinishAdoptedRows(
	ctx context.Context, entity *entities.Resource, fromAccountID string,
) ([]string, error) {
	if r.projMgr == nil {
		return nil, errors.New("finish an account adoption: the repository has no projection manager")
	}
	if fromAccountID == "" || entity.AccountID() == "" {
		return nil, fmt.Errorf("finish the move of %s: the source and the target account must both be given",
			entity.GetID())
	}
	return r.writeAdoptedRows(ctx, entity, fromAccountID, true, time.Now())
}

// WriteAccountAdoption writes one Resource.AccountAdopted to the read model
// with narrow statements (wm-xuskm9): the canonical record's owner, sequence
// number and update time, and on each projection row the sequence number,
// the update time and — compare-and-set in the statement — the owner. Every
// other column, the reference labels among them, keeps its value. A missing
// projection row is written whole.
func (r *ResourceRepository) WriteAccountAdoption(
	ctx context.Context, entity *entities.Resource, fromAccountID string, move bool,
) error {
	if r.projMgr == nil {
		return errors.New("write an account adoption: the repository has no projection manager")
	}
	id, to := entity.GetID(), entity.AccountID()
	if move && (fromAccountID == "" || to == "") {
		return fmt.Errorf("write the move of %s: the source and the target account must both be given", id)
	}
	now := time.Now()
	if err := r.writeDB(ctx).Table("resources").Where("id = ?", id).Updates(map[string]any{
		"account_id": to, "sequence_no": entity.GetSequenceNo(), "updated_at": now,
	}).Error; err != nil {
		return fmt.Errorf("write the owner of %s: %w", id, err)
	}
	_, err := r.writeAdoptedRows(ctx, entity, fromAccountID, move, now)
	return err
}

// writeAdoptedRows writes the projection rows of one adoption, its own
// type's and each ancestor's: a missing row whole, then the version, then
// the owner compare-and-set — when move is true on a row in fromAccountID or
// with no owner, otherwise only on a row with no owner. It returns the tables
// whose row it created or gave an owner, in write order.
func (r *ResourceRepository) writeAdoptedRows(
	ctx context.Context, entity *entities.Resource, fromAccountID string, move bool, now time.Time,
) ([]string, error) {
	id, to := entity.GetID(), entity.AccountID()
	db := r.writeDB(ctx)
	var written []string
	for _, slug := range append([]string{entity.TypeSlug()}, r.projMgr.AncestorSlugs(entity.TypeSlug())...) {
		if !r.projMgr.HasProjectionTable(slug) {
			continue
		}
		table := r.projMgr.TableName(slug)
		if isCanonicalTable(table) {
			continue
		}
		inserted, err := r.insertMissingAdoptedRow(ctx, entity, slug, table, fromAccountID, move)
		if err != nil {
			return written, err
		}
		version := map[string]any{"sequence_no": entity.GetSequenceNo(), "updated_at": now}
		r.dropMissingColumns(slug, version)
		if len(version) > 0 {
			if err := db.Table(table).Where("id = ?", id).Updates(version).Error; err != nil {
				return written, fmt.Errorf("write the version of %s in %s: %w", id, table, err)
			}
		}
		owner := db.Table(table).Where("id = ?", id)
		if move {
			owner = owner.Where("(account_id = ? OR account_id IS NULL OR account_id = '')", fromAccountID)
		} else {
			owner = owner.Where("(account_id IS NULL OR account_id = '')")
		}
		res := owner.Updates(map[string]any{"account_id": to})
		if res.Error != nil {
			return written, fmt.Errorf("write the owner of %s in %s: %w", id, table, res.Error)
		}
		if inserted || res.RowsAffected > 0 {
			written = append(written, table)
		}
	}
	return written, nil
}

// insertMissingAdoptedRow writes the whole projection row of entity in table
// when it has none, and reports whether it wrote one. Its reference labels are read in the entity's account
// and, for a move, in the source account for each one that resolves only
// there: the run moves resources one at a time, so a resource this one names
// may not have moved yet.
func (r *ResourceRepository) insertMissingAdoptedRow(
	ctx context.Context, entity *entities.Resource, slug, table, fromAccountID string, move bool,
) (bool, error) {
	var n int64
	if err := r.writeDB(ctx).Table(table).Where("id = ?", entity.GetID()).Count(&n).Error; err != nil {
		return false, fmt.Errorf("read the row of %s in %s: %w", entity.GetID(), table, err)
	}
	if n > 0 {
		return false, nil
	}
	row := r.projectionRow(ctx, entity, slug)
	if move {
		refs := r.projMgr.ForwardReferences(slug)
		inSource := make(map[string]any, len(refs))
		for _, ref := range refs {
			if v, ok := row[ref.FKColumn]; ok {
				inSource[ref.FKColumn] = v
			}
		}
		r.populateDisplayColumns(ctx, slug, inSource, buildLookupScope(fromAccountID, entity.CreatedBy()))
		for _, ref := range refs {
			if row[ref.DisplayColumn] == nil && inSource[ref.DisplayColumn] != nil {
				row[ref.DisplayColumn] = inSource[ref.DisplayColumn]
			}
		}
	}
	res := r.writeDB(ctx).Table(table).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if res.Error != nil {
		return false, fmt.Errorf("write the missing row of %s in %s: %w", entity.GetID(), table, res.Error)
	}
	return res.RowsAffected > 0, nil
}
