package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

// --- fakes -------------------------------------------------------------

// fakeSettings counts reads so the cache scenarios can assert that a warm
// evaluation touches the store zero times, and that an anonymous caller never
// reaches the account layer at all.
type fakeSettings struct {
	mu            sync.Mutex
	instance      map[string]bool
	account       map[string]map[string]bool
	instanceReads int
	accountReads  int
	err           error
	// beforeReturn runs inside a read, so a test can interleave an
	// invalidation with a resolve that is already in flight.
	beforeReturn func()
}

func (f *fakeSettings) InstanceOverrides(context.Context) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instanceReads++
	hook := f.beforeReturn
	err := f.err
	inst := copyFeatureValues(f.instance)
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	if err != nil {
		return nil, err
	}
	return inst, nil
}

func (f *fakeSettings) AccountOverrides(_ context.Context, accountID string) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accountReads++
	if f.err != nil {
		return nil, f.err
	}
	return copyFeatureValues(f.account[accountID]), nil
}

func (f *fakeSettings) SetOverride(_ context.Context, scopeType, scopeID, key string, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if scopeType == repositories.FeatureScopeInstance {
		if f.instance == nil {
			f.instance = map[string]bool{}
		}
		f.instance[key] = enabled
		return nil
	}
	if f.account == nil {
		f.account = map[string]map[string]bool{}
	}
	if f.account[scopeID] == nil {
		f.account[scopeID] = map[string]bool{}
	}
	f.account[scopeID][key] = enabled
	return nil
}

func (f *fakeSettings) ClearOverride(_ context.Context, scopeType, scopeID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if scopeType == repositories.FeatureScopeInstance {
		delete(f.instance, key)
		return nil
	}
	delete(f.account[scopeID], key)
	return nil
}

func (f *fakeSettings) reads() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instanceReads, f.accountReads
}

type fakeGrants struct {
	mu    sync.Mutex
	rows  []entities.FeatureGrantRecord
	reads int
	err   error
}

func newFakeGrants() *fakeGrants {
	return &fakeGrants{}
}

