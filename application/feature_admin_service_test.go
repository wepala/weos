package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
)

// adminAccounts is the slice of pericarp's AccountRepository the permission
// check actually uses. Everything else panics, which keeps the test honest
// about how much the guard touches.
type adminAccounts struct {
	authrepos.AccountRepository
	// roles is keyed "accountID|agentID".
	roles    map[string]string
	accounts []string
	err      error
}

func (a adminAccounts) FindByID(_ context.Context, id string) (*authentities.Account, error) {
	if a.err != nil {
		return nil, a.err
	}
	for _, known := range a.accounts {
		if known == id {
			acct := new(authentities.Account)
			if err := acct.Restore(id, id+" Account",
				authentities.AccountTypePersonal, true, time.Unix(0, 0)); err != nil {
				return nil, err
			}
			return acct, nil
		}
	}
	return nil, nil
}

func (a adminAccounts) FindMemberRole(_ context.Context, accountID, agentID string) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	return a.roles[accountID+"|"+agentID], nil
}

func (a adminAccounts) FindAll(
	_ context.Context, _ string, limit int,
) (*authrepos.PaginatedResponse[*authentities.Account], error) {
	if a.err != nil {
		return nil, a.err
	}
	out := make([]*authentities.Account, 0, len(a.accounts))
	for i, id := range a.accounts {
		if limit > 0 && i >= limit {
			break
		}
		acct := new(authentities.Account)
		if err := acct.Restore(id, id+" Account", authentities.AccountTypePersonal, true, time.Unix(0, 0)); err != nil {
			return nil, err
		}
		out = append(out, acct)
	}
	return &authrepos.PaginatedResponse[*authentities.Account]{Data: out}, nil
}

func testAdminService(t *testing.T, cfg config.Config, accounts adminAccounts) *FeatureAdminService {
	t.Helper()
	registry, _ := newTestFeatureRegistry(t, featEpisodic, featLedger, featAudit)
	settings := &fakeSettings{}
	grants := newFakeGrants()
	inv := newRecordingInvalidator()
	svc := NewFeatureService(registry, settings, grants, fakeMembers{}, inv, nil)
	resolver := NewFeatureResolver(cfg, registry, settings, grants,
		&fakeAccounts{roles: map[string]string{}}, nil)
	// No event store: the audit write fails and is swallowed by design, which
	// is exactly what these tests want to leave out of the way.
	return NewFeatureAdminService(cfg, svc, resolver, accounts, nil, nil, nil, nil)
}

func ctxAsAgent(agentID, accountID string) context.Context {
	return auth.ContextWithAgent(context.Background(),
		&auth.Identity{AgentID: agentID, ActiveAccountID: accountID})
}

// TestInstanceChangeRequiresTheInstanceAccount is the regression test for a
// privilege escalation.
//
// Registration mints every new user as OWNER of their own personal account, so
// a guard that accepts "owner or admin of the account you are acting in" means
// "anyone who signed up" on a multi-user instance. The role has to be held in
// the instance's own account.
func TestInstanceChangeRequiresTheInstanceAccount(t *testing.T) {
	cfg := config.Default()
	cfg.Features.PrimaryAccountID = "acct-instance"
	accounts := adminAccounts{
		accounts: []string{"acct-instance", "acct-newcomer"},
		roles: map[string]string{
			// The newcomer owns their OWN account, exactly as registration
			// leaves them.
			"acct-newcomer|agent-newcomer": authentities.RoleOwner,
			"acct-instance|agent-ops":      authentities.RoleOwner,
		},
	}
	svc := testAdminService(t, cfg, accounts)

	err := svc.SetInstance(ctxAsAgent("agent-newcomer", "acct-newcomer"),
		"episodic-recall", false, entities.FeatureChangeSourceAPI)
	if err == nil {
		t.Fatal("a self-registered owner of their own account changed instance-wide state")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("the refusal was %v, want ErrForbidden", err)
	}

	// The instance account's owner is still allowed.
	if err := svc.SetInstance(ctxAsAgent("agent-ops", "acct-instance"),
		"episodic-recall", false, entities.FeatureChangeSourceAPI); err != nil {
		t.Fatalf("the instance account's owner was refused: %v", err)
	}
}

// TestSingleAccountInstanceNeedsNoConfiguration keeps the mini-me shape
// working: one account IS the instance, so nothing has to be configured.
func TestSingleAccountInstanceNeedsNoConfiguration(t *testing.T) {
	accounts := adminAccounts{
		accounts: []string{"acct-only"},
		roles:    map[string]string{"acct-only|agent-ops": authentities.RoleOwner},
	}
	svc := testAdminService(t, config.Default(), accounts)

	if err := svc.SetInstance(ctxAsAgent("agent-ops", "acct-only"),
		"episodic-recall", false, entities.FeatureChangeSourceAPI); err != nil {
		t.Fatalf("the sole account's owner was refused: %v", err)
	}
}

// TestManyAccountsWithoutConfigurationRefuses fails closed rather than
// guessing. Picking one would hand the switch to whoever registered first.
func TestManyAccountsWithoutConfigurationRefuses(t *testing.T) {
	accounts := adminAccounts{
		accounts: []string{"acct-a", "acct-b"},
		roles:    map[string]string{"acct-a|agent-ops": authentities.RoleOwner},
	}
	svc := testAdminService(t, config.Default(), accounts)

	err := svc.SetInstance(ctxAsAgent("agent-ops", "acct-a"),
		"episodic-recall", false, entities.FeatureChangeSourceAPI)
	if err == nil {
		t.Fatal("an unconfigured multi-account instance guessed which account owns the switch")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("the refusal was %v, want ErrForbidden", err)
	}
}

