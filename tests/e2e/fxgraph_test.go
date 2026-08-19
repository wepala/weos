package e2e

import (
	"context"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
	"go.uber.org/fx"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
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
		service     *application.FeatureService
		invalidator repositories.FeatureCacheInvalidator
	)
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&registry, &resolver, &service, &invalidator),
	)
	if err := app.Err(); err != nil {
		t.Fatalf("fx graph failed to build: %v", err)
	}
	if registry == nil || resolver == nil || service == nil || invalidator == nil {
		t.Fatal("a feature dependency resolved to nil")
	}
	// On SQLite the resolver IS the invalidator — not a wrapper.
	if invalidator != repositories.FeatureCacheInvalidator(resolver) {
		t.Fatal("on SQLite the invalidator should be the resolver itself, with no broadcast wrapper")
	}
}

// TestFeatureProviderIsRegisteredWithoutBeingAskedFor is the regression test
// for the subsystem shipping inert.
//
// fx builds only what something depends on. The provider was originally offered
// with fx.Provide and demanded by nothing, so in the real server it was never
// constructed, the OpenFeature domain fell through to the SDK's no-op provider,
// and every evaluation returned the caller's default. Every test passed, because
// the tests populated the provider directly and thereby forced the construction
// that production never performed.
//
// So this test deliberately does NOT populate the provider. It starts the app
// the way `weos serve` does, then evaluates through a client obtained the way a
// call site would, and requires the answer to come from our resolver rather
// than from a default.
func TestFeatureProviderIsRegisteredWithoutBeingAskedFor(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseDSN = t.TempDir() + "/fxregister.db"

	var registry *application.FeatureRegistry
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&registry),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	if err := registry.Register(entities.FeatureMeta{
		Key: "wiring-probe", DisplayName: "Wiring probe", Default: true,
	}); err != nil {
		t.Fatalf("could not declare the probe feature: %v", err)
	}

	client := openfeature.NewClient(application.FeatureProviderDomain)
	detail, err := client.BooleanValueDetails(context.Background(),
		application.FeatureFlagPrefix+"wiring-probe", false, openfeature.EvaluationContext{})
	if err != nil {
		t.Fatalf("evaluation failed: %v", err)
	}

	// A no-op provider would return the supplied default (false) with reason
	// DEFAULT. Our resolver returns the declared value with TARGETING_MATCH.
	if !detail.Value {
		t.Fatal("the declared feature resolved off — the domain is bound to a no-op provider, " +
			"so the whole feature-flag subsystem is inert in the shipped binary")
	}
	if detail.Reason != openfeature.TargetingMatchReason {
		t.Fatalf("reason = %q, want %q — the answer did not come from the WeOS resolver",
			detail.Reason, openfeature.TargetingMatchReason)
	}
}
