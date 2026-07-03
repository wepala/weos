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
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"gorm.io/gorm"
)

// ProvideLexicalIndex supplies the FTS5-backed lexical index on SQLite, and an
// inactive index otherwise. On PostgreSQL FTS5 parity is explicitly out of
// scope (epic #386). The pure-Go SQLite driver (glebarez/modernc) compiles
// FTS5 in unconditionally — no build tag — so on SQLite the virtual table is
// always available; a creation failure still degrades to graph label search
// rather than failing startup.
func ProvideLexicalIndex(db *gorm.DB, cfg config.Config, logger entities.Logger) repositories.LexicalIndex {
	ctx := context.Background()
	if cfg.IsPostgres() {
		logger.Info(ctx, "lexical index disabled on PostgreSQL; memory_search falls back to graph label search")
		return nopLexicalIndex{}
	}
	err := db.Exec(
		"CREATE VIRTUAL TABLE IF NOT EXISTS resource_search USING fts5(content, id UNINDEXED, type_slug UNINDEXED)",
	).Error
	if err != nil {
		logger.Info(ctx, "sqlite FTS5 virtual table creation failed; "+
			"lexical index disabled, memory_search falls back to graph label search", "error", err)
		return nopLexicalIndex{}
	}
	return &sqliteLexicalIndex{db: db}
}

type sqliteLexicalIndex struct {
	db *gorm.DB
}

func (s *sqliteLexicalIndex) Active() bool { return true }

// writer joins the subscriber batch transaction when one is on the context.
// The batch transaction holds SQLite's write lock (txlock=immediate), so a
// write on a separate pooled connection would deadlock against the handler's
// own batch until the busy timeout — and then retry inside the still-open
// batch, starving every other writer in the process.
func (s *sqliteLexicalIndex) writer(ctx context.Context) *gorm.DB {
	if tx := subscriptions.TxFromContext(ctx); tx != nil {
		return tx
	}
	return s.db.WithContext(ctx)
}

func (s *sqliteLexicalIndex) Index(ctx context.Context, id, typeSlug, content string) error {
	// Transaction nests as a savepoint when the writer is the batch
	// transaction, keeping the delete+insert pair atomic either way.
	return s.writer(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM resource_search WHERE id = ?", id).Error; err != nil {
			return fmt.Errorf("lexical index: clear %s: %w", id, err)
		}
		if err := tx.Exec(
			"INSERT INTO resource_search(content, id, type_slug) VALUES (?, ?, ?)",
			content, id, typeSlug,
		).Error; err != nil {
			return fmt.Errorf("lexical index: insert %s: %w", id, err)
		}
		return nil
	})
}

func (s *sqliteLexicalIndex) Remove(ctx context.Context, id string) error {
	if err := s.writer(ctx).Exec("DELETE FROM resource_search WHERE id = ?", id).Error; err != nil {
		return fmt.Errorf("lexical index: remove %s: %w", id, err)
	}
	return nil
}

func (s *sqliteLexicalIndex) Search(
	ctx context.Context, query string, limit int,
) ([]repositories.LexicalHit, error) {
	match := fts5Match(query)
	if match == "" {
		return nil, nil
	}
	var rows []struct {
		ID       string
		TypeSlug string
		Snippet  string
	}
	err := s.db.WithContext(ctx).Raw(
		"SELECT id, type_slug, snippet(resource_search, 0, '', '', '…', 12) AS snippet "+
			"FROM resource_search WHERE resource_search MATCH ? ORDER BY rank LIMIT ?",
		match, limit,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("lexical index: search %q: %w", query, err)
	}
	hits := make([]repositories.LexicalHit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, repositories.LexicalHit{ID: r.ID, TypeSlug: r.TypeSlug, Snippet: r.Snippet})
	}
	return hits, nil
}

func (s *sqliteLexicalIndex) Clear(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Exec("DELETE FROM resource_search").Error; err != nil {
		return fmt.Errorf("lexical index: clear: %w", err)
	}
	return nil
}

// fts5Match turns free text into a safe FTS5 MATCH expression: each token is
// phrase-quoted so user input can never inject FTS5 query syntax (NEAR,
// column filters, boolean operators).
func fts5Match(query string) string {
	fields := strings.Fields(query)
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		if f == "" {
			continue
		}
		tokens = append(tokens, `"`+f+`"`)
	}
	return strings.Join(tokens, " ")
}

// nopLexicalIndex is the inactive fallback.
type nopLexicalIndex struct{}

func (nopLexicalIndex) Active() bool                                        { return false }
func (nopLexicalIndex) Index(context.Context, string, string, string) error { return nil }
func (nopLexicalIndex) Remove(context.Context, string) error                { return nil }
func (nopLexicalIndex) Search(context.Context, string, int) ([]repositories.LexicalHit, error) {
	return nil, nil
}
func (nopLexicalIndex) Clear(context.Context) error { return nil }
