package e2e

import (
	"testing"

	"go.uber.org/fx"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

// TestFeatureGraphResolves proves the feature-flag providers wire into the real
// fx container. A compile is not evidence: a missing or duplicated provider
// only shows up when the graph is built.
func TestFeatureGraphResolves(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseDSN = t.TempDir() + "/fxgraph.db"

	var (
		registry    *application.FeatureRegistry
		resolver    *application.FeatureResolver
		provider    *application.FeatureProvider
		service     *application.FeatureService
		invalidator repositories.FeatureCacheInvalidator
	)
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&registry, &resolver, &provider, &service, &invalidator),
	)
	if err := app.Err(); err != nil {
		t.Fatalf("fx graph failed to build: %v", err)
	}
	if registry == nil || resolver == nil || provider == nil || service == nil || invalidator == nil {
		t.Fatal("a feature dependency resolved to nil")
	}
	// On SQLite the resolver IS the invalidator — not a wrapper.
	if invalidator != repositories.FeatureCacheInvalidator(resolver) {
		t.Fatal("on SQLite the invalidator should be the resolver itself, with no broadcast wrapper")
	}
}
