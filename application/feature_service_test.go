package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
)

// recordingInvalidator records what was invalidated, so each write can be
// checked for calling the right one — the coupling this whole story depends on.
type recordingInvalidator struct {
	all      int
	accounts []string
	agents   map[string][]string
}

func newRecordingInvalidator() *recordingInvalidator {
	return &recordingInvalidator{agents: map[string][]string{}}
}

func (r *recordingInvalidator) InvalidateAll(context.Context) { r.all++ }

func (r *recordingInvalidator) InvalidateAccount(_ context.Context, accountID string) {
	r.accounts = append(r.accounts, accountID)
}

func (r *recordingInvalidator) InvalidateAgents(_ context.Context, accountID string, agentIDs ...string) {
	r.agents[accountID] = append(r.agents[accountID], agentIDs...)
}

func (r *recordingInvalidator) agentsFor(accountID string) []string {
	out := append([]string(nil), r.agents[accountID]...)
	sort.Strings(out)
	return out
}

type fakeMembers struct {
	byRole map[string][]string // "accountID|roleID" -> agent IDs
	err    error
}

func (f fakeMembers) ListMemberIDsByRole(_ context.Context, accountID, roleID string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byRole[accountID+"|"+roleID], nil
}

func testService(t *testing.T, members fakeMembers, features ...entities.FeatureMeta) (
	*FeatureService, *fakeSettings, *fakeGrants, *recordingInvalidator,
) {
	t.Helper()
	registry, _ := newTestFeatureRegistry(t, features...)
	settings := &fakeSettings{}
	grants := newFakeGrants()
	inv := newRecordingInvalidator()
	return NewFeatureService(registry, settings, grants, members, inv, nil), settings, grants, inv
}

func TestServiceInstanceWritesInvalidateEverything(t *testing.T) {
	ctx := context.Background()
	svc, settings, _, inv := testService(t, fakeMembers{}, featEpisodic)

	if err := svc.SetInstanceFeature(ctx, "episodic-recall", false); err != nil {
		t.Fatalf("SetInstanceFeature: %v", err)
	}
	if inv.all != 1 {
		t.Fatalf("InvalidateAll called %d times, want 1", inv.all)
	}
	if enabled, ok := settings.instance["episodic-recall"]; !ok || enabled {
		t.Fatal("the instance override was not stored as an explicit off")
	}

	// Clearing returns the layer to silence, and is not the same as off.
	if err := svc.ClearInstanceFeature(ctx, "episodic-recall"); err != nil {
		t.Fatalf("ClearInstanceFeature: %v", err)
	}
	if _, present := settings.instance["episodic-recall"]; present {
		t.Fatal("clearing left a row behind; unset and off must not be confusable")
	}
	if inv.all != 2 {
		t.Fatalf("InvalidateAll called %d times after a clear, want 2", inv.all)
	}
}

func TestServiceAccountWritesInvalidateOnlyThatAccount(t *testing.T) {
	ctx := context.Background()
	svc, _, _, inv := testService(t, fakeMembers{}, featEpisodic)

	if err := svc.SetAccountFeature(ctx, "acct-harbor", "episodic-recall", false); err != nil {
		t.Fatalf("SetAccountFeature: %v", err)
	}
	if inv.all != 0 {
		t.Fatal("an account change invalidated every account")
	}
	if len(inv.accounts) != 1 || inv.accounts[0] != "acct-harbor" {
		t.Fatalf("invalidated accounts = %v, want [acct-harbor]", inv.accounts)
	}
}

func TestServiceRefusesIneligibleWrites(t *testing.T) {
	ctx := context.Background()
	svc, _, _, inv := testService(t, fakeMembers{}, featEpisodic, featAudit)

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"undeclared key, instance", func() error {
			return svc.SetInstanceFeature(ctx, "shipping-labels", true)
		}, "no feature named"},
		{"undeclared key, account", func() error {
			return svc.SetAccountFeature(ctx, "acct-harbor", "shipping-labels", true)
		}, "no feature named"},
		{"non-manageable per account", func() error {
			return svc.SetAccountFeature(ctx, "acct-harbor", "audit-trail", false)
		}, "cannot be changed per account"},
		{"non-grantable to an agent", func() error {
			return svc.GrantToAgent(ctx, "acct-harbor", "agent-ops", "audit-trail", GrantTerms{})
		}, "cannot be granted"},
		{"non-grantable to a role", func() error {
			return svc.GrantToRole(ctx, "acct-harbor", "admin", "audit-trail", GrantTerms{})
		}, "cannot be granted"},
		{"account override with no account", func() error {
			return svc.SetAccountFeature(ctx, "", "episodic-recall", true)
		}, "account is required"},
		{"grant with no subject", func() error {
			return svc.GrantToAgent(ctx, "acct-harbor", "", "episodic-recall", GrantTerms{})
		}, "account and a subject are required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("the write was accepted, want a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}

	// A refused write must not have invalidated anything.
	if inv.all != 0 || len(inv.accounts) != 0 || len(inv.agents) != 0 {
		t.Fatal("a refused write still invalidated caches")
	}
}

