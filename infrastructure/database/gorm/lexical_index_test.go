package gorm

import (
	"context"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/internal/config"

	glebarez "github.com/glebarez/sqlite"
	gormlib "gorm.io/gorm"
)

// fts5TestDB opens an in-memory SQLite with FTS5 support. The project's
// default driver (mattn without the sqlite_fts5 tag) lacks the module, so the
// pure-Go glebarez driver — already a module dependency — exercises the real
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

func TestLexicalIndex_InactiveOnPostgres(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.DatabaseDSN = "host=localhost user=weos dbname=weos"
	idx := ProvideLexicalIndex(fts5TestDB(t), cfg, &testLogger{})
	if idx.Active() {
		t.Fatal("index must be inactive on PostgreSQL")
	}
	if err := idx.Index(context.Background(), "a", "b", "c"); err != nil {
		t.Errorf("nop Index must not error: %v", err)
	}
}