func (f *fakeGrants) GrantsFor(
	_ context.Context, accountID, agentID, roleID string,
) ([]entities.FeatureGrantRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	var out []entities.FeatureGrantRecord
	for _, r := range f.rows {
		if r.AccountID != accountID {
			continue
		}
		if r.SubjectType == repositories.FeatureSubjectAgent && r.SubjectID == agentID {
			out = append(out, r)
			continue
		}
		if roleID != "" && r.SubjectType == repositories.FeatureSubjectRole && r.SubjectID == roleID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeGrants) ListByFeature(
	_ context.Context, accountID, key string,
) ([]entities.FeatureGrantRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []entities.FeatureGrantRecord
	for _, r := range f.rows {
		if r.AccountID == accountID && r.FeatureKey == key {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeGrants) Grant(_ context.Context, record entities.FeatureGrantRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.SubjectType == record.SubjectType && r.SubjectID == record.SubjectID &&
			r.AccountID == record.AccountID && r.FeatureKey == record.FeatureKey {
			f.rows[i] = record
			return nil
		}
	}
	f.rows = append(f.rows, record)
	return nil
}

func (f *fakeGrants) Revoke(
	_ context.Context, subjectType, subjectID, accountID, key string,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.SubjectType == subjectType && r.SubjectID == subjectID &&
			r.AccountID == accountID && r.FeatureKey == key {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// grantNow is the windowless grant the #481 tests make. It must fold to
// exactly the old behavior — held, with no boundary — which is what keeps
// those scenarios meaningful across this change.
func (f *fakeGrants) grantNow(subjectType, subjectID, accountID, key string) {
	_ = f.Grant(context.Background(), entities.FeatureGrantRecord{
		SubjectType: subjectType, SubjectID: subjectID,
		AccountID: accountID, FeatureKey: key,
	})
}

func (f *fakeGrants) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// fakeAccounts embeds the pericarp interface so only the one method feature
// resolution actually calls has to be implemented. A call to anything else
// panics, which is the point: it proves resolution touches nothing more.
type fakeAccounts struct {
	authrepos.AccountRepository
	roles map[string]string // "accountID|agentID" -> roleID
	// nonMembers names people deliberately NOT in an account, for the tests
	// that care about somebody who was removed.
	nonMembers map[string]bool
	err        error
}

// FindMemberRole answers "member" for anyone without an explicit mapping.
//
// That is the faithful default: a grant can only be made to a member of the
// account, so a test subject who holds a grant is a member by construction.
// Returning "" for them would model somebody who had been removed — a real
// case, but one the tests that want it should ask for deliberately.
func (f fakeAccounts) FindMemberRole(_ context.Context, accountID, agentID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if role, ok := f.roles[accountID+"|"+agentID]; ok {
		return role, nil
	}
	if f.nonMembers[accountID+"|"+agentID] {
		return "", nil
	}
	return "member", nil
}

// --- helpers -----------------------------------------------------------

func testResolver(t *testing.T, features []entities.FeatureMeta) (
	*FeatureResolver, *fakeSettings, *fakeGrants, *fakeAccounts,
) {
	t.Helper()
	registry, _ := newTestFeatureRegistry(t, features...)
	settings := &fakeSettings{}
	grants := newFakeGrants()
	accounts := &fakeAccounts{roles: map[string]string{}}
	cfg := config.Default()
	return NewFeatureResolver(cfg, registry, settings, grants, accounts, nil), settings, grants, accounts
}

func ctxAs(agentID, accountID string) context.Context {
	return auth.ContextWithAgent(context.Background(),
		&auth.Identity{AgentID: agentID, ActiveAccountID: accountID})
}

var (
	featEpisodic = entities.FeatureMeta{
		Key: "episodic-recall", DisplayName: "Episodic recall",
		Default: true, Manageable: true, Grantable: true,
	}
	featLedger = entities.FeatureMeta{
		Key: "ledger-export", DisplayName: "Ledger export",
		Default: false, Manageable: true, Grantable: true,
	}
	featAudit = entities.FeatureMeta{
		Key: "audit-trail", DisplayName: "Audit trail", Default: true,
	}
)

func mustResolve(t *testing.T, r *FeatureResolver, ctx context.Context) map[string]bool {
	t.Helper()
	set, err := r.ResolvedSet(ctx)
	if err != nil {
		t.Fatalf("ResolvedSet: %v", err)
	}
	return set
}

// --- tests -------------------------------------------------------------

func TestResolverDefaultsWhenNothingStored(t *testing.T) {
	r, _, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic, featLedger})
	set := mustResolve(t, r, ctxAs("agent-ops", "acct-harbor"))
	if !set["episodic-recall"] {
		t.Fatal("a declared-on feature resolved off with nothing stored")
	}
	if set["ledger-export"] {
		t.Fatal("a declared-off feature resolved on with nothing stored")
	}
}

func TestResolverLayerPrecedence(t *testing.T) {
	ctx := ctxAs("agent-ops", "acct-harbor")
	cases := []struct {
		name  string
		setup func(*fakeSettings, *fakeGrants, *fakeAccounts)
		key   string
		want  bool
	}{
		{
			"account turns on a declared-off feature",
			func(s *fakeSettings, _ *fakeGrants, _ *fakeAccounts) {
				_ = s.SetOverride(context.Background(), repositories.FeatureScopeAccount, "acct-harbor", "ledger-export", true)
			}, "ledger-export", true,
		},
		{
			"account off beats a grant",
			func(s *fakeSettings, g *fakeGrants, _ *fakeAccounts) {
				_ = s.SetOverride(context.Background(), repositories.FeatureScopeAccount, "acct-harbor", "episodic-recall", false)
				g.grantNow(repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "episodic-recall")
			}, "episodic-recall", false,
		},
		{
			"instance off beats an account on",
			func(s *fakeSettings, _ *fakeGrants, _ *fakeAccounts) {
				_ = s.SetOverride(context.Background(), repositories.FeatureScopeInstance, "", "episodic-recall", false)
				_ = s.SetOverride(context.Background(), repositories.FeatureScopeAccount, "acct-harbor", "episodic-recall", true)
			}, "episodic-recall", false,
		},
		{
			"instance off beats a grant",
			func(s *fakeSettings, g *fakeGrants, _ *fakeAccounts) {
				_ = s.SetOverride(context.Background(), repositories.FeatureScopeInstance, "", "ledger-export", false)
				g.grantNow(repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "ledger-export")
			}, "ledger-export", false,
		},
		{
			"a direct grant turns a feature on",
			func(_ *fakeSettings, g *fakeGrants, _ *fakeAccounts) {
				g.grantNow(repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "ledger-export")
			}, "ledger-export", true,
		},
		{
			"a role grant resolves like a direct one",
			func(_ *fakeSettings, g *fakeGrants, a *fakeAccounts) {
				a.roles["acct-harbor|agent-ops"] = "admin"
				g.grantNow(repositories.FeatureSubjectRole, "admin", "acct-harbor", "ledger-export")
			}, "ledger-export", true,
		},
		{
			"an account override on a non-manageable feature is ignored",
			func(s *fakeSettings, _ *fakeGrants, _ *fakeAccounts) {
				_ = s.SetOverride(context.Background(), repositories.FeatureScopeAccount, "acct-harbor", "audit-trail", false)
			}, "audit-trail", true,
		},
		{
			"a grant on a non-grantable feature is ignored",
			func(s *fakeSettings, g *fakeGrants, _ *fakeAccounts) {
				_ = s.SetOverride(context.Background(), repositories.FeatureScopeInstance, "", "audit-trail", false)
				g.grantNow(repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "audit-trail")
			}, "audit-trail", false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s, g, a := testResolver(t, []entities.FeatureMeta{featEpisodic, featLedger, featAudit})
			tc.setup(s, g, a)
			set := mustResolve(t, r, ctx)
			if set[tc.key] != tc.want {
				t.Fatalf("%s resolved %v, want %v", tc.key, set[tc.key], tc.want)
			}
		})
	}
}

// TestResolverAnonymousReadsInstanceLayerOnly proves the nil-identity rule is
// implemented, not merely intended: the account and grant stores are never
// touched, so there is no path by which a background worker or a stdio MCP
// session could pick up someone's account override.
func TestResolverAnonymousReadsInstanceLayerOnly(t *testing.T) {
	r, s, g, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
	_ = s.SetOverride(context.Background(), repositories.FeatureScopeAccount, "acct-harbor", "episodic-recall", false)
	_ = s.SetOverride(context.Background(), repositories.FeatureScopeInstance, "", "episodic-recall", true)

	set := mustResolve(t, r, context.Background())
	if !set["episodic-recall"] {
		t.Fatal("the anonymous caller did not get the instance-level value")
	}
	if _, accountReads := s.reads(); accountReads != 0 {
		t.Fatalf("the anonymous path read the account layer %d times, want 0", accountReads)
	}
	if g.readCount() != 0 {
		t.Fatalf("the anonymous path read grants %d times, want 0", g.readCount())
	}
}

func TestResolverFailsClosedAndNeverCachesTheFailure(t *testing.T) {
	r, s, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
	boom := errors.New("database is unreachable")
	s.err = boom

	if _, err := r.ResolvedSet(ctxAs("agent-ops", "acct-harbor")); err == nil {
		t.Fatal("ResolvedSet returned nil error while the store was unreadable")
	}

	// Recovery must be immediate. A remembered failure would keep a feature
	// off long after the store came back.
	s.err = nil
	set := mustResolve(t, r, ctxAs("agent-ops", "acct-harbor"))
	if !set["episodic-recall"] {
		t.Fatal("the feature stayed off after the store recovered; the failure was cached")
	}
}

func TestResolverCachesPerAgentAndAccount(t *testing.T) {
	r, s, g, _ := testResolver(t, []entities.FeatureMeta{featEpisodic, featLedger})
	g.grantNow(repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "ledger-export")

	ops := ctxAs("agent-ops", "acct-harbor")
	for i := 0; i < 30; i++ {
		set := mustResolve(t, r, ops)
		if !set["ledger-export"] {
			t.Fatalf("evaluation %d lost the grant", i)
		}
	}
	instanceReads, _ := s.reads()
	if instanceReads != 1 {
		t.Fatalf("30 evaluations in one session read the store %d times, want 1", instanceReads)
	}

	// A different person in the same account must not be served the first
	// person's set.
	counsel := mustResolve(t, r, ctxAs("agent-counsel", "acct-harbor"))
	if counsel["ledger-export"] {
		t.Fatal("one person's grant leaked into another person's resolved set")
	}

	// Same person, different active account: a different key again.
	instanceReads, _ = s.reads()
	_ = mustResolve(t, r, ctxAs("agent-ops", "acct-cedar"))
	newReads, _ := s.reads()
	if newReads == instanceReads {
		t.Fatal("switching account served the previous account's cached set")
	}
}

func TestResolverInvalidation(t *testing.T) {
	ctx := context.Background()

	t.Run("InvalidateAll drops everything", func(t *testing.T) {
		r, s, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
		_ = mustResolve(t, r, ctxAs("agent-ops", "acct-harbor"))
		_ = mustResolve(t, r, ctxAs("agent-broker", "acct-cedar"))
		before, _ := s.reads()

		r.InvalidateAll(ctx)
		_ = mustResolve(t, r, ctxAs("agent-ops", "acct-harbor"))
		_ = mustResolve(t, r, ctxAs("agent-broker", "acct-cedar"))
		after, _ := s.reads()
		if after != before+2 {
			t.Fatalf("after InvalidateAll the store was read %d more times, want 2", after-before)
		}
	})

	t.Run("InvalidateAccount spares other accounts", func(t *testing.T) {
		r, s, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
		_ = mustResolve(t, r, ctxAs("agent-ops", "acct-harbor"))
		_ = mustResolve(t, r, ctxAs("agent-broker", "acct-cedar"))
		before, _ := s.reads()

		r.InvalidateAccount(ctx, "acct-harbor")
		_ = mustResolve(t, r, ctxAs("agent-broker", "acct-cedar"))
		if after, _ := s.reads(); after != before {
			t.Fatal("invalidating one account re-resolved another account's session")
		}
		_ = mustResolve(t, r, ctxAs("agent-ops", "acct-harbor"))
		if after, _ := s.reads(); after != before+1 {
			t.Fatal("invalidating an account did not re-resolve its own session")
		}
	})

	t.Run("InvalidateAgents reaches named people only", func(t *testing.T) {
		r, s, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
		_ = mustResolve(t, r, ctxAs("agent-counsel", "acct-harbor"))
		_ = mustResolve(t, r, ctxAs("agent-clerk", "acct-harbor"))
		_ = mustResolve(t, r, ctxAs("agent-temp", "acct-harbor"))
		before, _ := s.reads()

		// The role fan-out case: two holders invalidated in one call.
		r.InvalidateAgents(ctx, "acct-harbor", "agent-counsel", "agent-clerk")

		_ = mustResolve(t, r, ctxAs("agent-temp", "acct-harbor"))
		if after, _ := s.reads(); after != before {
			t.Fatal("a member who does not hold the role was re-resolved")
		}
		_ = mustResolve(t, r, ctxAs("agent-counsel", "acct-harbor"))
		_ = mustResolve(t, r, ctxAs("agent-clerk", "acct-harbor"))
		if after, _ := s.reads(); after != before+2 {
			t.Fatal("both role holders should have been re-resolved")
		}
	})
}

// TestResolverMaxAgeBoundsStaleness is the backstop that makes the Postgres
// path safe: NOTIFY is not durable, so a replica that misses an invalidation
// must still converge. The clock is injected rather than slept on.
func TestResolverMaxAgeBoundsStaleness(t *testing.T) {
	registry, _ := newTestFeatureRegistry(t, featEpisodic)
	settings := &fakeSettings{}
	cfg := config.Default()
	cfg.Features.CacheMaxAge = 2 * time.Second
	r := NewFeatureResolver(cfg, registry, settings, newFakeGrants(),
		&fakeAccounts{roles: map[string]string{}}, nil)

	now := time.Now()
	r.now = func() time.Time { return now }

	ctx := ctxAs("agent-ops", "acct-harbor")
	if set := mustResolve(t, r, ctx); !set["episodic-recall"] {
		t.Fatal("first resolve did not return the declared default")
	}

	// The change lands in the store with no invalidation reaching this cache.
	_ = settings.SetOverride(context.Background(), repositories.FeatureScopeInstance, "", "episodic-recall", false)

	// Straight away: still the cached answer.
	if set := mustResolve(t, r, ctx); !set["episodic-recall"] {
		t.Fatal("the cached set expired before the maximum cache age")
	}

	// Past the age: re-resolved, without anything having invalidated it.
	now = now.Add(3 * time.Second)
	if set := mustResolve(t, r, ctx); set["episodic-recall"] {
		t.Fatal("the set was not re-resolved after the maximum cache age passed")
	}
}

// TestResolverMaxAgeDoesNotChangeAnAnswer pins the other half: expiry costs one
// extra read, it does not alter what the caller is told.
func TestResolverMaxAgeDoesNotChangeAnAnswer(t *testing.T) {
	registry, _ := newTestFeatureRegistry(t, featEpisodic)
	settings := &fakeSettings{}
	cfg := config.Default()
	cfg.Features.CacheMaxAge = 2 * time.Second
	r := NewFeatureResolver(cfg, registry, settings, newFakeGrants(),
		&fakeAccounts{roles: map[string]string{}}, nil)

	now := time.Now()
	r.now = func() time.Time { return now }
	ctx := ctxAs("agent-ops", "acct-harbor")

	_ = mustResolve(t, r, ctx)
	before, _ := settings.reads()

	now = now.Add(3 * time.Second)
	set := mustResolve(t, r, ctx)
	after, _ := settings.reads()

	if !set["episodic-recall"] {
		t.Fatal("expiry changed the answer when nothing had changed")
	}
	if after != before+1 {
		t.Fatalf("expiry cost %d extra reads, want exactly 1", after-before)
	}
}

// TestResolverCachedSetIsNotAliased guards against a caller mutating the map
// it was handed and corrupting every later reader of the same cache entry.
func TestResolverCachedSetIsNotAliased(t *testing.T) {
	r, _, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
	ctx := ctxAs("agent-ops", "acct-harbor")

	first := mustResolve(t, r, ctx)
	first["episodic-recall"] = false
	first["injected"] = true

	second := mustResolve(t, r, ctx)
	if !second["episodic-recall"] {
		t.Fatal("mutating a returned set corrupted the cache")
	}
	if _, ok := second["injected"]; ok {
		t.Fatal("mutating a returned set injected a key into the cache")
	}
}

// TestResolverDoesNotCacheAResultAnInvalidationOvertook is the race the
// premortem predicted: a resolve that read the store BEFORE a write commits,
// but finishes AFTER the invalidation runs, must not store its values. If it
// did, stale values would get a fresh timestamp and survive up to the maximum
// cache age — breaking the one rule this design has, and most likely to happen
// exactly when the instance is busy.
func TestResolverDoesNotCacheAResultAnInvalidationOvertook(t *testing.T) {
	registry, _ := newTestFeatureRegistry(t, featEpisodic)
	settings := &fakeSettings{}
	grants := newFakeGrants()
	r := NewFeatureResolver(config.Default(), registry, settings, grants,
		&fakeAccounts{roles: map[string]string{}}, nil)

	ctx := ctxAs("agent-ops", "acct-harbor")

	// Simulate the interleaving: the read has happened, then an invalidation
	// lands, then the result tries to store itself.
	settings.beforeReturn = func() { r.InvalidateAll(context.Background()) }
	if _, err := r.ResolvedSet(ctx); err != nil {
		t.Fatalf("ResolvedSet: %v", err)
	}
	settings.beforeReturn = nil

	before, _ := settings.reads()
	if _, err := r.ResolvedSet(ctx); err != nil {
		t.Fatalf("ResolvedSet: %v", err)
	}
	after, _ := settings.reads()
	if after == before {
		t.Fatal("the overtaken result was cached; a change would not reach this session " +
			"until the maximum cache age expired")
	}
}

// TestResolverServesLastKnownSetWhenTheStoreFails covers the outage the
// premortem predicted: a database blip must not strip every capability from
// everyone mid-turn, including features nobody ever turned off.
func TestResolverServesLastKnownSetWhenTheStoreFails(t *testing.T) {
	r, settings, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
	ctx := ctxAs("agent-ops", "acct-harbor")

	if set := mustResolve(t, r, ctx); !set["episodic-recall"] {
		t.Fatal("the first resolve did not return the declared default")
	}

	settings.err = errors.New("database is unreachable")
	r.InvalidateAll(context.Background()) // force a refresh attempt
	set, err := r.ResolvedSet(ctx)
	if err == nil && set["episodic-recall"] {
		// This is only reachable if a previous set survived the invalidation,
		// which it must not — InvalidateAll drops everything.
		t.Fatal("InvalidateAll left a set behind")
	}

	// With a set present and no invalidation, a failing refresh serves it.
	settings.err = nil
	_ = mustResolve(t, r, ctx)
	settings.err = errors.New("database is unreachable")
	r.expire(featureCacheKey{AgentID: "agent-ops", AccountID: "acct-harbor"})

	set, err = r.ResolvedSet(ctx)
	if err != nil {
		t.Fatalf("a failing refresh with a previous set returned an error: %v", err)
	}
	if !set["episodic-recall"] {
		t.Fatal("a database blip stripped a capability that was on moments before")
	}
}

// TestResolverThrottlesRetriesWhileTheStoreIsDown stops a sustained outage
// becoming a query storm: thirty evaluations in one agent turn must not mean
// thirty failing round trips.
func TestResolverThrottlesRetriesWhileTheStoreIsDown(t *testing.T) {
	r, settings, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
	ctx := ctxAs("agent-ops", "acct-harbor")
	_ = mustResolve(t, r, ctx)

	settings.err = errors.New("database is unreachable")
	r.expire(featureCacheKey{AgentID: "agent-ops", AccountID: "acct-harbor"})

	before, _ := settings.reads()
	for i := 0; i < 30; i++ {
		if _, err := r.ResolvedSet(ctx); err != nil {
			t.Fatalf("evaluation %d returned an error instead of the last known set: %v", i, err)
		}
	}
	after, _ := settings.reads()
	if after-before > 2 {
		t.Fatalf("a down store was retried %d times across 30 evaluations, want at most 2", after-before)
	}
}

// TestResolverRebuildsWhenACachedSetLacksADeclaredKey covers the preset-install
// case: answering meta.Default for a key missing from a cached set would bypass
// every stored layer, so a feature could answer ON past an explicit instance
// OFF for the life of the entry.
func TestResolverRebuildsWhenACachedSetLacksADeclaredKey(t *testing.T) {
	r, settings, _, _ := testResolver(t, []entities.FeatureMeta{featEpisodic})
	ctx := ctxAs("agent-ops", "acct-harbor")
	_ = mustResolve(t, r, ctx)

	// A feature declared after the set was cached, with the instance already
	// holding an explicit off for it — the shape a preset install produces on
	// an instance that was pre-seeded.
	late := entities.FeatureMeta{Key: "invoice-export", DisplayName: "Invoice export", Default: true}
	if err := r.registry.Register(late); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_ = settings.SetOverride(context.Background(), repositories.FeatureScopeInstance, "", "invoice-export", false)

	value, declared, err := r.Enabled(ctx, "invoice-export")
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if !declared {
		t.Fatal("the late declaration was reported undeclared")
	}
	if value {
		t.Fatal("a key missing from the cached set answered its declared default, " +
			"ignoring an explicit instance off")
	}
}

// holds reports whether a grant row exists, for tests that assert on storage
// rather than on resolution.
func (f *fakeGrants) holds(subjectType, subjectID, accountID, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.SubjectType == subjectType && r.SubjectID == subjectID &&
			r.AccountID == accountID && r.FeatureKey == key {
			return true
		}
	}
	return false
}

