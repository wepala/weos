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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/testpg"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	glebarez "github.com/glebarez/sqlite"
	gormlib "gorm.io/gorm"
)

// Account scope on the lexical index (finding wm-yna4s3). Each engine is run
// through the same cases: the search applies the FindAccessibleIDs rule —
// own rows, ownerless rows, and rows the caller holds a read grant on — inside
// the query, before its LIMIT.

var (
	harbor = repositories.LexicalOwner{CreatedBy: "agent-harbor", AccountID: "harbor"}
	cedar  = repositories.LexicalOwner{CreatedBy: "agent-cedar", AccountID: "cedar"}
)

func scopeOf(owner repositories.LexicalOwner) *repositories.VisibilityScope {
	return &repositories.VisibilityScope{AgentID: owner.CreatedBy, AccountID: owner.AccountID}
}

// sqliteScopeDB is a file-backed SQLite database (every pooled connection
// sees the same tables, which :memory: does not guarantee) holding the
// resource_permissions table the scope reads, as the provider migrates it.
func sqliteScopeDB(t *testing.T) *gormlib.DB {
	t.Helper()
	db, err := gormlib.Open(glebarez.Open(filepath.Join(t.TempDir(), "lexical.db")), &gormlib.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.ResourcePermission{}, &subscriptions.GormCheckpointModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// postgresScopeDB is a fresh PostgreSQL database with the same tables. Skips
// without TEST_POSTGRES_DSN.
func postgresScopeDB(t *testing.T) (*gormlib.DB, config.Config) {
	t.Helper()
	admin := os.Getenv("TEST_POSTGRES_DSN")
	if admin == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL lexical index is skipped")
	}
	dsn, err := testpg.FreshDatabase(t, admin, "weos_lexical_scope")
	if err != nil {
		t.Fatalf("fresh database: %v", err)
	}
	db := openPostgresReplica(t, dsn)
	if err := db.AutoMigrate(&models.ResourcePermission{}, &subscriptions.GormCheckpointModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.Default()
	cfg.DatabaseDSN = dsn
	return db, cfg
}

func hitIDs(hits []repositories.LexicalHit) []string {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	sort.Strings(ids)
	return ids
}

func mustIndex(t *testing.T, idx repositories.LexicalIndex, id, typeSlug, content string,
	owner repositories.LexicalOwner,
) {
	t.Helper()
	if err := idx.Index(context.Background(), id, typeSlug, content, owner); err != nil {
		t.Fatalf("Index %s: %v", id, err)
	}
}

func assertHits(t *testing.T, idx repositories.LexicalIndex, label, query string, limit int,
	scope *repositories.VisibilityScope, want ...string,
) {
	t.Helper()
	hits, err := idx.Search(context.Background(), query, limit, scope)
	if err != nil {
		t.Fatalf("%s: Search: %v", label, err)
	}
	sort.Strings(want)
	if got := hitIDs(hits); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: hits = %v, want %v", label, got, want)
	}
}

func runScopeCases(t *testing.T, idx repositories.LexicalIndex, db *gormlib.DB) {
	t.Helper()
	mustIndex(t, idx, "urn:task:harbor", "task", "Dunmore settlement balance", harbor)
	mustIndex(t, idx, "urn:task:cedar", "task", "Dunmore Road viewing", cedar)
	mustIndex(t, idx, "urn:task:preset", "task", "Dunmore checklist from a preset", repositories.LexicalOwner{})

	assertHits(t, idx, "harbor", "dunmore", 10, scopeOf(harbor), "urn:task:harbor", "urn:task:preset")
	assertHits(t, idx, "cedar", "dunmore", 10, scopeOf(cedar), "urn:task:cedar", "urn:task:preset")
	assertHits(t, idx, "system caller", "dunmore", 10, nil,
		"urn:task:harbor", "urn:task:cedar", "urn:task:preset")
	assertHits(t, idx, "admin scope", "dunmore", 10, &repositories.VisibilityScope{AgentID: "x", IsAdmin: true},
		"urn:task:harbor", "urn:task:cedar", "urn:task:preset")

	// The snippet comes from the same scoped query, so a hidden row is never
	// quoted either.
	hits, err := idx.Search(context.Background(), "settlement", 10, scopeOf(cedar))
	if err != nil || len(hits) != 0 {
		t.Errorf("cedar searching harbor's word: hits = %+v, err = %v; want none", hits, err)
	}

	// A read grant opens one of harbor's rows to cedar, as it does for reads.
	grant := models.ResourcePermission{
		ID: "perm-1", ResourceID: "urn:task:harbor", AgentID: "agent-cedar",
		Actions: `["read"]`, GrantedBy: "agent-harbor", GrantedAt: time.Now(),
	}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("grant: %v", err)
	}
	assertHits(t, idx, "cedar with a grant", "settlement", 10, scopeOf(cedar), "urn:task:harbor")

	// Re-indexing writes the owner as well as the text.
	mustIndex(t, idx, "urn:task:cedar", "task", "Dunmore Road viewing moved", harbor)
	assertHits(t, idx, "cedar after its row changed owner", "viewing", 10, scopeOf(cedar))
}

