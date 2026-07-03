package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubKGService records call arguments so tool handlers can be exercised
// without running real SPARQL.
type stubKGService struct {
	active           bool
	expandIRI        string
	expandDepth      int
	expandTriples    []repositories.Triple
	queryReturned    repositories.KGQueryResult
	queryErr         error
	searchQ          string
	searchClass      string
	searchLimit      int
	searchMatches    []repositories.KGTerm
	listClasses      []repositories.KGTerm
	describeIRI      string
	describeSamples  int
	describeReturned application.ClassDescription
	findFrom, findTo string
	findHops         int
	findTriples      []repositories.Triple
}

func (s *stubKGService) Active() bool { return s.active }
func (s *stubKGService) Query(_ context.Context, _ string) (repositories.KGQueryResult, error) {
	return s.queryReturned, s.queryErr
}
func (s *stubKGService) ExpandEntity(_ context.Context, iri string, depth int) ([]repositories.Triple, error) {
	s.expandIRI, s.expandDepth = iri, depth
	return s.expandTriples, nil
}
func (s *stubKGService) SearchEntities(_ context.Context, q, classIRI string, limit int) ([]repositories.KGTerm, error) {
	s.searchQ, s.searchClass, s.searchLimit = q, classIRI, limit
	return s.searchMatches, nil
}
func (s *stubKGService) ListClasses(_ context.Context) ([]repositories.KGTerm, error) {
	return s.listClasses, nil
}
func (s *stubKGService) DescribeClass(_ context.Context, iri string, n int) (application.ClassDescription, error) {
	s.describeIRI, s.describeSamples = iri, n
	return s.describeReturned, nil
}
func (s *stubKGService) FindPath(_ context.Context, from, to string, hops int) ([]repositories.Triple, error) {
	s.findFrom, s.findTo, s.findHops = from, to, hops
	return s.findTriples, nil
}

func TestRegisterKnowledgeGraphTools_RegistersAllTools(t *testing.T) {
	t.Parallel()
	server := gomcp.NewServer(&gomcp.Implementation{Name: "test"}, nil)
	registerKnowledgeGraphTools(server, &stubKGService{active: true})

	names := toolNames(t, server)
	expected := []string{
		"kg_sparql_query",
		"kg_expand_entity",
		"kg_search_entities",
		"kg_describe_class",
		"kg_list_classes",
		"kg_find_path",
	}
	for _, want := range expected {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool %q not registered; got %v", want, names)
		}
	}
}

func TestNewMCPServer_KnowledgeGraphServiceOptional(t *testing.T) {
	t.Parallel()
	// Passing nil kgService must not break server creation; the tools just
	// don't appear. (Mirrors how downstream callers like the HTTP handler
	// can opt out.)
	server, err := NewMCPServer(&stubResourceTypeService{}, &stubResourceService{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewMCPServer with nil KG: %v", err)
	}
	for _, n := range toolNames(t, server) {
		if len(n) >= 3 && n[:3] == "kg_" {
			t.Errorf("kg_ tool registered without service: %s", n)
		}
	}
}

func TestNewMCPServer_KnowledgeGraphRegisteredWhenServiceProvided(t *testing.T) {
	t.Parallel()
	server, err := NewMCPServer(
		&stubResourceTypeService{}, &stubResourceService{}, &stubKGService{active: true}, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}
	hasKG := false
	for _, n := range toolNames(t, server) {
		if n == "kg_sparql_query" {
			hasKG = true
			break
		}
	}
	if !hasKG {
		t.Error("expected kg_sparql_query when KG service is provided")
	}
}

func TestKgErr_TranslatesUnavailable(t *testing.T) {
	t.Parallel()
	got := kgErr(application.ErrKGUnavailable)
	if got == nil {
		t.Fatal("expected non-nil error")
	}
	if errors.Is(got, application.ErrKGUnavailable) {
		t.Error("translated error should not still wrap ErrKGUnavailable (it's a user-facing message)")
	}
	other := errors.New("oxigraph syntax error")
	if got := kgErr(other); got != other {
		t.Errorf("non-unavailable errors should pass through; got %v", got)
	}
}

func TestResultForm_DiscriminatesByPopulatedField(t *testing.T) {
	t.Parallel()
	b := true
	cases := []struct {
		name string
		in   repositories.KGQueryResult
		want string
	}{
		{"select", repositories.KGQueryResult{Vars: []string{"s"}}, "select"},
		{"ask", repositories.KGQueryResult{Boolean: &b}, "ask"},
		{"construct", repositories.KGQueryResult{Triples: []repositories.Triple{{Subject: "a"}}}, "construct"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resultForm(tc.in); got != tc.want {
				t.Errorf("resultForm() = %q, want %q", got, tc.want)
			}
		})
	}
}