// --- window boundaries ------------------------------------------------

// grantWindow adds a grant with a window to the fake.
func (f *fakeGrants) grantWindow(agentID, accountID, key string, from, through *time.Time) {
	_ = f.Grant(context.Background(), entities.FeatureGrantRecord{
		SubjectType: repositories.FeatureSubjectAgent, SubjectID: agentID,
		AccountID: accountID, FeatureKey: key,
		ValidFrom: from, ValidThrough: through,
	})
}

func windowResolver(t *testing.T, maxAge time.Duration) (*FeatureResolver, *fakeGrants, *time.Time) {
	t.Helper()
	registry, _ := newTestFeatureRegistry(t, featLedger)
	grants := newFakeGrants()
	cfg := config.Default()
	cfg.Features.CacheMaxAge = maxAge
	r := NewFeatureResolver(cfg, registry, &fakeSettings{}, grants,
		&fakeAccounts{roles: map[string]string{}}, nil)
	now := time.Now()
	clock := &now
	r.now = func() time.Time { return *clock }
	return r, grants, clock
}

// TestGrantExpiresAtItsBoundaryNotAtTheCacheAge is the crux of #483. A grant
// with two seconds left must expire in two seconds even though the cache is
// good for ten minutes — and nothing invalidates, nothing restarts.
func TestGrantExpiresAtItsBoundaryNotAtTheCacheAge(t *testing.T) {
	r, grants, clock := windowResolver(t, 10*time.Minute)
	ctx := ctxAs("agent-ops", "acct-harbor")

	through := clock.Add(2 * time.Second)
	grants.grantWindow("agent-ops", "acct-harbor", "ledger-export", nil, &through)

	if set := mustResolve(t, r, ctx); !set["ledger-export"] {
		t.Fatal("a live grant did not resolve on")
	}
	// Straight away: still on, still cached.
	if set := mustResolve(t, r, ctx); !set["ledger-export"] {
		t.Fatal("the grant expired before its window closed")
	}

	*clock = clock.Add(3 * time.Second)
	if set := mustResolve(t, r, ctx); set["ledger-export"] {
		t.Fatal("the grant survived its own window — the cached set ignored the boundary")
	}
	// And the maximum cache age never came into it.
	if time.Until(*clock) > 10*time.Minute {
		t.Fatal("the test advanced past the maximum cache age; it proves nothing")
	}
}