// runCrowdedScopeCase: 25 of harbor's rows outrank cedar's one match. Scoped
// after the LIMIT, cedar would get nothing; scoped in the query it gets its own.
func runCrowdedScopeCase(t *testing.T, idx repositories.LexicalIndex) {
	t.Helper()
	for i := 0; i < 25; i++ {
		mustIndex(t, idx, fmt.Sprintf("urn:task:h%02d", i), "task",
			fmt.Sprintf("Dunmore Dunmore Dunmore workstream %02d", i), harbor)
	}
	mustIndex(t, idx, "urn:task:cedar", "task", "Dunmore Road viewing", cedar)
	assertHits(t, idx, "crowded", "dunmore", 10, scopeOf(cedar), "urn:task:cedar")

	// It composes with the type filter: narrowing never reaches across.
	mustIndex(t, idx, "urn:project:cedar", "project", "Dunmore onboarding", cedar)
	hits, err := idx.Search(context.Background(), "dunmore", 10, scopeOf(harbor), "project")
	if err != nil || len(hits) != 0 {
		t.Errorf("harbor narrowed to cedar's type: hits = %+v, err = %v; want none", hits, err)
	}
}

func TestLexicalIndex_AccountScope_SQLite(t *testing.T) {
	t.Parallel()
	db := sqliteScopeDB(t)
	runScopeCases(t, ProvideLexicalIndex(db, config.Default(), &testLogger{}), db)
}

func TestLexicalIndex_AccountScopeBeforeLimit_SQLite(t *testing.T) {
	t.Parallel()
	db := sqliteScopeDB(t)
	runCrowdedScopeCase(t, ProvideLexicalIndex(db, config.Default(), &testLogger{}))
}

func TestLexicalIndex_AccountScope_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	runScopeCases(t, ProvideLexicalIndex(db, cfg, &testLogger{}), db)
}

func TestLexicalIndex_AccountScopeBeforeLimit_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	runCrowdedScopeCase(t, ProvideLexicalIndex(db, cfg, &testLogger{}))
}

// --- Migration from the table shape before owner columns ---

// lexicalInfoLogger keeps Info messages, to see the recreate reported.
type lexicalInfoLogger struct {
	testLogger
	infos []string
}

func (l *lexicalInfoLogger) Info(_ context.Context, msg string, _ ...any) {
	l.infos = append(l.infos, msg)
}

func (l *lexicalInfoLogger) recreated() bool {
	return l.mentions("recreated it empty")
}

func (l *lexicalInfoLogger) mentions(text string) bool {
	for _, m := range l.infos {
		if strings.Contains(m, text) {
			return true
		}
	}
	return false
}

