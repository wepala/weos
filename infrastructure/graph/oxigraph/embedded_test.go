//go:build oxigraph_embedded

package oxigraph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

func mustEmbedded(t *testing.T, dir string) *EmbeddedStore {
	t.Helper()
	s, err := NewEmbeddedStore(filepath.Join(dir, "store"), nil)
	if err != nil {
		t.Fatalf("NewEmbeddedStore: %v", err)
	}
	es := s.(*EmbeddedStore)
	t.Cleanup(func() { _ = es.Close() })
	return es
}

func TestEmbeddedStore_AddQuerySelectRoundTrip(t *testing.T) {
	s := mustEmbedded(t, t.TempDir())
	ctx := context.Background()
	if !s.Active() {
		t.Fatal("a freshly opened embedded store must be Active")
	}
	err := s.AddTriples(ctx, []repositories.Triple{
		{Subject: "urn:account:1", Predicate: "https://schema.org/name", Object: `"Everyday"`},
	})
	if err != nil {
		t.Fatalf("AddTriples: %v", err)
	}
	res, err := s.Query(ctx, `SELECT ?name WHERE { <urn:account:1> <https://schema.org/name> ?name }`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := res.Vars; len(got) != 1 || got[0] != "name" {
		t.Fatalf("Vars = %v, want [name]", got)
	}
	if len(res.Bindings) != 1 || res.Bindings[0]["name"].Value != "Everyday" {
		t.Fatalf("Bindings = %v, want name=Everyday", res.Bindings)
	}
}

func TestEmbeddedStore_LoadJSONLDOntology(t *testing.T) {
	s := mustEmbedded(t, t.TempDir())
	ctx := context.Background()
	doc := []byte(`{"@context":{"name":"https://schema.org/name"},` +
		`"@id":"urn:agent:acme","name":"Acme"}`)
	if err := s.LoadOntology(ctx, "application/ld+json", doc); err != nil {
		t.Fatalf("LoadOntology JSON-LD: %v", err)
	}
	res, err := s.Query(ctx, `ASK { <urn:agent:acme> <https://schema.org/name> "Acme" }`)
	if err != nil {
		t.Fatalf("Query ASK: %v", err)
	}
	if res.Boolean == nil || !*res.Boolean {
		t.Fatalf("loaded JSON-LD triple not found: %+v", res)
	}
}

func TestEmbeddedStore_RemoveSubjectAndIsEmpty(t *testing.T) {
	s := mustEmbedded(t, t.TempDir())
	ctx := context.Background()
	if empty, err := s.IsEmpty(ctx); err != nil || !empty {
		t.Fatalf("new store IsEmpty = %v, %v; want true, nil", empty, err)
	}
	tr := repositories.Triple{Subject: "urn:x:1", Predicate: "urn:p", Object: "urn:o:1"}
	if err := s.AddTriples(ctx, []repositories.Triple{tr}); err != nil {
		t.Fatalf("AddTriples: %v", err)
	}
	if empty, _ := s.IsEmpty(ctx); empty {
		t.Fatal("store must not be empty after an add")
	}
	if err := s.RemoveSubject(ctx, "urn:x:1"); err != nil {
		t.Fatalf("RemoveSubject: %v", err)
	}
	if empty, _ := s.IsEmpty(ctx); !empty {
		t.Fatal("store must be empty after removing the only subject")
	}
}

func TestEmbeddedStore_SurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	first, err := NewEmbeddedStore(filepath.Join(dir, "store"), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tr := repositories.Triple{Subject: "urn:durable:1", Predicate: "urn:p", Object: `"kept"`}
	if err := first.AddTriples(context.Background(), []repositories.Triple{tr}); err != nil {
		t.Fatalf("AddTriples: %v", err)
	}
	// Close releases the directory lock so the same path can be reopened.
	if err := first.(*EmbeddedStore).Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, err := NewEmbeddedStore(filepath.Join(dir, "store"), nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = second.(*EmbeddedStore).Close() }()
	res, err := second.Query(context.Background(),
		`ASK { <urn:durable:1> <urn:p> "kept" }`)
	if err != nil {
		t.Fatalf("Query after reopen: %v", err)
	}
	if res.Boolean == nil || !*res.Boolean {
		t.Fatal("data written before Close must survive a reopen of the same path")
	}
}