// TestGrantStartsAtItsBoundaryWithNothingWritten: a grant dated forward begins
// on its own, in a session that was already open when it was made.
func TestGrantStartsAtItsBoundaryWithNothingWritten(t *testing.T) {
	r, grants, clock := windowResolver(t, 10*time.Minute)
	ctx := ctxAs("agent-ops", "acct-harbor")

	from := clock.Add(2 * time.Second)
	grants.grantWindow("agent-ops", "acct-harbor", "ledger-export", &from, nil)

	if set := mustResolve(t, r, ctx); set["ledger-export"] {
		t.Fatal("a grant that has not started was already held")
	}
	*clock = clock.Add(3 * time.Second)
	if set := mustResolve(t, r, ctx); !set["ledger-export"] {
		t.Fatal("the grant did not begin at its own boundary")
	}
}

// TestNoWindowMeansNoExtraResolves keeps the cache doing its job: a grant with
// no window must not make every evaluation re-read.
func TestNoWindowMeansNoExtraResolves(t *testing.T) {
	r, grants, clock := windowResolver(t, 10*time.Minute)
	ctx := ctxAs("agent-ops", "acct-harbor")
	grants.grantNow(repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "ledger-export")

	_ = mustResolve(t, r, ctx)
	before := grants.readCount()
	*clock = clock.Add(time.Minute)
	for i := 0; i < 20; i++ {
		_ = mustResolve(t, r, ctx)
	}
	if grants.readCount() != before {
		t.Fatalf("a windowless grant caused %d extra reads, want 0",
			grants.readCount()-before)
	}
}

