package gorm

import (
	"sort"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

// TestFindAllByTypeAndIDs_ReadsTheNamedResourcesOfTheType pins the read the
// MCP flat listing makes for a page's documents (wm-yh1xt5): it returns the
// named resources of the type and nothing else — not an unnamed resource,
// not a resource of another type, not an identifier with no resource.
func TestFindAllByTypeAndIDs_ReadsTheNamedResourcesOfTheType(t *testing.T) {
	t.Parallel()
	repo := newDocumentFilterRepo(t, newTestDB(t), "task", "", [][2]string{
		{"urn:task:1", `{"@graph":[{"@id":"urn:task:1","status":"open"}]}`},
		{"urn:task:2", `{"@graph":[{"@id":"urn:task:2","status":"done"}]}`},
		{"urn:task:3", `{"@graph":[{"@id":"urn:task:3","status":"open"}]}`},
	})
	if err := repo.Save(t.Context(), makeTestResource(t, "urn:note:1", "note", `{"name":"x"}`)); err != nil {
		t.Fatal(err)
	}
	var _ repositories.ResourceIDFinder = repo

	got, err := repo.FindAllByTypeAndIDs(t.Context(), "task",
		[]string{"urn:task:1", "urn:task:3", "urn:note:1", "urn:task:9"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got))
	for _, e := range got {
		ids = append(ids, e.GetID())
		if !strings.Contains(string(e.Data()), `"status"`) {
			t.Errorf("%s came back without its stored document: %s", e.GetID(), e.Data())
		}
	}
	sort.Strings(ids)
	if strings.Join(ids, ",") != "urn:task:1,urn:task:3" {
		t.Errorf("read %v, want [urn:task:1 urn:task:3]", ids)
	}
}

// TestFindAllByTypeAndIDs_AppliesTheVisibilityScope pins that the read by id
// sees what the listing sees: a resource another agent created, with no read
// grant, is left out.
func TestFindAllByTypeAndIDs_AppliesTheVisibilityScope(t *testing.T) {
	t.Parallel()
	repo := newDocumentFilterRepo(t, newTestDB(t), "task", "", nil)
	for _, e := range [][3]string{{"urn:task:1", "agent-1"}, {"urn:task:2", "agent-2"}} {
		if err := repo.Save(t.Context(), makeTestResourceForAgent(t, e[0], "task", `{"name":"x"}`, e[1], "acct-1")); err != nil {
			t.Fatal(err)
		}
	}
	scope := &repositories.VisibilityScope{AgentID: "agent-1", AccountID: "acct-1"}
	got, err := repo.FindAllByTypeAndIDs(t.Context(), "task", []string{"urn:task:1", "urn:task:2"}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GetID() != "urn:task:1" {
		t.Errorf("read %d resources, want only urn:task:1", len(got))
	}
}

func TestFindAllByTypeAndIDs_NoIdentifiersReadsNothing(t *testing.T) {
	t.Parallel()
	repo := newDocumentFilterRepo(t, newTestDB(t), "task", "", [][2]string{{"urn:task:1", `{"name":"x"}`}})
	got, err := repo.FindAllByTypeAndIDs(t.Context(), "task", nil, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("got %d resources, %v; want none and no error", len(got), err)
	}
}

// TestFindAllByTypeAndIDs_AnAncestorsFlatRowNamesTheSubtypeToReadItBy pins
// the facts the MCP flat overlay relies on to read a subtype row's document:
// the ancestor's flat row carries the subtype's own typeSlug, and the read by
// id finds the resource under that type — not under the ancestor's.
func TestFindAllByTypeAndIDs_AnAncestorsFlatRowNamesTheSubtypeToReadItBy(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)
	if err := repo.Save(ctx, makeTestResource(t, "urn:loan:1", "loan", `{"name":"Home Loan"}`)); err != nil {
		t.Fatal(err)
	}
	page, err := repo.FindAllByTypeFlat(ctx, "instrument", "", 10, repositories.SortOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Data[0]["typeSlug"] != "loan" {
		t.Fatalf("instrument flat page = %v, want one row with typeSlug loan", page.Data)
	}
	asAncestor, err := repo.FindAllByTypeAndIDs(ctx, "instrument", []string{"urn:loan:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(asAncestor) != 0 {
		t.Errorf("read under the ancestor found %d resources, want none", len(asAncestor))
	}
	asSubtype, err := repo.FindAllByTypeAndIDs(ctx, "loan", []string{"urn:loan:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(asSubtype) != 1 || asSubtype[0].GetID() != "urn:loan:1" {
		t.Errorf("read under the row's typeSlug found %d resources, want urn:loan:1", len(asSubtype))
	}
}
