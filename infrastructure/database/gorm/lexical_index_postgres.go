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
	"fmt"

	"github.com/wepala/weos/v3/domain/repositories"

	"gorm.io/gorm"
)

// pgLexicalSchemaLock is the advisory-lock key that serializes the index DDL,
// so a serve process and a worker process booting together do not race the
// CREATE ... IF NOT EXISTS statements (which are not concurrency-safe in
// PostgreSQL: the loser fails on the catalog's unique index).
const pgLexicalSchemaLock = 0x7765_6f73_6c78 // "weoslx"

// pgLexicalTable creates the PostgreSQL lexical index table. The tsvector
// column is generated from content, so a write only ever sets the text and the
// vector cannot drift from it.
//
// The text search configuration is 'simple' — lowercased words, no stemming,
// no stop words — so a search for a proper noun or an identifier (the stated
// purpose of memory_search) is never stemmed or dropped as a stop word, as on
// SQLite. The two engines do not split text into the same words, though.
// SQLite's unicode61 tokenizer folds accents and splits at every character
// that is not a letter or digit, '@' '.' and '-' included. PostgreSQL's parser
// folds no accents, and keeps an email address, a host name or a dotted token
// such as v3.2 as one word. So "Jose" finds "José", and "example" finds
// "jose@example.com", on SQLite only. A hyphenated word is indexed whole and
// as its parts, so "bar" finds "foo-bar" on both.
//
// created_by and account_id are the resource's owner, read from its events.
// Search filters on created_by by the FindAccessibleIDs rule; account_id is
// kept beside it, as on the resources table, so a later per-account rule needs
// no second migration.
//
// Both owner columns are NOT NULL with no default, so an insert that does not
// name them fails (wm-f3t6tp). A worker still running a binary from before
// the owner columns writes (id, type_slug, content) only. With an empty-string
// default that row would read as ownerless and be shown to every caller, and
// its checkpoint advance would commit. Without one the insert fails, its batch
// rolls back with the checkpoint advance, and the event is left for a current
// binary to index with its owner.
//
// The table statement runs only when to_regclass finds no resource_search on
// the search path. An unqualified CREATE TABLE IF NOT EXISTS checks only the
// schema it would create in — the first one on the path — so with a path of
// "app, public" and the table in public it would create an empty
// app.resource_search that shadows the populated one, with the checkpoint
// still advanced, so history would never be re-indexed into it.
const pgLexicalTable = "CREATE TABLE resource_search (" +
	"id TEXT PRIMARY KEY, " +
	"type_slug TEXT NOT NULL, " +
	"created_by TEXT NOT NULL, " +
	"account_id TEXT NOT NULL, " +
	"content TEXT NOT NULL, " +
	"tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple'::regconfig, content)) STORED)"

// pgLexicalIndexes are ensured on every start, on whichever resource_search
// the search path resolves to. An index is created in its table's schema, so
// IF NOT EXISTS checks the right one.
var pgLexicalIndexes = []string{
	"CREATE INDEX IF NOT EXISTS resource_search_tsv_idx ON resource_search USING GIN (tsv)",
	"CREATE INDEX IF NOT EXISTS resource_search_type_slug_idx ON resource_search (type_slug)",
	"CREATE INDEX IF NOT EXISTS resource_search_created_by_idx ON resource_search (created_by)",
}

// pgHeadlineOptions mirrors the SQLite snippet: no highlight markers, an
// ellipsis between fragments, about a dozen words.
const pgHeadlineOptions = `StartSel="", StopSel="", MaxWords=12, MinWords=5, FragmentDelimiter=" … "`

type postgresLexicalIndex struct {
	db *gorm.DB
}