// TestLocalTransportSkipsThePermissionCheck: the command line and `weos mcp`
// over stdio are trusted, because whoever can run them against the database
// can already change it directly.
func TestLocalTransportSkipsThePermissionCheck(t *testing.T) {
	accounts := adminAccounts{accounts: []string{"acct-a", "acct-b"}}
	svc := testAdminService(t, config.Default(), accounts)

	// Unconfigured and multi-account, which refuses over HTTP — but local.
	ctx := WithLocalTransport(context.Background())
	if err := svc.SetInstance(ctx, "episodic-recall", false, entities.FeatureChangeSourceCLI); err != nil {
		t.Fatalf("a local caller was refused: %v", err)
	}
}

// TestAccountScopeIsUnaffectedByTheInstanceAccount: an account admin changes
// their OWN account, which has nothing to do with who owns the instance.
func TestAccountScopeIsUnaffectedByTheInstanceAccount(t *testing.T) {
	cfg := config.Default()
	cfg.Features.PrimaryAccountID = "acct-instance"
	accounts := adminAccounts{
		accounts: []string{"acct-instance", "acct-other"},
		roles:    map[string]string{"acct-other|agent-counsel": authentities.RoleAdmin},
	}
	svc := testAdminService(t, cfg, accounts)

	if err := svc.SetAccount(ctxAsAgent("agent-counsel", "acct-other"),
		"ledger-export", true, entities.FeatureChangeSourceAPI); err != nil {
		t.Fatalf("an admin of their own account was refused an account override: %v", err)
	}
	// And they still cannot reach the instance.
	if err := svc.SetInstance(ctxAsAgent("agent-counsel", "acct-other"),
		"ledger-export", true, entities.FeatureChangeSourceAPI); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an admin of another account reached the instance switch: %v", err)
	}
}

// TestAnonymousCallerIsRefused covers a session that names no account.
func TestAnonymousCallerIsRefused(t *testing.T) {
	svc := testAdminService(t, config.Default(), adminAccounts{accounts: []string{"acct-only"}})
	if err := svc.SetInstance(context.Background(), "episodic-recall", false,
		entities.FeatureChangeSourceAPI); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an unidentified caller was not refused: %v", err)
	}
	if err := svc.SetInstance(ctxAsAgent("agent-ops", ""), "episodic-recall", false,
		entities.FeatureChangeSourceAPI); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a session naming no account was not refused: %v", err)
	}
}

// TestAccountScopeRefusesAnOrdinaryMember covers the guard on the account
// scope, which the acceptance contract does not reach — no scenario has an
// ordinary member attempt an account-scope change.
func TestAccountScopeRefusesAnOrdinaryMember(t *testing.T) {
	accounts := adminAccounts{
		accounts: []string{"acct-only"},
		roles:    map[string]string{"acct-only|agent-clerk": "member"},
	}
	svc := testAdminService(t, config.Default(), accounts)

	err := svc.SetAccount(ctxAsAgent("agent-clerk", "acct-only"),
		"ledger-export", true, entities.FeatureChangeSourceAPI)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("an ordinary member set an account override: %v", err)
	}
	err = svc.ResetAccount(ctxAsAgent("agent-clerk", "acct-only"),
		"ledger-export", entities.FeatureChangeSourceAPI)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("an ordinary member reset an account override: %v", err)
	}
}

// TestResetAccountClearsTheOverride covers the one method with no coverage on
// any surface — three entry points reach it and no scenario exercises any.
func TestResetAccountClearsTheOverride(t *testing.T) {
	accounts := adminAccounts{
		accounts: []string{"acct-only"},
		roles:    map[string]string{"acct-only|agent-ops": authentities.RoleOwner},
	}
	registry, _ := newTestFeatureRegistry(t, featEpisodic, featLedger)
	settings := &fakeSettings{}
	svc := NewFeatureService(registry, settings, newFakeGrants(), fakeMembers{},
		newRecordingInvalidator(), nil)
	resolver := NewFeatureResolver(config.Default(), registry, settings, newFakeGrants(),
		&fakeAccounts{roles: map[string]string{}}, nil)
	admin := NewFeatureAdminService(config.Default(), svc, resolver, accounts, nil, nil, nil, nil)

	ctx := ctxAsAgent("agent-ops", "acct-only")
	if err := admin.SetAccount(ctx, "ledger-export", true, entities.FeatureChangeSourceAPI); err != nil {
		t.Fatalf("SetAccount: %v", err)
	}
	if !settings.account["acct-only"]["ledger-export"] {
		t.Fatal("the override was not stored")
	}
	if err := admin.ResetAccount(ctx, "ledger-export", entities.FeatureChangeSourceAPI); err != nil {
		t.Fatalf("ResetAccount: %v", err)
	}
	if _, present := settings.account["acct-only"]["ledger-export"]; present {
		t.Fatal("reset left the override behind; unset and off must not be confusable")
	}
}

// TestConfiguredPrimaryAccountMustExist: a typo would otherwise tell every
// operator they lack a role, blaming the caller for a mistake in an
// environment variable.
func TestConfiguredPrimaryAccountMustExist(t *testing.T) {
	cfg := config.Default()
	cfg.Features.PrimaryAccountID = "acct-typo"
	accounts := adminAccounts{
		accounts: []string{"acct-real"},
		roles:    map[string]string{"acct-real|agent-ops": authentities.RoleOwner},
	}
	svc := testAdminService(t, cfg, accounts)

	err := svc.SetInstance(ctxAsAgent("agent-ops", "acct-real"),
		"episodic-recall", false, entities.FeatureChangeSourceAPI)
	if err == nil {
		t.Fatal("a configured account that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("the error blames the caller rather than the configuration: %v", err)
	}
}
