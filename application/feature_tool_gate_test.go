package application

import (
	"context"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
)

// erroringProvider answers every boolean evaluation with a ResolutionError,
// which is what the OpenFeature client turns into a non-nil error from
// BooleanValueDetails. It stands in for the provider states the client
// short-circuits on — NOT_READY and FATAL — which are unreachable while
// registration uses SetNamedProviderAndWait but are the same code path in the
// gate.
type erroringProvider struct {
	openfeature.NoopProvider
}

func (erroringProvider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: "erroring-test-provider"}
}

func (erroringProvider) BooleanEvaluation(
	_ context.Context, _ string, defaultValue bool, _ openfeature.FlattenedContext,
) openfeature.BoolResolutionDetail {
	return openfeature.BoolResolutionDetail{
		Value: defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			ResolutionError: openfeature.NewGeneralResolutionError("the flag source is unavailable"),
			Reason:          openfeature.ErrorReason,
		},
	}
}

// TestToolFeatureGateClosesWhenTheClientCannotAnswer is the regression test for
// the one real fail-open in this design.
//
// The gate passes `true` as the client default, because an undeclared key must
// leave a capability exactly where it was. That default is also what the
// OpenFeature client returns when it cannot reach the provider at all — and
// Client.Boolean discards the error that says so, which would open every gate
// at exactly the moment the flag source is unavailable. The gate reads the
// error instead.
func TestToolFeatureGateClosesWhenTheClientCannotAnswer(t *testing.T) {
	const domain = "weos-tool-gate-test"
	if err := openfeature.SetNamedProviderAndWait(domain, erroringProvider{}); err != nil {
		t.Fatalf("could not register the test provider: %v", err)
	}
	gate := ToolFeatureGate(openfeature.NewClient(domain))
	if gate == nil {
		t.Fatal("ToolFeatureGate returned nil for a real client")
	}

	// The naive form — the one this fix replaced — returns the caller's
	// default here, so this assertion is exactly the difference.
	if openfeature.NewClient(domain).Boolean(
		context.Background(), FeatureFlagPrefix+"ledger-export", true, openfeature.EvaluationContext{},
	) != true {
		t.Fatal("the test provider no longer reproduces the client's default-on-error behavior")
	}

	if gate(context.Background(), "ledger-export") {
		t.Fatal("the gate opened while the client could not answer; a capability is handed out during an outage")
	}
}

// TestToolFeatureGateIsNilWithoutAClient pins the signal the transports check:
// no client means no gate, and a transport that would have gated tools must
// refuse to start rather than serve them open.
func TestToolFeatureGateIsNilWithoutAClient(t *testing.T) {
	if ToolFeatureGate(nil) != nil {
		t.Fatal("a nil client produced a gate, which would look wired while gating nothing")
	}
}

// TestHasCallerIdentity pins the predicate the refusal wording branches on. It
// is the same condition resolution uses to decide whether to read the account
// layer and the caller's grants, so the two cannot drift.
func TestHasCallerIdentity(t *testing.T) {
	if HasCallerIdentity(context.Background()) {
		t.Fatal("a context with nobody on it reported a caller")
	}
}
