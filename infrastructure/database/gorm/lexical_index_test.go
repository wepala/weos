package gorm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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

	if err := idx.Index(ctx, "urn:fact:1", "fact", "Akeem prefers PRs based on the v3 branch", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := idx.Index(ctx, "urn:note:2", "note", "quarterly finance review with Wepala", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	hits, err := idx.Search(ctx, repositories.LexicalQuery{Text: "akeem v3", Limit: 10}, nil)
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
	if err := idx.Index(ctx, "urn:fact:1", "fact", "completely different text", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "akeem", Limit: 10}, nil)
	if err != nil {
		t.Fatalf("Search after reindex: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none after content replaced", hits)
	}

	if err := idx.Remove(ctx, "urn:note:2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "finance", Limit: 10}, nil)
	if err != nil {
		t.Fatalf("Search after removal: %v", err)
	}
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
	if err := idx.Index(ctx, "urn:x", "note", "hello world", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// FTS5 operators and column filters in user input must not error or be
	// interpreted — each token is phrase-quoted.
	for _, q := range []string{`hello NEAR world`, `content:hello`, `"hello`, `hello AND -world`} {
		if _, err := idx.Search(ctx, repositories.LexicalQuery{Text: q, Limit: 10}, nil); err != nil {
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
	if err := idx.Index(context.Background(), "a", "b", "c", repositories.LexicalOwner{}); err != nil {
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
		if err := idx.Index(ctx, id, "project", "invoice invoice invoice templates", repositories.LexicalOwner{}); err != nil {
			t.Fatalf("Index %s: %v", id, err)
		}
	}
	if err := idx.Index(ctx, "urn:task:1", "task", "Harbor Legal invoice chase", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index task: %v", err)
	}

	hits, err := idx.Search(ctx, repositories.LexicalQuery{Text: "invoice", Limit: 20, TypeSlugs: []string{"task"}}, nil)
	if err != nil {
		t.Fatalf("Search narrowed: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "urn:task:1" || hits[0].TypeSlug != "task" {
		t.Errorf("narrowed hits = %+v, want only the task despite 25 better-ranked projects", hits)
	}

	hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "invoice", Limit: 100, TypeSlugs: []string{"recipe"}}, nil)
	if err != nil {
		t.Fatalf("Search unknown type: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("unknown-type hits = %+v, want none", hits)
	}

	hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "invoice", Limit: 100}, nil)
	if err != nil {
		t.Fatalf("Search unnarrowed: %v", err)
	}
	if len(hits) != 26 {
		t.Errorf("unnarrowed hits = %d, want all 26", len(hits))
	}

	hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "invoice", Limit: 100, TypeSlugs: []string{"task", "project"}}, nil)
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
	if err := idx.Index(ctx, "urn:fact:1", "fact", "Akeem prefers PRs based on the v3 branch", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := idx.Index(ctx, "urn:note:2", "note", "quarterly finance review with Wepala", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	hits, err := idx.Search(ctx, repositories.LexicalQuery{Text: "akeem v3", Limit: 10}, nil)
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
	if err := idx.Index(ctx, "urn:fact:1", "fact", "completely different text", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("re-Index: %v", err)
	}
	var rows int64
	if err := db.Raw("SELECT count(*) FROM resource_search WHERE id = ?", "urn:fact:1").Scan(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows for urn:fact:1 = %d, want 1 after an upsert", rows)
	}
	if hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "akeem", Limit: 10}, nil); err != nil {
		t.Fatalf("Search after reindex: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none after content replaced", hits)
	}

	if err := idx.Remove(ctx, "urn:note:2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "finance", Limit: 10}, nil); err != nil {
		t.Fatalf("Search after removal: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none after removal", hits)
	}

	if err := idx.Index(ctx, "urn:note:3", "note", "finance again", repositories.LexicalOwner{}); err != nil {
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
	if err := idx.Index(ctx, "urn:x", "note", "hello world", repositories.LexicalOwner{}); err != nil {
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
		hits, err := idx.Search(ctx, repositories.LexicalQuery{Text: q, Limit: 10}, nil)
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
	if err := idx.Index(ctx, "urn:task:a", "task", "Harbor one invoice", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := idx.Index(ctx, "urn:task:b", "task", "Harbor invoice Harbor invoice Harbor invoice", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	hits, err := idx.Search(ctx, repositories.LexicalQuery{Text: "harbor invoice", Limit: 10}, nil)
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
	if err := idx.Index(context.Background(), "urn:x", "note", "kept across a restart", repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	cfg := config.Default()
	cfg.DatabaseDSN = "host=localhost dbname=restart" // only IsPostgres is read
	again := ProvideLexicalIndex(db, cfg, &testLogger{})
	if !again.Active() {
		t.Fatal("a second provide over the existing table must stay active")
	}
	hits, err := again.Search(context.Background(), repositories.LexicalQuery{Text: "restart", Limit: 10}, nil)
	if err != nil || len(hits) != 1 {
		t.Errorf("hits = %+v, err = %v; want the row indexed before the restart", hits, err)
	}
}

func TestCapLexicalContent(t *testing.T) {
	t.Parallel()

	if got := capLexicalContent("short text"); got != "short text" {
		t.Errorf("short content = %q, want it unchanged", got)
	}

	exact := strings.Repeat("a", lexicalContentBudget)
	if got := capLexicalContent(exact); got != exact {
		t.Errorf("content of exactly the budget was cut to %d bytes", len(got))
	}

	// "€" is three bytes; place it so it straddles the budget.
	straddle := strings.Repeat("a", lexicalContentBudget-1) + "€" + "tail"
	got := capLexicalContent(straddle)
	if len(got) > lexicalContentBudget {
		t.Errorf("capped length = %d, want at most %d", len(got), lexicalContentBudget)
	}
	if !utf8.ValidString(got) {
		t.Error("capped content is not valid UTF-8: the cut split a rune")
	}
	if got != strings.Repeat("a", lexicalContentBudget-1) {
		t.Errorf("capped content kept %d bytes, want the %d before the split rune", len(got), lexicalContentBudget-1)
	}
}

// largeLexicalContent is far over PostgreSQL's 1 MB tsvector limit when every
// word is distinct: a word near the start, then about 2 MB of distinct words,
// then a word near the end.
func largeLexicalContent() string {
	var b strings.Builder
	b.WriteString("zebrafinch ")
	for i := 0; b.Len() < 2<<20; i++ {
		fmt.Fprintf(&b, "w%07d ", i)
	}
	b.WriteString("kingfisher")
	return b.String()
}

// runLargeContentCases pins the content cap on an engine: a resource far over
// the budget is indexed, is found by a word near its start, and is not found
// by a word past the budget — the same answer on both engines.
func runLargeContentCases(t *testing.T, idx repositories.LexicalIndex) {
	t.Helper()
	ctx := context.Background()
	if err := idx.Index(ctx, "urn:note:big", "note", largeLexicalContent(), repositories.LexicalOwner{}); err != nil {
		t.Fatalf("Index of a large resource: %v", err)
	}
	hits, err := idx.Search(ctx, repositories.LexicalQuery{Text: "zebrafinch", Limit: 10}, nil)
	if err != nil {
		t.Fatalf("Search near the start: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "urn:note:big" {
		t.Errorf("hits = %+v, want the large resource found by a word near its start", hits)
	}
	hits, err = idx.Search(ctx, repositories.LexicalQuery{Text: "kingfisher", Limit: 10}, nil)
	if err != nil {
		t.Fatalf("Search past the budget: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none: a word past the budget is not indexed", hits)
	}
}

func TestLexicalIndex_LargeResource_SQLite(t *testing.T) {
	t.Parallel()

	idx := ProvideLexicalIndex(fts5TestDB(t), config.Default(), &testLogger{})
	if !idx.Active() {
		t.Skip("FTS5 unavailable in this build")
	}
	runLargeContentCases(t, idx)
}

func TestLexicalIndex_LargeResource_Postgres(t *testing.T) {
	t.Parallel()

	idx, _ := postgresLexicalIndexForTest(t)
	runLargeContentCases(t, idx)
}

// errorRecordingLogger keeps every Error message.
type errorRecordingLogger struct {
	testLogger
	mu     sync.Mutex
	errors []string
}

func (l *errorRecordingLogger) Error(_ context.Context, msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, msg)
}

// A hardened PostgreSQL (15+ grants no one CREATE on the public schema) lets
// the app user connect but not create the index. The index degrades, and the
// log names the privilege the operator must grant.
func TestLexicalIndex_NamesTheMissingPrivilege_Postgres(t *testing.T) {
	t.Parallel()

	admin := os.Getenv("TEST_POSTGRES_DSN")
	if admin == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL lexical index is skipped")
	}
	dsn, err := testpg.FreshDatabase(t, admin, "weos_lexical_priv")
	if err != nil {
		t.Fatalf("fresh database: %v", err)
	}
	owner := openPostgresReplica(t, dsn)
	role := fmt.Sprintf("weos_lx_noddl_%d", time.Now().UnixNano())
	if err := owner.Exec("CREATE ROLE " + role + " NOLOGIN").Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() {
		if err := owner.Exec("DROP ROLE IF EXISTS " + role).Error; err != nil {
			t.Errorf("drop role %s: %v", role, err)
		}
	})
	if err := owner.Exec("REVOKE CREATE ON SCHEMA public FROM PUBLIC").Error; err != nil {
		t.Fatalf("revoke create: %v", err)
	}

	// One connection, so the SET ROLE holds for every statement the index runs.
	restricted := openPostgresReplica(t, dsn)
	sqlDB, err := restricted.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := restricted.Exec("SET ROLE " + role).Error; err != nil {
		t.Fatalf("set role: %v", err)
	}

	logger := &errorRecordingLogger{}
	cfg := config.Default()
	cfg.DatabaseDSN = dsn
	idx := ProvideLexicalIndex(restricted, cfg, logger)
	if idx.Active() {
		t.Fatal("index must be inactive when the user cannot create it")
	}
	if len(logger.errors) != 1 {
		t.Fatalf("errors logged = %q, want one", logger.errors)
	}
	msg := logger.errors[0]
	for _, want := range []string{"CREATE privilege on the schema", "graph label search"} {
		if !strings.Contains(msg, want) {
			t.Errorf("logged %q, want it to say %q", msg, want)
		}
	}
}
