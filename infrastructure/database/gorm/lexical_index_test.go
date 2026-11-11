package gorm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/testpg"

	glebarez "github.com/glebarez/sqlite"
	gormlib "gorm.io/gorm"
)

// fts5TestDB opens an in-memory SQLite with FTS5 support via the pure-Go
// glebarez driver — the same driver production uses — exercising the real
// FTS5 SQL these methods emit.
func fts5TestDB(t *testing.T) *gormlib.DB {
	t.Helper()
	db, err := gormlib.Open(glebarez.Open(":memory:"), &gormlib.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func TestLexicalIndex_RoundTrip(t *testing.T) {
	t.Parallel()

	idx := ProvideLexicalIndex(fts5TestDB(t), config.Default(), &testLogger{})
	if !idx.Active() {
		t.Skip("FTS5 unavailable in this build")
	}
	ctx := context.Background()

	if err := idx.Index(ctx, "urn:fact:1", "fact", "Akeem prefers PRs based on the v3 branch"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := idx.Index(ctx, "urn:note:2", "note", "quarterly finance review with Wepala"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	hits, err := idx.Search(ctx, "akeem v3", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "urn:fact:1" || hits[0].TypeSlug != "fact" {
		t.Fatalf("hits = %+v, want the fact", hits)
	}
	if !strings.Contains(hits[0].Snippet, "v3") {
		t.Errorf("snippet = %q, want it to contain the match", hits[0].Snippet)
	}

	// Re-indexing replaces content (no duplicate rows).
	if err := idx.Index(ctx, "urn:fact:1", "fact", "completely different text"); err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	hits, err = idx.Search(ctx, "akeem", 10)
	if err != nil {
		t.Fatalf("Search after reindex: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none after content replaced", hits)
	}

	if err := idx.Remove(ctx, "urn:note:2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	hits, _ = idx.Search(ctx, "finance", 10)
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none after removal", hits)
	}
}

func TestLexicalIndex_QuerySyntaxIsNeutralized(t *testing.T) {
	t.Parallel()

	idx := ProvideLexicalIndex(fts5TestDB(t), config.Default(), &testLogger{})
	if !idx.Active() {
		t.Skip("FTS5 unavailable in this build")
	}
	ctx := context.Background()
	if err := idx.Index(ctx, "urn:x", "note", "hello world"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// FTS5 operators and column filters in user input must not error or be
	// interpreted — each token is phrase-quoted.
	for _, q := range []string{`hello NEAR world`, `content:hello`, `"hello`, `hello AND -world`} {
		if _, err := idx.Search(ctx, q, 10); err != nil {
			t.Errorf("Search(%q) errored: %v — query syntax not neutralized", q, err)
		}
	}
}

// TestLexicalIndex_DegradesWhenPostgresIndexCannotBeCreated replaces the old
// TestLexicalIndex_InactiveOnPostgres: PostgreSQL now has a lexical index
// (TestLexicalIndex_ActiveOnPostgres), so what is left to hold without a
// server is the degradation — a PostgreSQL configuration whose index DDL fails
// yields an inactive index rather than a failed startup.
func TestLexicalIndex_DegradesWhenPostgresIndexCannotBeCreated(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.DatabaseDSN = "host=localhost user=weos dbname=weos"
	// A SQLite handle cannot run the PostgreSQL DDL, so creation fails.
	idx := ProvideLexicalIndex(fts5TestDB(t), cfg, &testLogger{})
	if idx.Active() {
		t.Fatal("index must be inactive when the PostgreSQL index cannot be created")
	}
	if idx.Engine() != "" {
		t.Errorf("Engine() = %q, want empty for an inactive index", idx.Engine())
	}
	if err := idx.Index(context.Background(), "a", "b", "c"); err != nil {
		t.Errorf("nop Index must not error: %v", err)
	}
}

func TestLexicalIndex_TypeFilter_SQLite(t *testing.T) {
	t.Parallel()

	idx := ProvideLexicalIndex(fts5TestDB(t), config.Default(), &testLogger{})
	if !idx.Active() {
		t.Skip("FTS5 unavailable in this build")
	}
	if idx.Engine() != repositories.LexicalEngineFTS5 {
		t.Errorf("Engine() = %q, want fts5", idx.Engine())
	}
	runTypeFilterCases(t, idx)
}

// runTypeFilterCases holds the type-filter contract both engines share: a
// filter narrows the hits, the limit is spent on the narrowed type, an
// unknown type matches nothing, and no filter matches every type.
func runTypeFilterCases(t *testing.T, idx repositories.LexicalIndex) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("urn:project:%02d", i)
		if err := idx.Index(ctx, id, "project", "invoice invoice invoice templates"); err != nil {
			t.Fatalf("Index %s: %v", id, err)
		}
	}
	if err := idx.Index(ctx, "urn:task:1", "task", "Harbor Legal invoice chase"); err != nil {
		t.Fatalf("Index task: %v", err)
	}

	hits, err := idx.Search(ctx, "invoice", 20, "task")
	if err != nil {
		t.Fatalf("Search narrowed: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "urn:task:1" || hits[0].TypeSlug != "task" {
		t.Errorf("narrowed hits = %+v, want only the task despite 25 better-ranked projects", hits)
	}

	hits, err = idx.Search(ctx, "invoice", 100, "recipe")
	if err != nil {
		t.Fatalf("Search unknown type: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("unknown-type hits = %+v, want none", hits)
	}

	hits, err = idx.Search(ctx, "invoice", 100)
	if err != nil {
		t.Fatalf("Search unnarrowed: %v", err)
	}
	if len(hits) != 26 {
		t.Errorf("unnarrowed hits = %d, want all 26", len(hits))
	}

	hits, err = idx.Search(ctx, "invoice", 100, "task", "project")
	if err != nil {
		t.Fatalf("Search two types: %v", err)
	}
	if len(hits) != 26 {
		t.Errorf("two-type hits = %d, want 26", len(hits))
	}
}

// postgresLexicalIndex opens a fresh PostgreSQL database and provides the
// index on it. Skips without TEST_POSTGRES_DSN.
func postgresLexicalIndexForTest(t *testing.T) (repositories.LexicalIndex, *gormlib.DB) {
	t.Helper()
	admin := os.Getenv("TEST_POSTGRES_DSN")
	if admin == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL lexical index is skipped")
	}
	dsn, err := testpg.FreshDatabase(t, admin, "weos_lexical_unit")
	if err != nil {
		t.Fatalf("fresh database: %v", err)
	}
	db := openPostgresReplica(t, dsn)
	cfg := config.Default()
	cfg.DatabaseDSN = dsn
	return ProvideLexicalIndex(db, cfg, &testLogger{}), db
}

func TestLexicalIndex_ActiveOnPostgres(t *testing.T) {
	t.Parallel()

	idx, _ := postgresLexicalIndexForTest(t)
	if !idx.Active() {
		t.Fatal("index must be active on PostgreSQL")
	}
	if idx.Engine() != repositories.LexicalEnginePostgres {
		t.Errorf("Engine() = %q, want %q", idx.Engine(), repositories.LexicalEnginePostgres)
	}
}

func TestLexicalIndex_RoundTrip_Postgres(t *testing.T) {
	t.Parallel()

	idx, db := postgresLexicalIndexForTest(t)
	ctx := context.Background()
	if err := idx.Index(ctx, "urn:fact:1", "fact", "Akeem prefers PRs based on the v3 branch"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := idx.Index(ctx, "urn:note:2", "note", "quarterly finance review with Wepala"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	hits, err := idx.Search(ctx, "akeem v3", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "urn:fact:1" || hits[0].TypeSlug != "fact" {
		t.Fatalf("hits = %+v, want the fact", hits)
	}
	if !strings.Contains(hits[0].Snippet, "v3") {
		t.Errorf("snippet = %q, want it to contain the match", hits[0].Snippet)
	}

	// Re-indexing replaces content: one row, and the old words stop matching.
	if err := idx.Index(ctx, "urn:fact:1", "fact", "completely different text"); err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	var rows int64
	if err := db.Raw("SELECT count(*) FROM resource_search WHERE id = ?", "urn:fact:1").Scan(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows for urn:fact:1 = %d, want 1 after an upsert", rows)
	}
	if hits, _ = idx.Search(ctx, "akeem", 10); len(hits) != 0 {
		t.Errorf("hits = %+v, want none after content replaced", hits)
	}

	if err := idx.Remove(ctx, "urn:note:2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if hits, _ = idx.Search(ctx, "finance", 10); len(hits) != 0 {
		t.Errorf("hits = %+v, want none after removal", hits)
	}

	if err := idx.Index(ctx, "urn:note:3", "note", "finance again"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := idx.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if err := db.Raw("SELECT count(*) FROM resource_search").Scan(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Errorf("rows after Clear = %d, want 0", rows)
	}
}

func TestLexicalIndex_QuerySyntaxIsNeutralized_Postgres(t *testing.T) {
	t.Parallel()

	idx, _ := postgresLexicalIndexForTest(t)
	ctx := context.Background()
	if err := idx.Index(ctx, "urn:x", "note", "hello world"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	cases := map[string]int{
		`hello & world`:       1,
		`hello | world`:       1,
		`hello:*`:             1,
		`!hello`:              1,
		`"hello world"`:       1,
		`"world hello"`:       1, // quotes are not an adjacency phrase
		`hello <-> world`:     1,
		`(hello) & !(world)`:  1, // ! is not negation
		`hello or nothing`:    0, // "or" is a word, AND-ed, not an operator
		`hello -world`:        1, // a leading - is not negation
		`hello NEAR world`:    0, // NEAR is a word the text lacks
		`& | ! :*`:            0, // no words: matches nothing, not everything
		``:                    0,
		`'; DROP TABLE x; --`: 0,
	}
	for q, want := range cases {
		hits, err := idx.Search(ctx, q, 10)
		if err != nil {
			t.Errorf("Search(%q) errored: %v — query syntax not neutralized", q, err)
			continue
		}
		if len(hits) != want {
			t.Errorf("Search(%q) = %d hits, want %d", q, len(hits), want)
		}
	}
}

func TestLexicalIndex_RanksMoreMentionsFirst_Postgres(t *testing.T) {
	t.Parallel()

	idx, _ := postgresLexicalIndexForTest(t)
	ctx := context.Background()
	if err := idx.Index(ctx, "urn:task:a", "task", "Harbor one invoice"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := idx.Index(ctx, "urn:task:b", "task", "Harbor invoice Harbor invoice Harbor invoice"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	hits, err := idx.Search(ctx, "harbor invoice", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 || hits[0].ID != "urn:task:b" {
		t.Errorf("hits = %+v, want urn:task:b first", hits)
	}
}

func TestLexicalIndex_TypeFilter_Postgres(t *testing.T) {
	t.Parallel()

	idx, _ := postgresLexicalIndexForTest(t)
	runTypeFilterCases(t, idx)
}

func TestLexicalIndex_SchemaCreationIsIdempotent_Postgres(t *testing.T) {
	t.Parallel()

	idx, db := postgresLexicalIndexForTest(t)
	if err := idx.Index(context.Background(), "urn:x", "note", "kept across a restart"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	cfg := config.Default()
	cfg.DatabaseDSN = "host=localhost dbname=restart" // only IsPostgres is read
	again := ProvideLexicalIndex(db, cfg, &testLogger{})
	if !again.Active() {
		t.Fatal("a second provide over the existing table must stay active")
	}
	hits, err := again.Search(context.Background(), "restart", 10)
	if err != nil || len(hits) != 1 {
		t.Errorf("hits = %+v, err = %v; want the row indexed before the restart", hits, err)
	}
}