// newPostgresLexicalIndex creates the index schema, first replacing a table of
// an old shape — no owner columns, or owner columns that default to the empty string (see
// pgLexicalTable). It creates the table only when the search path resolves to
// none, and ensures its indexes either way. It reports whether it replaced one.
//
// The old table is dropped rather than altered: ALTER ... ADD COLUMN would
// give every existing row an empty created_by, which the scope reads as ownerless
// and shows to every caller — the leak this column closes. The rows are a
// derived view, so dropping them loses nothing; the checkpoint reset in the
// same transaction makes the subscriber refill them, owners included, from
// history.
func newPostgresLexicalIndex(ctx context.Context, db *gorm.DB) (*postgresLexicalIndex, bool, error) {
	recreated := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", pgLexicalSchemaLock).Error; err != nil {
			return fmt.Errorf("lock lexical schema: %w", err)
		}
		exists, stale, err := pgLexicalTableState(tx)
		if err != nil {
			return err
		}
		if stale {
			if err := tx.Exec("DROP TABLE resource_search").Error; err != nil {
				return fmt.Errorf("drop lexical table of an old shape: %w", err)
			}
			if err := resetLexicalCheckpoint(tx); err != nil {
				return err
			}
			exists = false
			recreated = true
		}
		if !exists {
			if err := tx.Exec(pgLexicalTable).Error; err != nil {
				return fmt.Errorf("create lexical table: %w", err)
			}
		}
		for _, stmt := range pgLexicalIndexes {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("create lexical index: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &postgresLexicalIndex{db: db}, recreated, nil
}

// pgLexicalTableState reports whether the search path resolves to a
// resource_search table, and whether that table is of an old shape: without
// the created_by column, or with a default on created_by or account_id. A
// table with an owner default may hold rows an old binary wrote with no owner,
// which read as ownerless, so it is rebuilt rather than altered.
//
// The table is resolved once, by to_regclass on the search path — the same
// resolution the DROP and every query use — and its columns are read from that
// one relation, so the check can never describe a different table from the one
// it drops, and the caller never creates a second table beside the one found.
func pgLexicalTableState(tx *gorm.DB) (exists, stale bool, err error) {
	var row struct {
		Exists       bool
		HasOwner     bool
		OwnerDefault bool
	}
	err = tx.Raw("SELECT t.rel IS NOT NULL AS exists, " +
		"EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = t.rel " +
		"AND a.attname = 'created_by' AND a.attnum > 0 AND NOT a.attisdropped) AS has_owner, " +
		"EXISTS (SELECT 1 FROM pg_attrdef d JOIN pg_attribute a " +
		"ON a.attrelid = d.adrelid AND a.attnum = d.adnum " +
		"WHERE d.adrelid = t.rel AND a.attname IN ('created_by', 'account_id')) AS owner_default " +
		"FROM (SELECT to_regclass('resource_search')::oid AS rel) AS t").
		Scan(&row).Error
	if err != nil {
		return false, false, fmt.Errorf("read lexical table columns: %w", err)
	}
	return row.Exists, row.Exists && (!row.HasOwner || row.OwnerDefault), nil
}

func (p *postgresLexicalIndex) Active() bool   { return true }
func (p *postgresLexicalIndex) Engine() string { return repositories.LexicalEnginePostgres }

// Index upserts the row. The update path writes the owner as well as the
// text, so a re-indexed resource never keeps a stale or empty owner.
func (p *postgresLexicalIndex) Index(
	ctx context.Context, id, typeSlug, content string, owner repositories.LexicalOwner,
) error {
	err := lexicalWriter(ctx, p.db).Exec(
		"INSERT INTO resource_search (id, type_slug, created_by, account_id, content) VALUES (?, ?, ?, ?, ?) "+
			"ON CONFLICT (id) DO UPDATE SET type_slug = EXCLUDED.type_slug, "+
			"created_by = EXCLUDED.created_by, account_id = EXCLUDED.account_id, content = EXCLUDED.content",
		id, typeSlug, owner.CreatedBy, owner.AccountID, capLexicalContent(content),
	).Error
	if err != nil {
		return fmt.Errorf("lexical index: upsert %s: %w", id, err)
	}
	return nil
}

func (p *postgresLexicalIndex) Remove(ctx context.Context, id string) error {
	if err := lexicalWriter(ctx, p.db).Exec("DELETE FROM resource_search WHERE id = ?", id).Error; err != nil {
		return fmt.Errorf("lexical index: remove %s: %w", id, err)
	}
	return nil
}

// Owner reads the owner a resource is indexed with, by primary key.
func (p *postgresLexicalIndex) Owner(ctx context.Context, id string) (repositories.LexicalOwner, bool, error) {
	return lexicalOwnerRow(lexicalWriter(ctx, p.db), id)
}

// Search matches the query with plainto_tsquery, which reads its input as
// plain words and ANDs them: every tsquery operator (& | ! <-> :* and
// parentheses) and every quote is a word separator, never syntax. That is the
// same guarantee fts5Match gives on SQLite. websearch_to_tsquery is not used
// because it obeys quotes (an adjacency phrase), "or" and a leading "-", so a
// typed question could quietly mean something else. A query with no words
// left becomes an empty tsquery, which matches nothing.
func (p *postgresLexicalIndex) Search(
	ctx context.Context, query string, limit int, scope *repositories.VisibilityScope, typeSlugs ...string,
) ([]repositories.LexicalHit, error) {
	// ts_headline re-parses the whole content, so it runs in the outer query
	// over the limited page only, never over every matching row.
	inner := "SELECT id, type_slug, content, q, ts_rank(tsv, q) AS rank " +
		"FROM resource_search, plainto_tsquery('simple'::regconfig, ?) AS q " +
		"WHERE tsv @@ q"
	args := []any{pgHeadlineOptions, query}
	if types := lexicalTypeFilter(typeSlugs); len(types) > 0 {
		inner += " AND type_slug IN ?"
		args = append(args, types)
	}
	if predicate, scopeArgs := lexicalScopeFilter(scope); predicate != "" {
		inner += " AND (" + predicate + ")"
		args = append(args, scopeArgs...)
	}
	inner += " ORDER BY rank DESC, id LIMIT ?"
	args = append(args, limit)
	sqlText := "SELECT id, type_slug, ts_headline('simple'::regconfig, content, q, ?) AS snippet " +
		"FROM (" + inner + ") AS page ORDER BY rank DESC, id"
	var rows []lexicalRow
	if err := p.db.WithContext(ctx).Raw(sqlText, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lexical index: search %q: %w", query, err)
	}
	return lexicalHits(rows), nil
}

func (p *postgresLexicalIndex) Lag(ctx context.Context) (int64, error) { return lexicalLag(ctx, p.db) }

// Clear empties the index before a rebuild replays history into it.
func (p *postgresLexicalIndex) Clear(ctx context.Context) error {
	if err := lexicalWriter(ctx, p.db).Exec("TRUNCATE resource_search").Error; err != nil {
		return fmt.Errorf("lexical index: clear: %w", err)
	}
	return nil
}