// runOldShapeMigration provides the index over a table of the old shape that
// holds an unowned row, with the lexical-index checkpoint far along, and
// checks the table is replaced empty and the checkpoint sent back to 0 so the
// subscriber refills it — owners included — from history. A second provide
// over the new shape changes nothing.
func runOldShapeMigration(t *testing.T, db *gormlib.DB, cfg config.Config, oldSchema []string) {
	t.Helper()
	for _, stmt := range oldSchema {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create old shape: %v", err)
		}
	}
	if err := db.Exec("INSERT INTO resource_search (id, type_slug, content) VALUES (?, ?, ?)",
		"urn:task:old", "task", "Dunmore settlement").Error; err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	for _, sub := range []string{repositories.LexicalIndexSubscriber, "oxigraph"} {
		if err := db.Create(&subscriptions.GormCheckpointModel{
			Subscriber: sub, Position: 42, UpdatedAt: time.Now(),
		}).Error; err != nil {
			t.Fatalf("seed checkpoint %s: %v", sub, err)
		}
	}

	logger := &lexicalInfoLogger{}
	idx := ProvideLexicalIndex(db, cfg, logger)
	if !idx.Active() {
		t.Fatal("index inactive after migrating the old table")
	}
	if !logger.recreated() {
		t.Errorf("the recreate was not reported; infos = %q", logger.infos)
	}
	// A resource whose event the refill parks stays out of search, so the
	// report names the command that finds those events (wm-a0ouop).
	if !logger.mentions("weos worker parked list --subscriber " + repositories.LexicalIndexSubscriber) {
		t.Errorf("the recreate report does not name `weos worker parked list`; infos = %q", logger.infos)
	}
	// The unowned row is gone: left in place it would be read as ownerless and
	// shown to every account.
	assertHits(t, idx, "after migration", "dunmore", 10, nil)
	positions := map[string]int64{}
	var rows []subscriptions.GormCheckpointModel
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("read checkpoints: %v", err)
	}
	for _, r := range rows {
		positions[r.Subscriber] = r.Position
	}
	if positions[repositories.LexicalIndexSubscriber] != 0 {
		t.Errorf("lexical-index checkpoint = %d, want 0 so the index refills from history",
			positions[repositories.LexicalIndexSubscriber])
	}
	if positions["oxigraph"] != 42 {
		t.Errorf("oxigraph checkpoint = %d, want it untouched at 42", positions["oxigraph"])
	}

	// The new table takes owners and scopes by them.
	mustIndex(t, idx, "urn:task:harbor", "task", "Dunmore settlement", harbor)
	assertHits(t, idx, "cedar after migration", "dunmore", 10, scopeOf(cedar))

	again := &lexicalInfoLogger{}
	idx = ProvideLexicalIndex(db, cfg, again)
	if again.recreated() {
		t.Error("a second start recreated a table that already has owner columns")
	}
	assertHits(t, idx, "after restart", "dunmore", 10, nil, "urn:task:harbor")
}

func TestLexicalIndex_MigratesOldTableShape_SQLite(t *testing.T) {
	t.Parallel()
	db := sqliteScopeDB(t)
	runOldShapeMigration(t, db, config.Default(), []string{
		"CREATE VIRTUAL TABLE resource_search USING fts5(content, id UNINDEXED, type_slug UNINDEXED)",
	})
}

func TestLexicalIndex_MigratesOldTableShape_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	runOldShapeMigration(t, db, cfg, []string{
		"CREATE TABLE resource_search (id TEXT PRIMARY KEY, type_slug TEXT NOT NULL, content TEXT NOT NULL, " +
			"tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple'::regconfig, content)) STORED)",
		"CREATE INDEX resource_search_tsv_idx ON resource_search USING GIN (tsv)",
		"CREATE INDEX resource_search_type_slug_idx ON resource_search (type_slug)",
	})
}

// --- An insert that does not name the owner (wm-f3t6tp) ---

// oldBinaryUpsert is the write a worker still running the binary from before
// the owner columns makes: it names no owner.
const oldBinaryUpsert = "INSERT INTO resource_search (id, type_slug, content) VALUES (?, ?, ?) " +
	"ON CONFLICT (id) DO UPDATE SET type_slug = EXCLUDED.type_slug, content = EXCLUDED.content"

