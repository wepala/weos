package graph

import (
	"context"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

func TestNopStore_QuietlyDoesNothing(t *testing.T) {
	ctx := context.Background()
	s := NewNopStore()

	if s.Active() {
		t.Fatal("nop store reported Active()==true")
	}

	t.Run("writes succeed", func(t *testing.T) {
		if err := s.AddTriples(ctx, []repositories.Triple{{Subject: "s", Predicate: "p", Object: "o"}}); err != nil {
			t.Fatalf("AddTriples: %v", err)
		}
		if err := s.RemoveTriples(ctx, []repositories.Triple{{Subject: "s", Predicate: "p", Object: "o"}}); err != nil {
			t.Fatalf("RemoveTriples: %v", err)
		}
		if err := s.RemoveSubject(ctx, "s"); err != nil {
			t.Fatalf("RemoveSubject: %v", err)
		}
		if err := s.Update(ctx, "INSERT DATA { }"); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if err := s.LoadOntology(ctx, "text/turtle", []byte("@prefix : <#> .")); err != nil {
			t.Fatalf("LoadOntology: %v", err)
		}
		if err := s.Clear(ctx); err != nil {
			t.Fatalf("Clear: %v", err)
		}
	})

	t.Run("reads return empty", func(t *testing.T) {
		res, err := s.Query(ctx, "SELECT * WHERE { ?s ?p ?o }")
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if res.Bindings != nil || res.Boolean != nil || res.Triples != nil {
			t.Fatalf("nop store returned non-empty result: %+v", res)
		}
		empty, err := s.IsEmpty(ctx)
		if err != nil {
			t.Fatalf("IsEmpty: %v", err)
		}
		if !empty {
			t.Fatal("nop store should always report empty")
		}
	})
}
