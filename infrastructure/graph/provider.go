// Package graph wires the optional knowledge-graph projection store. When the
// Oxigraph endpoint is configured the provider returns the HTTP-backed store;
// otherwise it returns a nop store so call sites never have to nil-check.
package graph

import (
	"context"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
	"github.com/wepala/weos/v3/internal/config"
)

// ProvideKnowledgeGraphStore returns a KnowledgeGraphStore for the application
// container. If Oxigraph is not configured (or explicitly disabled), a nop
// store is returned and downstream code becomes a no-op.
func ProvideKnowledgeGraphStore(
	cfg config.Config, logger entities.Logger,
) repositories.KnowledgeGraphStore {
	if !cfg.Oxigraph.Active() {
		logger.Debug(context.Background(), "knowledge graph: Oxigraph not configured, using nop store")
		return NewNopStore()
	}
	store, err := oxigraph.NewStore(oxigraph.Options{
		Endpoint:            cfg.Oxigraph.URL,
		Username:            cfg.Oxigraph.Username,
		Password:            cfg.Oxigraph.Password,
		QueryTimeoutSeconds: cfg.Oxigraph.QueryTimeoutSeconds,
		Logger:              logger,
	})
	if err != nil {
		// Fail soft: log and fall back to nop so the application still boots.
		// The error here is typically a misconfigured URL — turning the rest
		// of the system off because of it would be more disruptive than
		// running without the optional projection.
		logger.Error(context.Background(),
			"knowledge graph: failed to create Oxigraph store, falling back to nop",
			"url", cfg.Oxigraph.URL, "error", err)
		return NewNopStore()
	}
	logger.Info(context.Background(), "knowledge graph: Oxigraph store enabled",
		"url", cfg.Oxigraph.URL)
	return store
}
