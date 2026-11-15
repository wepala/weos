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

import "gorm.io/gorm"

// columnExists reports whether table has a column named exactly column. Use
// it instead of db.Migrator().HasColumn everywhere in this package.
//
// The glebarez SQLite migrator does not look the column up: it matches the
// name with LIKE patterns over the table's CREATE statement in sqlite_master
// ('%<name> %' among them). SQLite's LIKE ignores case and treats '_' as a
// wildcard, so the check reports a column as present whenever its name, or
// any pattern it forms, appears anywhere in the DDL followed by a space. Every
// projection table is created with "id TEXT PRIMARY KEY" and "type_slug TEXT",
// so HasColumn(table, "text") and HasColumn(table, "slug") are true on all of
// them, and EnsureTable skipped the ALTER that would have added a property
// column with such a name — every write of that property then failed with
// "table X has no column named text" (wm-rk8abw).
//
// On SQLite this reads pragma_table_info, which lists the real columns. The
// name comparison ignores case because SQLite identifiers do: a column "Text"
// already blocks ADD COLUMN text. Every other dialect keeps the migrator's
// check — PostgreSQL's reads information_schema.columns with an exact match.
//
// A query error reads as "no such column", as HasColumn did; a caller that
// then issues an ALTER or a query on the column surfaces the real error.
func columnExists(db *gorm.DB, table, column string) bool {
	if db.Name() != "sqlite" {
		return db.Migrator().HasColumn(table, column)
	}
	var count int64
	if err := db.Raw(
		"SELECT count(*) FROM pragma_table_info(?) WHERE name = ? COLLATE NOCASE",
		table, column,
	).Scan(&count).Error; err != nil {
		return false
	}
	return count > 0
}
