package graph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/fx/fxtest"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

func newTestPerAccountStores(t *testing.T) *perAccountStores {
	t.Helper()
	return &perAccountStores{
		base:   t.TempDir(),
		logger: provLogger{},
		open:   map[string]repositories.KnowledgeGraphStore{},
	}
}

func TestIsSafeAccountID(t *testing.T) {
	cases := []struct {
		id   string
		safe bool
	}{
		{"local", true},
		{"2Nq8y3kFq9zK1abcDEF456ghijk", true}, // KSUID-shaped
		{"acct-01", true},
		{"a_b", true},
		{"", false},
		{"..", false},
		{"../other", false},
		{"a/b", false},
		{"a.b", false}, // dots are not in the allowed set
		{"a b", false},
		{"a\x00b", false},
		{"acct/../escape", false},
	}
	for _, c := range cases {
		if got := isSafeAccountID(c.id); got != c.safe {
			t.Errorf("isSafeAccountID(%q) = %v, want %v", c.id, got, c.safe)
		}
	}
}

func TestPerAccountStores_ForAccountEmptyIsErrNoAccount(t *testing.T) {
	f := newTestPerAccountStores(t)
	_, err := f.ForAccount(context.Background(), "")
	if !errors.Is(err, repositories.ErrNoAccount) {
		t.Fatalf("ForAccount(\"\") = %v, want ErrNoAccount", err)
	}
}

func TestPerAccountStores_ForAccountRejectsUnsafeID(t *testing.T) {
	f := newTestPerAccountStores(t)
	// A traversal id must be rejected before any store/dir is opened.
	if _, err := f.ForAccount(context.Background(), "../escape"); err == nil {
		t.Fatal("ForAccount with a traversal id should error")
	}
	if entries, _ := os.ReadDir(f.base); len(entries) != 0 {
		t.Fatalf("unsafe id must not create anything under base, found %d entries", len(entries))
	}
}

func TestPerAccountStores_TruncateRemovesAccountDirsKeepsBase(t *testing.T) {
	f := newTestPerAccountStores(t)
	for _, id := range []string{"harbor", "cedar"} {
		if err := os.MkdirAll(filepath.Join(f.base, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A non-account entry an operator might keep under the base must survive: a
	// misconfigured base can't turn a rebuild into a data-wipe of unrelated files.
	keep := filepath.Join(f.base, "operator-notes.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(context.Background()); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if _, err := os.Stat(f.base); err != nil {
		t.Fatalf("base dir must survive Truncate: %v", err)
	}
	for _, id := range []string{"harbor", "cedar"} {
		if _, err := os.Stat(filepath.Join(f.base, id)); !os.IsNotExist(err) {
			t.Fatalf("account dir %q should be removed by Truncate", id)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a non-account file must survive Truncate: %v", err)
	}
}

func TestPerAccountStores_TruncateMissingBaseIsNil(t *testing.T) {
	f := &perAccountStores{
		base:   filepath.Join(t.TempDir(), "does-not-exist"),
		logger: provLogger{},
		open:   map[string]repositories.KnowledgeGraphStore{},
	}
	if err := f.Truncate(context.Background()); err != nil {
		t.Fatalf("Truncate on missing base should be nil, got %v", err)
	}
}

func TestPerAccountStores_CloseEmptyIsNil(t *testing.T) {
	f := newTestPerAccountStores(t)
	if err := f.Close(); err != nil {
		t.Fatalf("Close on empty resolver should be nil, got %v", err)
	}
}

// TestProvideKnowledgeGraphStores_PerAccountDegradesToNopWithoutTag pins the
// fail-safe boot: with per-account requested but the embedded backend absent
// (this test binary is built WITHOUT the oxigraph_embedded tag), the resolver
// degrades to a nop single store — the graph is off, not a crash — rather than
// silently running per-account against a missing backend.
func TestProvideKnowledgeGraphStores_PerAccountDegradesToNopWithoutTag(t *testing.T) {
	cfg := config.Config{}
	cfg.Oxigraph.AccountStorePath = t.TempDir()
	stores := ProvideKnowledgeGraphStores(cfg, provLogger{}, fxtest.NewLifecycle(t))
	if stores.Active() {
		t.Error("without the embedded tag, per-account mode must degrade to an inactive store")
	}
	if stores.PerAccount() {
		t.Error("a degraded nop fallback must not report per-account mode")
	}
}

func TestProvideKnowledgeGraphStores_SingleTenantUnchanged(t *testing.T) {
	// No AccountStorePath, no URL, no Path -> nop single store, PerAccount false.
	stores := ProvideKnowledgeGraphStores(config.Config{}, provLogger{}, fxtest.NewLifecycle(t))
	if stores.PerAccount() {
		t.Error("single-tenant config must not report per-account mode")
	}
	if stores.Active() {
		t.Error("unconfigured graph must be inactive")
	}
	// ForAccount ignores the account and returns the one store in single-tenant.
	got, err := stores.ForAccount(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("single-tenant ForAccount should not error: %v", err)
	}
	if got == nil || got.Active() {
		t.Error("single-tenant ForAccount should return the (inactive nop) process store")
	}
}
