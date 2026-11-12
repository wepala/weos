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
	"time"
	"unicode/utf8"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"gorm.io/gorm"
)

// ProvideLexicalIndex supplies the lexical index for the configured database:
// an FTS5 virtual table on SQLite, and a tsvector column under a GIN index on
// PostgreSQL. The pure-Go SQLite driver (glebarez/modernc) compiles FTS5 in
// unconditionally — no build tag — so on SQLite the virtual table is always
// available. A creation failure on either engine degrades memory_search to
// graph label search rather than failing startup.
//
// An index table written before the owner columns existed (wm-yna4s3) is
// dropped and recreated, and the lexical-index checkpoint is reset to 0 in the
// same transaction, so the subscriber refills the new table from history with
// every row's owner. See createSQLiteLexicalIndex and newPostgresLexicalIndex.
func ProvideLexicalIndex(db *gorm.DB, cfg config.Config, logger entities.Logger) repositories.LexicalIndex {
	ctx := context.Background()
	if cfg.IsPostgres() {
		idx, recreated, err := newPostgresLexicalIndex(ctx, db)
		if err != nil {
			logger.Error(ctx, "postgres lexical index creation failed; the database user needs the "+
				"CREATE privilege on the schema to create the resource_search table (PostgreSQL 15+ "+
				"no longer grants it on the public schema to every user). Lexical index disabled: "+
				"memory_search falls back to graph label search until the privilege is granted and "+
				"the process restarts", "error", err)
			return nopLexicalIndex{}
		}
		logLexicalRecreate(ctx, logger, recreated)
		return idx
	}
	recreated, err := createSQLiteLexicalIndex(ctx, db)
	if err != nil {
		logger.Info(ctx, "sqlite FTS5 virtual table creation failed; "+
			"lexical index disabled, memory_search falls back to graph label search", "error", err)
		return nopLexicalIndex{}
	}
	logLexicalRecreate(ctx, logger, recreated)
	return &sqliteLexicalIndex{db: db}
}

// sqliteLexicalSchema is the FTS5 table. Only content is tokenized; the owner
// columns are stored for the scope filter and never matched as text.
const sqliteLexicalSchema = "CREATE VIRTUAL TABLE IF NOT EXISTS resource_search USING fts5(" +
	"content, id UNINDEXED, type_slug UNINDEXED, created_by UNINDEXED, account_id UNINDEXED)"

// createSQLiteLexicalIndex creates the FTS5 table, first replacing a table of
// the old shape. FTS5 has no ALTER TABLE ADD COLUMN, so the old table is
// dropped. It reports whether it replaced one.
func createSQLiteLexicalIndex(ctx context.Context, db *gorm.DB) (bool, error) {
	recreated := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tables int64
		if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE name = 'resource_search'").
			Scan(&tables).Error; err != nil {
			return fmt.Errorf("look up lexical table: %w", err)
		}
		if tables > 0 {
			var owner int64
			if err := tx.Raw("SELECT COUNT(*) FROM pragma_table_info('resource_search') " +
				"WHERE name = 'created_by'").Scan(&owner).Error; err != nil {
				return fmt.Errorf("read lexical table columns: %w", err)
			}
			if owner == 0 {
				if err := tx.Exec("DROP TABLE resource_search").Error; err != nil {
					return fmt.Errorf("drop lexical table without owner columns: %w", err)
				}
				if err := resetLexicalCheckpoint(tx); err != nil {
					return err
				}
				recreated = true
			}
		}
		return tx.Exec(sqliteLexicalSchema).Error
	})
	return recreated, err
}

// resetLexicalCheckpoint moves the lexical-index subscriber back to event 0,
// in the caller's transaction, so the index it just recreated empty is
// refilled from the whole history — the same effect as
// `weos worker checkpoint reset lexical-index`. A database whose checkpoint
// table or row does not exist yet needs nothing: a subscriber with no row
// starts at 0.
func resetLexicalCheckpoint(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&subscriptions.GormCheckpointModel{}) {
		return nil
	}
	err := tx.Model(&subscriptions.GormCheckpointModel{}).
		Where("subscriber = ?", repositories.LexicalIndexSubscriber).
		Updates(map[string]any{"position": 0, "updated_at": time.Now()}).Error
	if err != nil {
		return fmt.Errorf("reset %s checkpoint: %w", repositories.LexicalIndexSubscriber, err)
	}
	return nil
}

