// Package graph wires the optional knowledge-graph projection store. When the
// Oxigraph endpoint is configured the provider returns the HTTP-backed store;
// otherwise it returns a nop store so call sites never have to nil-check.
package graph

import (
	"context"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
	"github.com/wepala/weos/v3/internal/config"
)

// pingTimeout caps how long the startup reachability check waits for the
// Oxigraph endpoint. Short enough that a misconfigured URL doesn't slow
// boot noticeably; long enough to ride out a slow first connection on a
// freshly-started endpoint.
const pingTimeout = 3 * time.Second

// ProvideKnowledgeGraphStore returns a KnowledgeGraphStore for the application
// container. If Oxigraph is not configured (or explicitly disabled), a nop
// store is returned and downstream code becomes a no-op. If configured but
// unreachable at startup, we log a single ERROR and fall back to nop —
// preferable to letting every event log per-write failures forever.
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
		logger.Error(context.Background(),
			"knowledge graph: failed to create Oxigraph store, falling back to nop",
			"url", cfg.Oxigraph.URL, "error", err)
		return NewNopStore()
	}
	if err := pingStore(store); err != nil {
		logger.Error(context.Background(),
			"knowledge graph: Oxigraph endpoint unreachable, falling back to nop "+
				"(unset OXIGRAPH_URL or fix the endpoint to silence)",
			"url", cfg.Oxigraph.URL, "error", err)
		return NewNopStore()
	}
	logger.Info(context.Background(), "knowledge graph: Oxigraph store enabled",
		"url", cfg.Oxigraph.URL)
	return store
}

// pingStore issues a cheap ASK against the configured endpoint to confirm
// reachability and that the SPARQL protocol is being spoken. We use IsEmpty
// because it goes through the same query path the projector uses, so a
// configuration that works here is one that works in production.
func pingStore(store repositories.KnowledgeGraphStore) error {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	_, err := store.IsEmpty(ctx)
	return err
}
