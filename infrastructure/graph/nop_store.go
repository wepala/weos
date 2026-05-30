package graph

import (
	"context"

	"github.com/wepala/weos/v3/domain/repositories"
)

// nopStore is the KnowledgeGraphStore returned when Oxigraph is not configured.
// All write operations are silent no-ops; reads return empty results. The
// projector and MCP tools branch on Active() once at construction (or
// per-call) to skip work cheaply when disabled.
type nopStore struct{}

// NewNopStore returns a KnowledgeGraphStore that quietly drops every write and
// returns empty results for every read. Useful when Oxigraph is not configured
// and as a stand-in in tests that don't care about the graph.
func NewNopStore() repositories.KnowledgeGraphStore { return nopStore{} }

func (nopStore) Active() bool { return false }

func (nopStore) AddTriples(_ context.Context, _ []repositories.Triple) error { return nil }

func (nopStore) RemoveTriples(_ context.Context, _ []repositories.Triple) error { return nil }

func (nopStore) RemoveSubject(_ context.Context, _ string) error { return nil }

func (nopStore) Query(_ context.Context, _ string) (repositories.KGQueryResult, error) {
	return repositories.KGQueryResult{}, nil
}

func (nopStore) Update(_ context.Context, _ string) error { return nil }

func (nopStore) LoadOntology(_ context.Context, _ string, _ []byte) error { return nil }

func (nopStore) Clear(_ context.Context) error { return nil }

func (nopStore) IsEmpty(_ context.Context) (bool, error) { return true, nil }