// logLexicalRecreate reports a recreate. It names the parked-event list
// because an event the refill cannot index (its owner could not be read, for
// one) is parked rather than retried for ever, and that resource stays out of
// search until the event is replayed.
func logLexicalRecreate(ctx context.Context, logger entities.Logger, recreated bool) {
	if !recreated {
		return
	}
	logger.Info(ctx, "lexical index table had an old shape (no owner columns, or owner columns with a "+
		"default); recreated it empty and reset the "+
		repositories.LexicalIndexSubscriber+" checkpoint to 0. The index refills from the event "+
		"history, and memory_search answers from the part already refilled until it catches up. "+
		"A resource whose event the refill could not index is left out of search: list those events "+
		"with `weos worker parked list --subscriber "+repositories.LexicalIndexSubscriber+"` and "+
		"replay each with `weos worker parked replay "+repositories.LexicalIndexSubscriber+" <event-id>`")
}

type sqliteLexicalIndex struct {
	db *gorm.DB
}

func (s *sqliteLexicalIndex) Active() bool   { return true }
func (s *sqliteLexicalIndex) Engine() string { return repositories.LexicalEngineFTS5 }

// lexicalWriter joins the subscriber batch transaction when one is on the
// context. On SQLite the batch transaction holds the write lock
// (txlock=immediate), so a write on a separate pooled connection would
// deadlock against the handler's own batch until the busy timeout — and then
// retry inside the still-open batch, starving every other writer in the
// process. On PostgreSQL joining the batch keeps the index write and the
// checkpoint advance atomic.
func lexicalWriter(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx := subscriptions.TxFromContext(ctx); tx != nil {
		return tx
	}
	return db.WithContext(ctx)
}

func (s *sqliteLexicalIndex) writer(ctx context.Context) *gorm.DB {
	return lexicalWriter(ctx, s.db)
}