// On PostgreSQL the owner columns have no default, so an old binary's write
// fails — for a new row and for an existing one — and the batch it ran in
// rolls back with its checkpoint advance. With a default of the empty string
// the row would read as ownerless and be shown to every caller.
func TestLexicalIndex_RefusesAWriteWithNoOwner_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	idx := ProvideLexicalIndex(db, cfg, &testLogger{})
	mustIndex(t, idx, "urn:task:harbor", "task", "Dunmore settlement", harbor)
	if err := db.Create(&subscriptions.GormCheckpointModel{
		Subscriber: repositories.LexicalIndexSubscriber, Position: 7, UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	for _, id := range []string{"urn:task:new", "urn:task:harbor"} {
		err := db.Transaction(func(tx *gormlib.DB) error {
			if err := tx.Model(&subscriptions.GormCheckpointModel{}).
				Where("subscriber = ?", repositories.LexicalIndexSubscriber).
				Update("position", 8).Error; err != nil {
				return err
			}
			return tx.Exec(oldBinaryUpsert, id, "task", "Dunmore written by the old binary").Error
		})
		if err == nil {
			t.Errorf("old-binary write of %s succeeded, want a NOT NULL violation", id)
		}
	}

	var position int64
	if err := db.Model(&subscriptions.GormCheckpointModel{}).Select("position").
		Where("subscriber = ?", repositories.LexicalIndexSubscriber).Scan(&position).Error; err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if position != 7 {
		t.Errorf("checkpoint = %d, want 7: the failed write must roll back its checkpoint advance", position)
	}
	assertHits(t, idx, "cedar after the refused writes", "dunmore", 10, scopeOf(cedar))
	assertHits(t, idx, "harbor after the refused writes", "settlement", 10, scopeOf(harbor), "urn:task:harbor")
}

// A table built with an empty-string default on the owner columns (a build of
// this story before wm-f3t6tp) may hold rows an old binary wrote with no
// owner, so startup rebuilds it like a table with no owner columns at all.
func TestLexicalIndex_RebuildsATableWithOwnerDefaults_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	runOldShapeMigration(t, db, cfg, []string{
		"CREATE TABLE resource_search (id TEXT PRIMARY KEY, type_slug TEXT NOT NULL, " +
			"created_by TEXT NOT NULL DEFAULT '', account_id TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, " +
			"tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple'::regconfig, content)) STORED)",
	})
	if err := db.Exec(oldBinaryUpsert, "urn:task:old2", "task", "Dunmore").Error; err == nil {
		t.Error("old-binary write succeeded on the rebuilt table, want a NOT NULL violation")
	}
}

