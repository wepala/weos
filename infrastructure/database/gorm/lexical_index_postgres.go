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

// pgLexicalSchema creates the PostgreSQL lexical index. The tsvector column is
// generated from content, so a write only ever sets the text and the vector
// cannot drift from it.
//
// The text search configuration is 'simple' — lowercased words, no stemming,
// no stop words — which is what SQLite's default FTS5 tokenizer (unicode61)
// does. Both engines therefore match the same words for the same query, and a
// search for a proper noun or an identifier (the stated purpose of
// memory_search) is never stemmed or dropped as a stop word.
var pgLexicalSchema = []string{
	"CREATE TABLE IF NOT EXISTS resource_search (" +
		"id TEXT PRIMARY KEY, " +
		"type_slug TEXT NOT NULL, " +
		"content TEXT NOT NULL, " +
		"tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple'::regconfig, content)) STORED)",
	"CREATE INDEX IF NOT EXISTS resource_search_tsv_idx ON resource_search USING GIN (tsv)",
	"CREATE INDEX IF NOT EXISTS resource_search_type_slug_idx ON resource_search (type_slug)",
}

// pgHeadlineOptions mirrors the SQLite snippet: no highlight markers, an
// ellipsis between fragments, about a dozen words.
const pgHeadlineOptions = `StartSel="", StopSel="", MaxWords=12, MinWords=5, FragmentDelimiter=" … "`

type postgresLexicalIndex struct {
	db *gorm.DB
}

func newPostgresLexicalIndex(ctx context.Context, db *gorm.DB) (*postgresLexicalIndex, error) {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", pgLexicalSchemaLock).Error; err != nil {
			return fmt.Errorf("lock lexical schema: %w", err)
		}
		for _, stmt := range pgLexicalSchema {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("create lexical schema: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &postgresLexicalIndex{db: db}, nil
}

func (p *postgresLexicalIndex) Active() bool   { return true }
func (p *postgresLexicalIndex) Engine() string { return repositories.LexicalEnginePostgres }

func (p *postgresLexicalIndex) Index(ctx context.Context, id, typeSlug, content string) error {
	err := lexicalWriter(ctx, p.db).Exec(
		"INSERT INTO resource_search (id, type_slug, content) VALUES (?, ?, ?) "+
			"ON CONFLICT (id) DO UPDATE SET type_slug = EXCLUDED.type_slug, content = EXCLUDED.content",
		id, typeSlug, content,
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

// Search matches the query with plainto_tsquery, which reads its input as
// plain words and ANDs them: every tsquery operator (& | ! <-> :* and
// parentheses) and every quote is a word separator, never syntax. That is the
// same guarantee fts5Match gives on SQLite. websearch_to_tsquery is not used
// because it obeys quotes (an adjacency phrase), "or" and a leading "-", so a
// typed question could quietly mean something else. A query with no words
// left becomes an empty tsquery, which matches nothing.
func (p *postgresLexicalIndex) Search(
	ctx context.Context, query string, limit int, typeSlugs ...string,
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

// Clear empties the index before a rebuild replays history into it.
func (p *postgresLexicalIndex) Clear(ctx context.Context) error {
	if err := lexicalWriter(ctx, p.db).Exec("TRUNCATE resource_search").Error; err != nil {
		return fmt.Errorf("lexical index: clear: %w", err)
	}
	return nil
}