func (s *sqliteLexicalIndex) Index(
	ctx context.Context, id, typeSlug, content string, owner repositories.LexicalOwner,
) error {
	// Transaction nests as a savepoint when the writer is the batch
	// transaction, keeping the delete+insert pair atomic either way.
	return s.writer(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM resource_search WHERE id = ?", id).Error; err != nil {
			return fmt.Errorf("lexical index: clear %s: %w", id, err)
		}
		if err := tx.Exec(
			"INSERT INTO resource_search(content, id, type_slug, created_by, account_id) VALUES (?, ?, ?, ?, ?)",
			capLexicalContent(content), id, typeSlug, owner.CreatedBy, owner.AccountID,
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

// Owner reads the owner a resource is indexed with. A row an old binary wrote
// has NULL owner columns (FTS5 takes no NOT NULL); it is reported as no row,
// so the caller reads the owner from the events and re-indexes it.
//
// id is an UNINDEXED FTS5 column, so this is a scan of the table, as the
// DELETE in Index already is. An indexed id lookup is wm-xs10t3.
func (s *sqliteLexicalIndex) Owner(ctx context.Context, id string) (repositories.LexicalOwner, bool, error) {
	return lexicalOwnerRow(s.writer(ctx), id)
}

func (s *sqliteLexicalIndex) Search(
	ctx context.Context, query string, limit int, scope *repositories.VisibilityScope, typeSlugs ...string,
) ([]repositories.LexicalHit, error) {
	match := fts5Match(query)
	if match == "" {
		return nil, nil
	}
	sqlText := "SELECT id, type_slug, snippet(resource_search, 0, '', '', '…', 12) AS snippet " +
		"FROM resource_search WHERE resource_search MATCH ?"
	args := []any{match}
	if types := lexicalTypeFilter(typeSlugs); len(types) > 0 {
		sqlText += " AND type_slug IN ?"
		args = append(args, types)
	}
	if predicate, scopeArgs := lexicalScopeFilter(scope); predicate != "" {
		sqlText += " AND (" + predicate + ")"
		args = append(args, scopeArgs...)
	}
	sqlText += " ORDER BY rank LIMIT ?"
	args = append(args, limit)
	var rows []lexicalRow
	if err := s.db.WithContext(ctx).Raw(sqlText, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lexical index: search %q: %w", query, err)
	}
	return lexicalHits(rows), nil
}

func (s *sqliteLexicalIndex) Clear(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Exec("DELETE FROM resource_search").Error; err != nil {
		return fmt.Errorf("lexical index: clear: %w", err)
	}
	return nil
}

// lexicalLagQuery is the lexical index's lag in one statement: the newest
// event position less the lexical-index checkpoint. MAX(position) reads the
// events table's unique position index from its end, and the checkpoint is a
// primary-key lookup, so the cost does not grow with the history. The table
// names are pericarp's (GormEventModel, GormCheckpointModel); a test pins them.
const lexicalLagQuery = "SELECT COALESCE((SELECT MAX(position) FROM events), 0) - " +
	"COALESCE((SELECT position FROM subscriber_checkpoints WHERE subscriber = ?), 0)"

// lexicalLag runs lexicalLagQuery on either engine. A checkpoint ahead of the
// newest surviving event (its last events were retired by compaction) is
// caught up, not negative.
func lexicalLag(ctx context.Context, db *gorm.DB) (int64, error) {
	var lag int64
	if err := db.WithContext(ctx).Raw(lexicalLagQuery, repositories.LexicalIndexSubscriber).
		Scan(&lag).Error; err != nil {
		return 0, fmt.Errorf("lexical index: read lag: %w", err)
	}
	if lag < 0 {
		lag = 0
	}
	return lag, nil
}

func (s *sqliteLexicalIndex) Lag(ctx context.Context) (int64, error) { return lexicalLag(ctx, s.db) }

// lexicalOwnerRow reads one resource's owner from resource_search on either
// engine. A row with a NULL owner counts as no row.
func lexicalOwnerRow(db *gorm.DB, id string) (repositories.LexicalOwner, bool, error) {
	var rows []struct {
		CreatedBy string
		AccountID string
	}
	err := db.Raw("SELECT created_by, account_id FROM resource_search "+
		"WHERE id = ? AND created_by IS NOT NULL AND account_id IS NOT NULL LIMIT 1", id).
		Scan(&rows).Error
	if err != nil {
		return repositories.LexicalOwner{}, false, fmt.Errorf("lexical index: owner of %s: %w", id, err)
	}
	if len(rows) == 0 {
		return repositories.LexicalOwner{}, false, nil
	}
	return repositories.LexicalOwner{CreatedBy: rows[0].CreatedBy, AccountID: rows[0].AccountID}, true, nil
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

// lexicalContentBudget is the most bytes of a resource's text either engine
// indexes. PostgreSQL refuses a tsvector over 1 MB (SQLSTATE 54000), and the
// vector of text whose words are all distinct is up to three times the size of
// the text, so 256 KiB keeps every resource indexable there. SQLite takes the
// same cut so both engines index the same text. Text past the budget is not
// searchable.
const lexicalContentBudget = 256 << 10

// capLexicalContent cuts content to lexicalContentBudget bytes, on a rune
// boundary so the cut never leaves invalid UTF-8 (which PostgreSQL rejects).
func capLexicalContent(content string) string {
	if len(content) <= lexicalContentBudget {
		return content
	}
	cut := lexicalContentBudget
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut]
}

// lexicalRow is one search result row, on either engine.
type lexicalRow struct {
	ID       string
	TypeSlug string
	Snippet  string
}

func lexicalHits(rows []lexicalRow) []repositories.LexicalHit {
	hits := make([]repositories.LexicalHit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, repositories.LexicalHit{ID: r.ID, TypeSlug: r.TypeSlug, Snippet: r.Snippet})
	}
	return hits
}

// lexicalTypeFilter drops blank type slugs, so a filter of only blanks means
// no filter rather than "match nothing".
func lexicalTypeFilter(typeSlugs []string) []string {
	var types []string
	for _, t := range typeSlugs {
		if t = strings.TrimSpace(t); t != "" {
			types = append(types, t)
		}
	}
	return types
}

// lexicalScopeFilter is the caller's visibility scope as a predicate for the
// search query, or "" for a nil or admin scope (a system caller sees every
// resource). It is the FindAccessibleIDs rule (accessiblePredicate) applied in
// the WHERE clause, before ORDER BY ... LIMIT, so the limit is spent on rows
// the caller may read and another account's better-ranked matches cannot push
// the caller's own out of the page.
func lexicalScopeFilter(scope *repositories.VisibilityScope) (string, []any) {
	if scope == nil || scope.IsAdmin {
		return "", nil
	}
	return accessiblePredicate(scope)
}

// nopLexicalIndex is the inactive fallback.
type nopLexicalIndex struct{}

func (nopLexicalIndex) Active() bool   { return false }
func (nopLexicalIndex) Engine() string { return "" }
func (nopLexicalIndex) Index(context.Context, string, string, string, repositories.LexicalOwner) error {
	return nil
}
func (nopLexicalIndex) Remove(context.Context, string) error { return nil }
func (nopLexicalIndex) Owner(context.Context, string) (repositories.LexicalOwner, bool, error) {
	return repositories.LexicalOwner{}, false, nil
}
func (nopLexicalIndex) Search(
	context.Context, string, int, *repositories.VisibilityScope, ...string,
) ([]repositories.LexicalHit, error) {
	return nil, nil
}
func (nopLexicalIndex) Clear(context.Context) error        { return nil }
func (nopLexicalIndex) Lag(context.Context) (int64, error) { return 0, nil }