// The shape check resolves the table once, on the search path, and reads the
// columns of that one table. When the first schema on the path does not hold
// the table but a later one does, the check must describe the later one — the
// one the DROP and every query reach — and not report it stale.
func TestPgLexicalTableIsStale_OneSchemaResolution(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	ProvideLexicalIndex(db, cfg, &testLogger{}) // current shape, in public
	err := db.Transaction(func(tx *gormlib.DB) error {
		if err := tx.Exec("CREATE SCHEMA app").Error; err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL search_path TO app, public").Error; err != nil {
			return err
		}
		exists, stale, err := pgLexicalTableState(tx)
		if err != nil {
			return err
		}
		if !exists {
			t.Error("public.resource_search is on the search path, but the check did not find it")
		}
		if stale {
			t.Error("public.resource_search has owner columns, but the check called it stale " +
				"(columns read from current_schema(), the table found on the search path)")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
}

// Startup must not create a second, empty resource_search in the first schema
// on the search path when the current-shape table already exists in a later
// one. An unqualified CREATE TABLE IF NOT EXISTS checks only the schema it
// creates in, so it would make app.resource_search, which then shadows the
// populated public table for every query — and, the shape check having found
// nothing stale, the checkpoint stays where it is, so history is never
// re-indexed into the new table (Copilot review 5869602749).
func TestNewPostgresLexicalIndex_DoesNotShadowAnExistingTable(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	idx := ProvideLexicalIndex(db, cfg, &testLogger{}) // current shape, in public
	mustIndex(t, idx, "urn:task:harbor", "task", "Dunmore settlement", harbor)
	err := db.Transaction(func(tx *gormlib.DB) error {
		if err := tx.Exec("CREATE SCHEMA app").Error; err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL search_path TO app, public").Error; err != nil {
			return err
		}
		reopened, recreated, err := newPostgresLexicalIndex(context.Background(), tx)
		if err != nil {
			return err
		}
		if recreated {
			t.Error("the current-shape table was reported as recreated")
		}
		var shadow bool
		if err := tx.Raw("SELECT to_regclass('app.resource_search') IS NOT NULL").
			Scan(&shadow).Error; err != nil {
			return err
		}
		if shadow {
			t.Error("startup created app.resource_search, which shadows the populated public.resource_search")
		}
		assertHits(t, reopened, "harbor after a restart on a new search path", "settlement", 10,
			scopeOf(harbor), "urn:task:harbor")
		return nil
	})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
}

// SQLite cannot refuse the write: an FTS5 table takes no constraints and no
// triggers. An old binary's row gets a NULL owner, which the scope never
// matches (NULL is neither the caller nor the empty string), so a scoped
// caller is not shown it — the gap stays closed. Only a nil or admin scope
// sees it, as it sees everything.
func TestLexicalIndex_AWriteWithNoOwnerStaysHidden_SQLite(t *testing.T) {
	t.Parallel()
	db := sqliteScopeDB(t)
	idx := ProvideLexicalIndex(db, config.Default(), &testLogger{})
	if err := db.Exec("INSERT INTO resource_search(content, id, type_slug) VALUES (?, ?, ?)",
		"Dunmore written by the old binary", "urn:task:old", "task").Error; err != nil {
		t.Fatalf("old-binary insert: %v", err)
	}
	assertHits(t, idx, "harbor", "dunmore", 10, scopeOf(harbor))
	assertHits(t, idx, "system caller", "dunmore", 10, nil, "urn:task:old")
}

// --- Reading a resource's owner back (wm-ehvlu3) ---

// runOwnerCases: Owner returns the owner a row was indexed with (an ownerless
// row included), reports no row for a resource never indexed or removed, and
// sees a row written earlier in the same subscriber batch.
func runOwnerCases(t *testing.T, idx repositories.LexicalIndex, db *gormlib.DB) {
	t.Helper()
	ctx := context.Background()
	mustIndex(t, idx, "urn:task:harbor", "task", "Dunmore settlement", harbor)
	mustIndex(t, idx, "urn:task:preset", "task", "Dunmore checklist", repositories.LexicalOwner{})

	for _, c := range []struct {
		id    string
		want  repositories.LexicalOwner
		found bool
	}{
		{"urn:task:harbor", harbor, true},
		{"urn:task:preset", repositories.LexicalOwner{}, true},
		{"urn:task:never", repositories.LexicalOwner{}, false},
	} {
		got, found, err := idx.Owner(ctx, c.id)
		if err != nil || found != c.found || got != c.want {
			t.Errorf("Owner(%s) = %+v, %v, %v; want %+v, %v, nil", c.id, got, found, err, c.want, c.found)
		}
	}
	if err := idx.Remove(ctx, "urn:task:harbor"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, found, err := idx.Owner(ctx, "urn:task:harbor"); err != nil || found {
		t.Errorf("Owner after Remove: found = %v, err = %v; want no row", found, err)
	}

	// A row written earlier in the batch is not committed yet; the lookup
	// must read through the batch transaction to see it.
	cps, err := subscriptions.NewGormCheckpointStore(db)
	if err != nil {
		t.Fatalf("checkpoint store: %v", err)
	}
	batch, acquired, err := cps.Acquire(ctx, "lexical-owner-probe")
	if err != nil || !acquired {
		t.Fatalf("acquire batch: acquired = %v, err = %v", acquired, err)
	}
	defer func() {
		// The batch is only a probe; nothing it wrote is meant to persist.
		_ = batch.Rollback()
	}()
	hctx := batch.HandlerContext(ctx)
	if err := idx.Index(hctx, "urn:task:cedar", "task", "Dunmore Road viewing", cedar); err != nil {
		t.Fatalf("Index in batch: %v", err)
	}
	if got, found, err := idx.Owner(hctx, "urn:task:cedar"); err != nil || !found || got != cedar {
		t.Errorf("Owner in the same batch = %+v, %v, %v; want %+v, true, nil", got, found, err, cedar)
	}
}

func TestLexicalIndex_Owner_SQLite(t *testing.T) {
	t.Parallel()
	db := sqliteScopeDB(t)
	idx := ProvideLexicalIndex(db, config.Default(), &testLogger{})
	runOwnerCases(t, idx, db)

	// A row an old binary wrote has NULL owner columns: it counts as no row,
	// so the handler reads the owner from the events and re-indexes it.
	if err := db.Exec("INSERT INTO resource_search(content, id, type_slug) VALUES (?, ?, ?)",
		"Dunmore", "urn:task:old", "task").Error; err != nil {
		t.Fatalf("old-binary insert: %v", err)
	}
	if _, found, err := idx.Owner(context.Background(), "urn:task:old"); err != nil || found {
		t.Errorf("Owner of a NULL-owner row: found = %v, err = %v; want no row", found, err)
	}
}

func TestLexicalIndex_Owner_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	runOwnerCases(t, ProvideLexicalIndex(db, cfg, &testLogger{}), db)
}