func TestServiceAgentGrantInvalidatesThatPersonOnly(t *testing.T) {
	ctx := context.Background()
	svc, _, grants, inv := testService(t, fakeMembers{}, featLedger)

	if err := svc.GrantToAgent(ctx, "acct-harbor", "agent-ops", "ledger-export", GrantTerms{}); err != nil {
		t.Fatalf("GrantToAgent: %v", err)
	}
	if got := inv.agentsFor("acct-harbor"); len(got) != 1 || got[0] != "agent-ops" {
		t.Fatalf("invalidated agents = %v, want [agent-ops]", got)
	}
	if !grants.holds("agent", "agent-ops", "acct-harbor", "ledger-export") {
		t.Fatal("the grant was not stored")
	}

	if _, err := svc.RevokeFromAgent(ctx, "acct-harbor", "agent-ops", "ledger-export"); err != nil {
		t.Fatalf("RevokeFromAgent: %v", err)
	}
	if grants.holds("agent", "agent-ops", "acct-harbor", "ledger-export") {
		t.Fatal("the grant survived the revoke")
	}
	if got := inv.agentsFor("acct-harbor"); len(got) != 2 {
		t.Fatalf("a revoke did not invalidate; agents = %v", got)
	}
}

// TestServiceRoleGrantFansOutToEveryHolder is the expensive case Akeem chose
// precision on. One row changes; every holder's cache entry must be dropped,
// and nobody else's.
func TestServiceRoleGrantFansOutToEveryHolder(t *testing.T) {
	ctx := context.Background()
	members := fakeMembers{byRole: map[string][]string{
		"acct-harbor|admin": {"agent-counsel", "agent-clerk"},
	}}
	svc, _, _, inv := testService(t, members, featLedger)

	if err := svc.GrantToRole(ctx, "acct-harbor", "admin", "ledger-export", GrantTerms{}); err != nil {
		t.Fatalf("GrantToRole: %v", err)
	}
	got := inv.agentsFor("acct-harbor")
	if len(got) != 2 || got[0] != "agent-clerk" || got[1] != "agent-counsel" {
		t.Fatalf("invalidated agents = %v, want both role holders", got)
	}
	if len(inv.accounts) != 0 || inv.all != 0 {
		t.Fatal("a role grant invalidated more broadly than the holders")
	}
}

// TestServiceRoleGrantFallsBackWhenTheMemberListFails covers the case that
// would otherwise strand stale answers: the grant landed, so leaving caches
// alone is not an option, and over-invalidating only costs a re-resolve.
func TestServiceRoleGrantFallsBackWhenTheMemberListFails(t *testing.T) {
	ctx := context.Background()
	members := fakeMembers{err: errors.New("account_members is unreadable")}
	svc, _, grants, inv := testService(t, members, featLedger)

	if err := svc.GrantToRole(ctx, "acct-harbor", "admin", "ledger-export", GrantTerms{}); err != nil {
		t.Fatalf("GrantToRole returned %v; the write landed, so it must not fail", err)
	}
	if !grants.holds("role", "admin", "acct-harbor", "ledger-export") {
		t.Fatal("the grant was not stored")
	}
	if len(inv.accounts) != 1 || inv.accounts[0] != "acct-harbor" {
		t.Fatalf("fell back to %v, want a whole-account invalidation", inv.accounts)
	}
}

func TestServiceListReturnsDeclarationsSorted(t *testing.T) {
	svc, _, _, _ := testService(t, fakeMembers{}, featLedger, featEpisodic, featAudit)
	got := svc.List()
	if len(got) != 3 {
		t.Fatalf("List returned %d features, want 3", len(got))
	}
	if got[0].Key != "audit-trail" || got[1].Key != "episodic-recall" || got[2].Key != "ledger-export" {
		t.Fatalf("List is not sorted by key: %v, %v, %v", got[0].Key, got[1].Key, got[2].Key)
	}
}
