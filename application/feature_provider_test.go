package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"

	"github.com/wepala/weos/v3/domain/entities"
)

// countingLogger records Warn lines so the log-once rule can be asserted
// rather than assumed.
type countingLogger struct {
	mu    sync.Mutex
	warns []string
}

func (l *countingLogger) Debug(context.Context, string, ...interface{}) {}
func (l *countingLogger) Info(context.Context, string, ...interface{})  {}
func (l *countingLogger) Error(context.Context, string, ...interface{}) {}
func (l *countingLogger) Warn(_ context.Context, msg string, fields ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	parts := []string{msg}
	for _, f := range fields {
		if s, ok := f.(string); ok {
			parts = append(parts, s)
		}
	}
	l.warns = append(l.warns, strings.Join(parts, " "))
}

func (l *countingLogger) matching(substr string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, w := range l.warns {
		if strings.Contains(w, substr) {
			out = append(out, w)
		}
	}
	return out
}

func testProvider(t *testing.T, features ...entities.FeatureMeta) (
	*FeatureProvider, *fakeSettings, *fakeGrants, *countingLogger,
) {
	t.Helper()
	r, settings, grants, _ := testResolver(t, features)
	logger := &countingLogger{}
	r.logger = logger
	return NewFeatureProvider(r, logger), settings, grants, logger
}

func TestProviderIgnoresKeysOutsideItsNamespace(t *testing.T) {
	p, settings, _, _ := testProvider(t, featEpisodic)
	ctx := ctxAs("agent-ops", "acct-harbor")

	// Apollo's namespace, evaluated against this provider: it must pass
	// through untouched so the two can be composed.
	got := p.BooleanEvaluation(ctx, "entitlement.sea_math_2027", true, nil)
	if !got.Value {
		t.Fatalf("a foreign-namespace key resolved %v, want the supplied default true", got.Value)
	}
	if got.Reason != openfeature.DefaultReason {
		t.Fatalf("reason = %q, want %q", got.Reason, openfeature.DefaultReason)
	}
	if instanceReads, _ := settings.reads(); instanceReads != 0 {
		t.Fatal("a foreign-namespace key reached the store")
	}

	// A bare prefix names no feature.
	if got := p.BooleanEvaluation(ctx, FeatureFlagPrefix, true, nil); !got.Value {
		t.Fatal("a bare prefix did not fall through to the caller's default")
	}
}

func TestProviderUndeclaredKeyReturnsCallerDefaultAndLogsOnce(t *testing.T) {
	p, _, _, logger := testProvider(t, featEpisodic)
	ctx := ctxAs("agent-ops", "acct-harbor")

	for i := 0; i < 20; i++ {
		got := p.BooleanEvaluation(ctx, FeatureFlagPrefix+"shipping-labels", false, nil)
		if got.Value {
			t.Fatalf("evaluation %d of an undeclared key returned true, want the supplied default false", i)
		}
		if got.Reason != openfeature.DefaultReason {
			t.Fatalf("reason = %q, want %q", got.Reason, openfeature.DefaultReason)
		}
	}
	// And the caller's default is honored whichever way it points.
	if got := p.BooleanEvaluation(ctx, FeatureFlagPrefix+"shipping-labels", true, nil); !got.Value {
		t.Fatal("an undeclared key did not honor a true default")
	}

	logs := logger.matching("shipping-labels")
	if len(logs) != 1 {
		t.Fatalf("an undeclared key logged %d times, want exactly 1 — a hot path must not drown the signal", len(logs))
	}
}

// TestProviderFailsClosedNotToTheCallersDefault is the distinction that
// matters most in this file: on a store failure the answer is OFF, not
// whatever the call site happened to pass as its default.
func TestProviderFailsClosedNotToTheCallersDefault(t *testing.T) {
	p, settings, _, logger := testProvider(t, featEpisodic)
	settings.err = errors.New("database is unreachable")

	got := p.BooleanEvaluation(ctxAs("agent-ops", "acct-harbor"), FeatureFlagPrefix+"episodic-recall", true, nil)
	if got.Value {
		t.Fatal("a store failure resolved on; it must fail closed even when the caller's default is true")
	}
	if got.Reason != openfeature.ErrorReason {
		t.Fatalf("reason = %q, want %q", got.Reason, openfeature.ErrorReason)
	}
	if len(logger.matching("feature evaluation failed")) == 0 {
		t.Fatal("a store failure was not logged")
	}
}