// TestBoundaryDoesNotDefeatTheFailureBackoff: while the store is down, serving
// the last known set beats re-reading a database that cannot answer, even past
// a boundary.
func TestBoundaryDoesNotDefeatTheFailureBackoff(t *testing.T) {
	registry, _ := newTestFeatureRegistry(t, featLedger)
	settings := &fakeSettings{}
	grants := newFakeGrants()
	cfg := config.Default()
	cfg.Features.CacheMaxAge = 10 * time.Minute
	r := NewFeatureResolver(cfg, registry, settings, grants,
		&fakeAccounts{roles: map[string]string{}}, nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	ctx := ctxAs("agent-ops", "acct-harbor")

	through := now.Add(2 * time.Second)
	grants.grantWindow("agent-ops", "acct-harbor", "ledger-export", nil, &through)
	_ = mustResolve(t, r, ctx)

	settings.err = errors.New("database is unreachable")
	r.expire(featureCacheKey{AgentID: "agent-ops", AccountID: "acct-harbor"})
	if _, err := r.ResolvedSet(ctx); err != nil {
		t.Fatalf("a failing refresh did not serve the last known set: %v", err)
	}

	before, _ := settings.reads()
	for i := 0; i < 10; i++ {
		if _, err := r.ResolvedSet(ctx); err != nil {
			t.Fatalf("evaluation %d errored instead of serving the last known set: %v", i, err)
		}
	}
	if after, _ := settings.reads(); after-before > 1 {
		t.Fatalf("a down store was retried %d times past a boundary, want at most 1", after-before)
	}
}

// TestAGrantDoesNotOutliveMembership is the regression test for a hole the
// premortem found: membership is checked when a grant is MADE, and nothing
// removes the row when somebody leaves an account. Without a gate at
// resolution, a removed member with a live session keeps every feature granted
// to them — and the row silently comes back to life if they are re-added
// months later.
func TestAGrantDoesNotOutliveMembership(t *testing.T) {
	registry, _ := newTestFeatureRegistry(t, featLedger)
	grants := newFakeGrants()
	accounts := &fakeAccounts{
		roles:      map[string]string{},
		nonMembers: map[string]bool{"acct-harbor|agent-gone": true},
	}
	r := NewFeatureResolver(config.Default(), registry, &fakeSettings{}, grants, accounts, nil)

	grants.grantNow(repositories.FeatureSubjectAgent, "agent-gone", "acct-harbor", "ledger-export")
	grants.grantNow(repositories.FeatureSubjectAgent, "agent-still-here", "acct-harbor", "ledger-export")

	gone := mustResolve(t, r, ctxAs("agent-gone", "acct-harbor"))
	if gone["ledger-export"] {
		t.Fatal("somebody who is no longer a member kept a grant made while they were one")
	}
	still := mustResolve(t, r, ctxAs("agent-still-here", "acct-harbor"))
	if !still["ledger-export"] {
		t.Fatal("the membership gate took the grant from a member who still belongs")
	}
}
