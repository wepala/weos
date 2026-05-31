package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wepala/weos/v3/internal/config"
)

type provLogger struct{}

func (provLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (provLogger) Info(_ context.Context, _ string, _ ...interface{})  {}
func (provLogger) Warn(_ context.Context, _ string, _ ...interface{})  {}
func (provLogger) Error(_ context.Context, _ string, _ ...interface{}) {}

func TestProvideKnowledgeGraphStore_NopWhenInactive(t *testing.T) {
	t.Parallel()
	cfg := config.Config{} // Oxigraph zero-value: not active
	store := ProvideKnowledgeGraphStore(cfg, provLogger{})
	if store.Active() {
		t.Error("inactive config should produce a nop store")
	}
}

func TestProvideKnowledgeGraphStore_ActiveWhenURLSetAndReachable(t *testing.T) {
	t.Parallel()
	// httptest stands in for a live Oxigraph; our ping issues an ASK and
	// expects a SPARQL JSON response. Reply with the smallest valid one.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write([]byte(`{"head":{},"boolean":false}`))
	}))
	defer srv.Close()

	cfg := config.Config{Oxigraph: config.OxigraphConfig{URL: srv.URL, Enabled: true}}
	store := ProvideKnowledgeGraphStore(cfg, provLogger{})
	if !store.Active() {
		t.Error("configured + reachable Oxigraph should produce an active store")
	}
}

func TestProvideKnowledgeGraphStore_NopWhenEndpointUnreachable(t *testing.T) {
	t.Parallel()
	// Bind a server, then close it — the URL is well-formed but the
	// listener refuses connections, exercising the ping-fail path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	cfg := config.Config{Oxigraph: config.OxigraphConfig{URL: deadURL, Enabled: true}}
	store := ProvideKnowledgeGraphStore(cfg, provLogger{})
	if store.Active() {
		t.Error("unreachable endpoint should fall back to nop instead of returning an active store")
	}
}
