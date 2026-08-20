package gorm

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

func newFeatureTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.FeatureSetting{}, &models.FeatureGrant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestFeatureSettingsOverridesAreTriState(t *testing.T) {
	ctx := context.Background()
	repo := ProvideFeatureSettingsRepository(newFeatureTestDB(t))

	// No row at all: the layer says nothing.
	got, err := repo.InstanceOverrides(ctx)
	if err != nil {
		t.Fatalf("InstanceOverrides: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("InstanceOverrides on an empty store = %v, want empty", got)
	}

	// An explicit off is a row, and is distinguishable from absence.
	if err := repo.SetOverride(ctx, repositories.FeatureScopeInstance, "", "ledger-export", false); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	got, err = repo.InstanceOverrides(ctx)
	if err != nil {
		t.Fatalf("InstanceOverrides: %v", err)
	}
	enabled, present := got["ledger-export"]
	if !present {
		t.Fatal("an explicit off did not come back as a present key")
	}
	if enabled {
		t.Fatal("an explicit off came back enabled")
	}

	// Setting again replaces rather than duplicating.
	if err := repo.SetOverride(ctx, repositories.FeatureScopeInstance, "", "ledger-export", true); err != nil {
		t.Fatalf("SetOverride (replace): %v", err)
	}
	got, _ = repo.InstanceOverrides(ctx)
	if !got["ledger-export"] {
		t.Fatal("replacing an override did not take effect")
	}
	assertSingleOverrideRow(t, repo)

	// Clearing returns the layer to silence — not to false.
	if err := repo.ClearOverride(ctx, repositories.FeatureScopeInstance, "", "ledger-export"); err != nil {
		t.Fatalf("ClearOverride: %v", err)
	}
	got, _ = repo.InstanceOverrides(ctx)
	if _, present := got["ledger-export"]; present {
		t.Fatal("a cleared override is still present; unset and off must not be confusable")
	}

	// Clearing what is not there satisfies the caller's intent already.
	if err := repo.ClearOverride(ctx, repositories.FeatureScopeInstance, "", "ledger-export"); err != nil {
		t.Fatalf("ClearOverride on an absent row = %v, want nil", err)
	}
}

// assertSingleOverrideRow proves the unique index collapsed a repeated
// SetOverride into one row rather than accumulating history — an override is
// current state, not an audit log.
func assertSingleOverrideRow(t *testing.T, repo repositories.FeatureSettingsRepository) {
	t.Helper()
	r, ok := repo.(*FeatureSettingsRepository)
	if !ok {
		t.Fatal("unexpected repository implementation")
	}
	var count int64
	if err := r.db.Model(&models.FeatureSetting{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("after replacing an override there are %d rows, want 1", count)
	}
}

// TestAccountOverridesDoNotMatchInstanceRows is the trap this schema invites:
// instance rows carry an empty ScopeID, so a careless account query would
// match them and silently promote an instance setting into every account.
func TestAccountOverridesDoNotMatchInstanceRows(t *testing.T) {
	ctx := context.Background()
	repo := ProvideFeatureSettingsRepository(newFeatureTestDB(t))

	if err := repo.SetOverride(ctx, repositories.FeatureScopeInstance, "", "episodic-recall", false); err != nil {
		t.Fatalf("SetOverride instance: %v", err)
	}
	if err := repo.SetOverride(ctx, repositories.FeatureScopeAccount, "acct-harbor", "ledger-export", true); err != nil {
		t.Fatalf("SetOverride account: %v", err)
	}

	acct, err := repo.AccountOverrides(ctx, "acct-harbor")
	if err != nil {
		t.Fatalf("AccountOverrides: %v", err)
	}
	if _, leaked := acct["episodic-recall"]; leaked {
		t.Fatal("an instance-scoped row leaked into an account's overrides")
	}
	if !acct["ledger-export"] {
		t.Fatal("the account's own override did not come back")
	}

	inst, err := repo.InstanceOverrides(ctx)
	if err != nil {
		t.Fatalf("InstanceOverrides: %v", err)
	}
	if _, leaked := inst["ledger-export"]; leaked {
		t.Fatal("an account-scoped row leaked into the instance overrides")
	}

	// An empty account ID must not fall back to reading instance rows.
	empty, err := repo.AccountOverrides(ctx, "")
	if err != nil {
		t.Fatalf("AccountOverrides(\"\"): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("AccountOverrides(\"\") = %v, want empty", empty)
	}
}

func TestAccountOverridesAreIsolatedPerAccount(t *testing.T) {
	ctx := context.Background()
	repo := ProvideFeatureSettingsRepository(newFeatureTestDB(t))

	if err := repo.SetOverride(ctx, repositories.FeatureScopeAccount, "acct-harbor", "episodic-recall", false); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	cedar, err := repo.AccountOverrides(ctx, "acct-cedar")
	if err != nil {
		t.Fatalf("AccountOverrides: %v", err)
	}
	if len(cedar) != 0 {
		t.Fatalf("another account's override was visible: %v", cedar)
	}
}

func TestFeatureGrantsUnionAgentAndRole(t *testing.T) {
	ctx := context.Background()
	repo := ProvideFeatureGrantRepository(newFeatureTestDB(t))

	if err := repo.Grant(ctx, entities.FeatureGrantRecord{SubjectType: repositories.FeatureSubjectAgent, SubjectID: "agent-ops", AccountID: "acct-harbor", FeatureKey: "ledger-export"}); err != nil {
		t.Fatalf("Grant agent: %v", err)
	}
	if err := repo.Grant(ctx, entities.FeatureGrantRecord{SubjectType: repositories.FeatureSubjectRole, SubjectID: "admin", AccountID: "acct-harbor", FeatureKey: "audit-trail"}); err != nil {
		t.Fatalf("Grant role: %v", err)
	}

	// Holding the role sees both legs.
	got, err := grantedKeys(t, repo, ctx, "acct-harbor", "agent-ops", "admin")
	if err != nil {
		t.Fatalf("GrantedKeys: %v", err)
	}
	if !got["ledger-export"] || !got["audit-trail"] {
		t.Fatalf("GrantedKeys = %v, want both the direct and the role grant", got)
	}

	// Without the role, only the direct grant.
	got, err = grantedKeys(t, repo, ctx, "acct-harbor", "agent-ops", "")
	if err != nil {
		t.Fatalf("GrantedKeys (no role): %v", err)
	}
	if !got["ledger-export"] {
		t.Fatal("the direct grant vanished when the caller held no role")
	}
	if got["audit-trail"] {
		t.Fatal("a role grant reached someone who does not hold the role")
	}

	// Another agent holding the same role gets the role grant only.
	got, err = grantedKeys(t, repo, ctx, "acct-harbor", "agent-counsel", "admin")
	if err != nil {
		t.Fatalf("GrantedKeys (other agent): %v", err)
	}
	if got["ledger-export"] {
		t.Fatal("a direct grant reached a different agent")
	}
	if !got["audit-trail"] {
		t.Fatal("the role grant did not reach another holder of the role")
	}
}

func TestFeatureGrantsAreAccountScoped(t *testing.T) {
	ctx := context.Background()
	repo := ProvideFeatureGrantRepository(newFeatureTestDB(t))

	if err := repo.Grant(ctx, entities.FeatureGrantRecord{SubjectType: repositories.FeatureSubjectAgent, SubjectID: "agent-ops", AccountID: "acct-harbor", FeatureKey: "ledger-export"}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	got, err := grantedKeys(t, repo, ctx, "acct-cedar", "agent-ops", "")
	if err != nil {
		t.Fatalf("GrantedKeys: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a grant in one account reached the same agent in another: %v", got)
	}

	// No account and no agent must never match rows on empty strings.
	for _, tc := range []struct{ accountID, agentID string }{{"", "agent-ops"}, {"acct-harbor", ""}, {"", ""}} {
		got, err := grantedKeys(t, repo, ctx, tc.accountID, tc.agentID, "")
		if err != nil {
			t.Fatalf("GrantedKeys(%q,%q): %v", tc.accountID, tc.agentID, err)
		}
		if len(got) != 0 {
			t.Fatalf("GrantedKeys(%q,%q) = %v, want empty", tc.accountID, tc.agentID, got)
		}
	}
}

func TestFeatureGrantIsIdempotentAndRevocable(t *testing.T) {
	ctx := context.Background()
	repo := ProvideFeatureGrantRepository(newFeatureTestDB(t))

	for i := 0; i < 3; i++ {
		if err := repo.Grant(ctx, entities.FeatureGrantRecord{SubjectType: repositories.FeatureSubjectAgent, SubjectID: "agent-ops", AccountID: "acct-harbor", FeatureKey: "ledger-export"}); err != nil {
			t.Fatalf("Grant %d: %v", i, err)
		}
	}
	got, _ := grantedKeys(t, repo, ctx, "acct-harbor", "agent-ops", "")
	if len(got) != 1 {
		t.Fatalf("granting three times produced %v, want one key", got)
	}

	if _, err := repo.Revoke(ctx, repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "ledger-export"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, _ = grantedKeys(t, repo, ctx, "acct-harbor", "agent-ops", "")
	if len(got) != 0 {
		t.Fatalf("after revoke, the caller still holds %v, want nothing", got)
	}
	// Revoking what nobody holds succeeds and reports that nothing was there,
	// which is what lets a caller record only real changes.
	removed, err := repo.Revoke(ctx, repositories.FeatureSubjectAgent, "agent-ops", "acct-harbor", "ledger-export")
	if err != nil {
		t.Fatalf("Revoke on an absent grant = %v, want nil", err)
	}
	if removed {
		t.Fatal("Revoke reported it removed a grant that was not there")
	}
}

func TestAccountMemberQueryListsByRole(t *testing.T) {
	ctx := context.Background()
	db := newFeatureTestDB(t)

	// account_members is pericarp's projection; core reads it without owning
	// the struct, so the test creates it the same way — by table name.
	if err := db.Exec(`CREATE TABLE account_members (
		account_id TEXT NOT NULL, agent_id TEXT NOT NULL, role_id TEXT NOT NULL,
		PRIMARY KEY (account_id, agent_id))`).Error; err != nil {
		t.Fatalf("create account_members: %v", err)
	}
	rows := []struct{ account, agent, role string }{
		{"acct-harbor", "agent-counsel", "admin"},
		{"acct-harbor", "agent-clerk", "admin"},
		{"acct-harbor", "agent-temp", "member"},
		{"acct-cedar", "agent-broker", "admin"},
	}
	for _, r := range rows {
		if err := db.Exec(
			`INSERT INTO account_members (account_id, agent_id, role_id) VALUES (?, ?, ?)`,
			r.account, r.agent, r.role).Error; err != nil {
			t.Fatalf("insert member: %v", err)
		}
	}

	q := ProvideAccountMemberQuery(db)
	got, err := q.ListMemberIDsByRole(ctx, "acct-harbor", "admin")
	if err != nil {
		t.Fatalf("ListMemberIDsByRole: %v", err)
	}
	sort.Strings(got)
	want := []string{"agent-clerk", "agent-counsel"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ListMemberIDsByRole = %v, want %v — a member missed here is a stale answer nobody can see", got, want)
	}

	// A role nobody holds invalidates nothing, and is not an error.
	got, err = q.ListMemberIDsByRole(ctx, "acct-harbor", "owner")
	if err != nil {
		t.Fatalf("ListMemberIDsByRole (unheld role): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an unheld role returned %v, want empty", got)
	}

	for _, tc := range []struct{ account, role string }{{"", "admin"}, {"acct-harbor", ""}} {
		got, err := q.ListMemberIDsByRole(ctx, tc.account, tc.role)
		if err != nil {
			t.Fatalf("ListMemberIDsByRole(%q,%q): %v", tc.account, tc.role, err)
		}
		if len(got) != 0 {
			t.Fatalf("ListMemberIDsByRole(%q,%q) = %v, want empty", tc.account, tc.role, got)
		}
	}
}

// grantedKeys folds a caller's rows the way the resolver does, so these tests
// keep asserting on what a caller holds rather than on row shape.
func grantedKeys(
	t *testing.T, repo repositories.FeatureGrantRepository,
	ctx context.Context, accountID, agentID, roleID string,
) (map[string]bool, error) {
	t.Helper()
	rows, err := repo.GrantsFor(ctx, accountID, agentID, roleID)
	if err != nil {
		return nil, err
	}
	held, _ := entities.FoldGrants(rows, time.Now())
	return held, nil
}
