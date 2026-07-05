//go:build oxigraph_embedded

package graph

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/fx/fxtest"

	"github.com/wepala/weos/v3/internal/config"
)

// An embedded store path activates the embedded backend (a real, Active store)
// even when no OXIGRAPH_URL is set.
func TestProvideKnowledgeGraphStore_EmbeddedPathActivates(t *testing.T) {
	cfg := config.Config{}
	cfg.Oxigraph.Path = filepath.Join(t.TempDir(), "graph")

	store := ProvideKnowledgeGraphStore(cfg, provLogger{}, fxtest.NewLifecycle(t))
	if !store.Active() {
		t.Fatal("an embedded store path must activate the embedded backend")
	}
}

// The embedded path wins over OXIGRAPH_URL when both are set.
func TestProvideKnowledgeGraphStore_EmbeddedPathWinsOverURL(t *testing.T) {
	cfg := config.Config{}
	cfg.Oxigraph.Path = filepath.Join(t.TempDir(), "graph")
	cfg.Oxigraph.URL = "http://127.0.0.1:1" // would fail to ping if selected
	cfg.Oxigraph.Enabled = true

	store := ProvideKnowledgeGraphStore(cfg, provLogger{}, fxtest.NewLifecycle(t))
	if !store.Active() {
		t.Fatal("the embedded path must be selected (and Active) ahead of OXIGRAPH_URL")
	}
}

// An unopenable embedded path degrades to nop (Active() == false) rather than
// failing the app — the soft-dependency contract.
func TestProvideKnowledgeGraphStore_EmbeddedOpenFailureFallsBackToNop(t *testing.T) {
	// A regular file where the store directory is expected: oxigraph can't
	// open a store there, so Open fails deterministically on every platform.
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.Oxigraph.Path = f

	store := ProvideKnowledgeGraphStore(cfg, provLogger{}, fxtest.NewLifecycle(t))
	if store.Active() {
		t.Fatal("an unopenable embedded path must degrade to the nop store")
	}
}