func TestProviderResolvesDeclaredFeatures(t *testing.T) {
	p, _, grants, _ := testProvider(t, featEpisodic, featLedger)
	ctx := ctxAs("agent-ops", "acct-harbor")

	got := p.BooleanEvaluation(ctx, FeatureFlagPrefix+"episodic-recall", false, nil)
	if !got.Value {
		t.Fatal("a declared-on feature resolved off")
	}
	if got.Reason != openfeature.TargetingMatchReason {
		t.Fatalf("reason = %q, want %q", got.Reason, openfeature.TargetingMatchReason)
	}

	// The caller's default must not override stored state in either direction.
	if got := p.BooleanEvaluation(ctx, FeatureFlagPrefix+"ledger-export", true, nil); got.Value {
		t.Fatal("a declared-off feature resolved on because the caller's default was true")
	}

	grants.grantNow("agent", "agent-ops", "acct-harbor", "ledger-export")
	p.resolver.InvalidateAll(context.Background())
	if got := p.BooleanEvaluation(ctx, FeatureFlagPrefix+"ledger-export", false, nil); !got.Value {
		t.Fatal("a grant did not reach the provider after invalidation")
	}
}

// TestProviderNonBooleanEvaluationsReturnTheCallersDefault keeps the seam open
// for a later flag source that does carry richer values, without surprising any
// call site today.
func TestProviderNonBooleanEvaluationsReturnTheCallersDefault(t *testing.T) {
	p, _, _, _ := testProvider(t, featEpisodic)
	ctx := ctxAs("agent-ops", "acct-harbor")
	flag := FeatureFlagPrefix + "episodic-recall"

	if got := p.StringEvaluation(ctx, flag, "trial", nil); got.Value != "trial" || got.Reason != openfeature.DefaultReason {
		t.Fatalf("StringEvaluation = %+v, want the caller's default with reason DEFAULT", got)
	}
	if got := p.IntEvaluation(ctx, flag, 7, nil); got.Value != 7 || got.Reason != openfeature.DefaultReason {
		t.Fatalf("IntEvaluation = %+v, want the caller's default with reason DEFAULT", got)
	}
	got := p.FloatEvaluation(ctx, flag, 1.5, nil)
	if got.Value != 1.5 || got.Reason != openfeature.DefaultReason {
		t.Fatalf("FloatEvaluation = %+v, want the caller's default with reason DEFAULT", got)
	}
	obj := map[string]string{"tier": "pro"}
	if got := p.ObjectEvaluation(ctx, flag, obj, nil); got.Reason != openfeature.DefaultReason {
		t.Fatalf("ObjectEvaluation reason = %q, want %q", got.Reason, openfeature.DefaultReason)
	}
}

func TestProviderMetadataIsStable(t *testing.T) {
	p, _, _, _ := testProvider(t, featEpisodic)
	if got := p.Metadata().Name; got != featureProviderName {
		t.Fatalf("provider name = %q, want %q — it is an operations contract", got, featureProviderName)
	}
	if p.Hooks() != nil {
		t.Fatal("the provider installs global hooks; callers should add their own at the client level")
	}
}

// TestProviderAnswersPerCallerNotPerLastAsker guards the failure mode a single
// shared cache entry would produce: two people in one account being served
// whichever set was resolved most recently.
func TestProviderAnswersPerCallerNotPerLastAsker(t *testing.T) {
	p, _, grants, _ := testProvider(t, featLedger)
	grants.grantNow("agent", "agent-ops", "acct-harbor", "ledger-export")

	ops := ctxAs("agent-ops", "acct-harbor")
	counsel := ctxAs("agent-counsel", "acct-harbor")
	flag := FeatureFlagPrefix + "ledger-export"

	if got := p.BooleanEvaluation(ops, flag, false, nil); !got.Value {
		t.Fatal("the grant holder was answered off")
	}
	if got := p.BooleanEvaluation(counsel, flag, false, nil); got.Value {
		t.Fatal("someone without the grant was answered on")
	}
	if got := p.BooleanEvaluation(ops, flag, false, nil); !got.Value {
		t.Fatal("the grant holder lost their answer after someone else asked")
	}
}
