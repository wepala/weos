package gorm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	authmodels "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/models"
	esinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
	"github.com/wepala/weos/v3/internal/oauth"
)

// purgeFixture is a store holding two accounts that share a person, laid out
// the way the running application lays it out: pericarp's auth tables, the
// event log, the core projections and the connector tables.
type purgeFixture struct {
	t  *testing.T
	db *gorm.DB
	pm repositories.ProjectionManager
}

func newPurgeFixture(t *testing.T) *purgeFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&esinfra.GormEventModel{}, &subscriptions.GormParkedEventModel{},
		&authmodels.AgentModel{}, &authmodels.CredentialModel{}, &authmodels.PasswordCredentialModel{},
		&authmodels.AuthSessionModel{}, &authmodels.AccountModel{}, &authmodels.AccountMemberModel{},
		&authmodels.InviteModel{},
		&models.ResourceType{}, &models.Resource{}, &models.Triple{}, &models.EventReference{},
		&models.ResourcePermission{}, &models.BehaviorSettings{}, &models.FeatureSetting{},
		&models.FeatureGrant{}, &models.AccountErasure{},
		&oauth.OAuthClient{}, &oauth.OAuthAuthorizationCode{}, &oauth.OAuthRefreshToken{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The casbin gorm adapter's table, by name: the purge reads it by name too.
	if err := db.Exec("CREATE TABLE casbin_rule (id INTEGER PRIMARY KEY, ptype TEXT, v0 TEXT, v1 TEXT, v2 TEXT, v3 TEXT, v4 TEXT, v5 TEXT)").Error; err != nil {
		t.Fatalf("create casbin_rule: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	if err := pm.EnsureTable(context.Background(), "recipe", schema, nil); err != nil {
		t.Fatalf("ensure recipes table: %v", err)
	}
	if err := db.Create(&models.ResourceType{ID: "urn:type:recipe", Name: "Recipe", Slug: "recipe", Status: "active"}).Error; err != nil {
		t.Fatalf("create resource type: %v", err)
	}
	return &purgeFixture{t: t, db: db, pm: pm}
}

func (f *purgeFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *purgeFixture) account(id string) {
	f.must(f.db.Create(&authmodels.AccountModel{ID: id, Name: id, AccountType: "personal", Active: true}).Error)
}

// person creates an agent with a password credential and a session in each
// of the accounts named.
func (f *purgeFixture) person(agentID string, accounts ...string) {
	f.must(f.db.Create(&authmodels.AgentModel{ID: agentID, Name: agentID, AgentType: "foaf:Person", Status: "active"}).Error)
	credID := "cred-" + agentID
	f.must(f.db.Create(&authmodels.CredentialModel{ID: credID, AgentID: agentID, Provider: "password",
		ProviderUserID: agentID + "@example", Email: agentID + "@example", Active: true}).Error)
	f.must(f.db.Create(&authmodels.PasswordCredentialModel{ID: "pw-" + agentID, CredentialID: credID,
		Algorithm: "bcrypt", Hash: "x"}).Error)
	for _, account := range accounts {
		f.must(f.db.Create(&authmodels.AccountMemberModel{AccountID: account, AgentID: agentID, RoleID: "owner"}).Error)
		f.must(f.db.Create(&authmodels.AuthSessionModel{ID: "sess-" + agentID + "-" + account, AgentID: agentID,
			AccountID: account, CredentialID: credID, Active: true, ExpiresAt: time.Now().Add(time.Hour)}).Error)
		f.must(f.db.Exec("INSERT INTO casbin_rule (ptype, v0, v1, v2) VALUES ('g', ?, 'owner', ?)", agentID, account).Error)
	}
}

func (f *purgeFixture) event(id, aggregate, txID, account string) {
	payload := esinfra.JSONB{"Timestamp": time.Now().Format(time.RFC3339)}
	if account != "" {
		payload["AccountID"] = account
	}
	var position int64
	f.must(f.db.Model(&esinfra.GormEventModel{}).Select("COALESCE(MAX(position), 0) + 1").Scan(&position).Error)
	f.must(f.db.Create(&esinfra.GormEventModel{ID: id, AggregateID: aggregate, EventType: "Resource.Published",
		SequenceNo: int(position), TransactionID: txID, Position: position, Payload: payload}).Error)
}

func (f *purgeFixture) recipe(urn, account, agent string) {
	f.must(f.db.Create(&models.Resource{ID: urn, TypeSlug: "recipe", Data: `{"name":"x"}`, Status: "active",
		CreatedBy: agent, AccountID: account}).Error)
	f.must(f.db.Exec("INSERT INTO recipes (id, type_slug, status, account_id, created_by, name) VALUES (?, 'recipe', 'active', ?, ?, 'x')",
		urn, account, agent).Error)
	f.must(f.db.Create(&models.Triple{Subject: urn, Predicate: "schema:name", Object: "x"}).Error)
	f.must(f.db.Create(&models.EventReference{EventID: "ev-ref-" + urn, ResourceURN: urn}).Error)
	f.must(f.db.Create(&models.ResourcePermission{ID: "perm-" + urn, ResourceID: urn, AgentID: "someone", Actions: `["read"]`, GrantedBy: agent}).Error)
}

func (f *purgeFixture) count(table, where string, args ...any) int64 {
	f.t.Helper()
	var n int64
	f.must(f.db.Table(table).Where(where, args...).Count(&n).Error)
	return n
}

func TestAccountPurger_TakesOnlyEventsOfAggregatesBeingDeleted(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	f.account("acct-cedar")
	f.person("ops", "acct-harbor")                   // belongs to nothing else
	f.person("counsel", "acct-harbor", "acct-cedar") // shared: survives
	f.recipe("urn:recipe:h1", "acct-harbor", "ops")
	f.recipe("urn:recipe:c1", "acct-cedar", "counsel")

	// One transaction carries an event of Harbor Legal's recipe and an event of
	// Cedar Realty's: the surviving aggregate's event must stay.
	f.event("ev-h1", "urn:recipe:h1", "tx-shared", "acct-harbor")
	f.event("ev-c1", "urn:recipe:c1", "tx-shared", "acct-cedar")
	// The account's own aggregate, and counsel's agent aggregate committed in
	// the same transaction as the membership (the invite shape).
	f.event("ev-acct", "acct-harbor", "tx-member", "")
	f.event("ev-counsel", "counsel", "tx-member", "")
	// ops's own auth aggregates go with them.
	f.event("ev-ops", "ops", "tx-ops", "")
	f.event("ev-ops-cred", "cred-ops", "tx-ops", "")
	// A resource whose row is already gone still names the account in its
	// payload, so it is enumerated by the payload rather than the row.
	f.event("ev-gone", "urn:recipe:gone", "tx-gone", "acct-harbor")
	f.must(f.db.Create(&subscriptions.GormParkedEventModel{Subscriber: "oxigraph", EventID: "ev-h1", EventType: "Resource.Published", Position: 1}).Error)
	f.must(f.db.Create(&subscriptions.GormParkedEventModel{Subscriber: "oxigraph", EventID: "ev-c1", EventType: "Resource.Published", Position: 2}).Error)

	purger := NewAccountPurgerForTest(f.db, f.pm, &testLogger{})
	report, err := purger.Purge(context.Background(), "acct-harbor")
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if report.Members != 2 {
		t.Errorf("report.Members = %d, want 2", report.Members)
	}
	if len(report.DeletedAgents) != 1 || report.DeletedAgents[0] != "ops" {
		t.Errorf("report.DeletedAgents = %v, want [ops]", report.DeletedAgents)
	}
	for _, gone := range []string{"ev-h1", "ev-acct", "ev-ops", "ev-ops-cred", "ev-gone"} {
		if f.count("events", "id = ?", gone) != 0 {
			t.Errorf("event %s of a deleted aggregate survived", gone)
		}
	}
	for _, kept := range []string{"ev-c1", "ev-counsel"} {
		if f.count("events", "id = ?", kept) != 1 {
			t.Errorf("event %s of a surviving aggregate was deleted through the shared transaction", kept)
		}
	}
	if f.count("parked_events", "event_id = ?", "ev-h1") != 0 {
		t.Error("the parked copy of a deleted event survived")
	}
	if f.count("parked_events", "event_id = ?", "ev-c1") != 1 {
		t.Error("the parked copy of a surviving event was deleted")
	}
	if f.count("agents", "id = ?", "counsel") != 1 || f.count("credentials", "agent_id = ?", "counsel") != 1 {
		t.Error("the shared person lost their agent or credential")
	}
	if f.count("account_members", "agent_id = ? AND account_id = ?", "counsel", "acct-cedar") != 1 {
		t.Error("the shared person lost their other membership")
	}
	if f.count("auth_sessions", "id = ?", "sess-counsel-acct-cedar") != 1 {
		t.Error("the shared person's session in the other account was deleted")
	}
	if f.count("agents", "id = ?", "ops") != 0 || f.count("credentials", "agent_id = ?", "ops") != 0 ||
		f.count("password_credentials", "credential_id = ?", "cred-ops") != 0 {
		t.Error("the member who belonged to nothing else kept an agent, credential or password")
	}
	if f.count("auth_sessions", "agent_id = ?", "ops") != 0 {
		t.Error("the deleted member's session survived")
	}
	if f.count("accounts", "id = ?", "acct-harbor") != 0 || f.count("account_members", "account_id = ?", "acct-harbor") != 0 {
		t.Error("the account row or its memberships survived")
	}
	if f.count("casbin_rule", "v2 = ?", "acct-harbor") != 0 || f.count("casbin_rule", "v2 = ?", "acct-cedar") != 1 {
		t.Error("grouping policies were not confined to the deleted account's domain")
	}
	for _, table := range []string{"resources", "recipes"} {
		if f.count(table, "account_id = ?", "acct-harbor") != 0 || f.count(table, "account_id = ?", "acct-cedar") != 1 {
			t.Errorf("%s rows were not confined to the deleted account", table)
		}
	}
	if f.count("triples", "subject = ?", "urn:recipe:h1") != 0 || f.count("triples", "subject = ?", "urn:recipe:c1") != 1 {
		t.Error("triples were not confined to the deleted account's subjects")
	}
	if f.count("event_references", "resource_urn = ?", "urn:recipe:h1") != 0 || f.count("resource_permissions", "resource_id = ?", "urn:recipe:h1") != 0 {
		t.Error("references or permissions keyed by the deleted account's resources survived")
	}
}

func TestAccountPurger_ConnectorRegistrationSurvivesWhileAnotherAccountNamesIt(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	f.account("acct-cedar")
	f.person("ops", "acct-harbor")
	f.person("counsel", "acct-cedar")
	for _, client := range []string{"claude", "only-harbor"} {
		f.must(f.db.Create(&oauth.OAuthClient{ClientID: client, ClientName: client, RedirectURIs: "[]", GrantTypes: "[]", ResponseTypes: "[]"}).Error)
	}
	f.must(f.db.Create(&oauth.OAuthAuthorizationCode{Code: "code-h", ClientID: "claude", AgentID: "ops", AccountID: "acct-harbor",
		RedirectURI: "x", CodeChallenge: "x", Status: "exchanged", ExpiresAt: time.Now()}).Error)
	f.must(f.db.Create(&oauth.OAuthRefreshToken{ID: "rt-h", TokenHash: "h1", AgentID: "ops", AccountID: "acct-harbor", ClientID: "claude", FamilyID: "rt-h"}).Error)
	f.must(f.db.Create(&oauth.OAuthRefreshToken{ID: "rt-h2", TokenHash: "h2", AgentID: "ops", AccountID: "acct-harbor", ClientID: "only-harbor", FamilyID: "rt-h2"}).Error)
	f.must(f.db.Create(&oauth.OAuthRefreshToken{ID: "rt-c", TokenHash: "c1", AgentID: "counsel", AccountID: "acct-cedar", ClientID: "claude", FamilyID: "rt-c"}).Error)

	purger := NewAccountPurgerForTest(f.db, f.pm, &testLogger{})
	if _, err := purger.Purge(context.Background(), "acct-harbor"); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if f.count("oauth_authorization_codes", "account_id = ?", "acct-harbor") != 0 ||
		f.count("oauth_refresh_tokens", "account_id = ?", "acct-harbor") != 0 {
		t.Error("the deleted account's connector access survived")
	}
	if f.count("oauth_refresh_tokens", "id = ?", "rt-c") != 1 {
		t.Error("the other account's refresh token was deleted")
	}
	if f.count("oauth_clients", "client_id = ?", "claude") != 1 {
		t.Error("a registration another account's token still names was deleted")
	}
	if f.count("oauth_clients", "client_id = ?", "only-harbor") != 0 {
		t.Error("a registration nothing names any more survived")
	}
}

func TestAccountPurger_DeletesInChunksUnderTheBoundParameterCap(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	f.person("ops", "acct-harbor")
	const many = purgeChunk*2 + 37
	for i := range many {
		urn := fmt.Sprintf("urn:recipe:%05d", i)
		f.recipe(urn, "acct-harbor", "ops")
		f.event("ev-"+urn, urn, "tx-"+urn, "acct-harbor")
	}

	purger := NewAccountPurgerForTest(f.db, f.pm, &testLogger{})
	report, err := purger.Purge(context.Background(), "acct-harbor")
	if err != nil {
		t.Fatalf("Purge over %d resources: %v", many, err)
	}
	if report.Resources != many || report.Events != many {
		t.Errorf("report = %d resources, %d events; want %d of each", report.Resources, report.Events, many)
	}
	for _, table := range []string{"resources", "recipes", "events", "triples", "event_references", "resource_permissions"} {
		var n int64
		f.must(f.db.Table(table).Count(&n).Error)
		if n != 0 {
			t.Errorf("%s still holds %d rows", table, n)
		}
	}
}

func TestAccountPurger_SettingsAndLockGoWithTheAccount(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	f.person("ops", "acct-harbor")
	f.must(f.db.Create(&models.BehaviorSettings{AccountID: "acct-harbor", TypeSlug: "pantry", EnabledBehaviors: "[]"}).Error)
	f.must(f.db.Create(&models.FeatureGrant{SubjectType: "agent", SubjectID: "ops", AccountID: "acct-harbor", FeatureKey: "export"}).Error)
	f.must(f.db.Create(&models.FeatureSetting{ScopeType: "account", ScopeID: "acct-harbor", FeatureKey: "export", Enabled: true}).Error)
	f.must(f.db.Create(&models.FeatureSetting{ScopeType: "instance", ScopeID: "", FeatureKey: "export", Enabled: true}).Error)
	f.must(f.db.Create(&authmodels.InviteModel{ID: "inv-1", AccountID: "acct-harbor", Email: "new@example", RoleID: "member",
		InviterAgentID: "ops", InviteeAgentID: "skeleton", Status: "pending"}).Error)

	locks := ProvideAccountErasureLocks(f.db)
	ctx := context.Background()
	if err := locks.Lock(ctx, "acct-harbor", "ops"); err != nil {
		t.Fatal(err)
	}
	if err := locks.Lock(ctx, "acct-harbor", "ops-again"); err != nil {
		t.Fatalf("a second lock of the same account must not fail: %v", err)
	}
	if locked, err := locks.IsLocked(ctx, "acct-harbor"); err != nil || !locked {
		t.Fatalf("IsLocked = %v, %v; want true", locked, err)
	}

	purger := NewAccountPurgerForTest(f.db, f.pm, &testLogger{})
	if _, err := purger.Purge(ctx, "acct-harbor"); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	for table, where := range map[string]string{
		"behavior_settings": "account_id = 'acct-harbor'",
		"feature_grants":    "account_id = 'acct-harbor'",
		"feature_settings":  "scope_id = 'acct-harbor'",
		"invites":           "account_id = 'acct-harbor'",
		"account_erasures":  "account_id = 'acct-harbor'",
	} {
		if f.count(table, where) != 0 {
			t.Errorf("%s still holds rows for the account", table)
		}
	}
	if f.count("feature_settings", "scope_type = 'instance'") != 1 {
		t.Error("the instance-level setting was deleted with the account")
	}
	if locked, err := locks.IsLocked(ctx, "acct-harbor"); err != nil || locked {
		t.Fatalf("IsLocked after the purge = %v, %v; want false", locked, err)
	}
}

func TestAccountPurger_EnumerateReadsWithoutDeleting(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	f.person("ops", "acct-harbor")
	f.person("counsel", "acct-harbor")
	f.recipe("urn:recipe:h1", "acct-harbor", "ops")

	purger := NewAccountPurgerForTest(f.db, f.pm, &testLogger{})
	got, err := purger.Enumerate(context.Background(), "acct-harbor")
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}
	if got.Members != 2 || len(got.ResourceURNs) != 1 || got.ResourceURNs[0] != "urn:recipe:h1" {
		t.Errorf("Enumerate = %+v, want 2 members and the one recipe", got)
	}
	if f.count("resources", "account_id = ?", "acct-harbor") != 1 {
		t.Error("Enumerate deleted something")
	}
}

// wm-4cysr: two deletions that arrive together both reach the purge; the
// second finds the account row gone and nothing left, and says so.
func TestAccountPurger_ASecondPurgeOfAGoneAccountIsNothingToPurge(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	f.person("ops", "acct-harbor")
	f.recipe("urn:recipe:1", "acct-harbor", "ops")
	f.event("ev-1", "urn:recipe:1", "tx-1", "acct-harbor")
	purger := NewAccountPurgerForTest(f.db, f.pm, &testLogger{})

	if _, err := purger.Purge(context.Background(), "acct-harbor"); err != nil {
		t.Fatalf("first Purge: %v", err)
	}
	left, err := purger.Remains(context.Background(), "acct-harbor")
	if err != nil || left {
		t.Fatalf("Remains after the purge = %v, %v; want false", left, err)
	}
	_, err = purger.Purge(context.Background(), "acct-harbor")
	if !errors.Is(err, repositories.ErrNothingToPurge) {
		t.Fatalf("second Purge error = %v, want ErrNothingToPurge", err)
	}
}

// wm-mnry2: rows that name an account whose row is gone are orphans; the
// purge takes them rather than refusing.
func TestAccountPurger_SweepsTheOrphansOfAGoneAccount(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	f.person("ops", "acct-harbor")
	purger := NewAccountPurgerForTest(f.db, f.pm, &testLogger{})
	if _, err := purger.Purge(context.Background(), "acct-harbor"); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	// A request admitted before the lock lands its resource and event now.
	f.recipe("urn:recipe:late", "acct-harbor", "ops")
	f.event("ev-late", "urn:recipe:late", "tx-late", "acct-harbor")

	left, err := purger.Remains(context.Background(), "acct-harbor")
	if err != nil || !left {
		t.Fatalf("Remains with a late resource = %v, %v; want true", left, err)
	}
	report, err := purger.Purge(context.Background(), "acct-harbor")
	if err != nil {
		t.Fatalf("Purge of the orphans: %v", err)
	}
	if report.Resources != 1 || report.Events != 1 {
		t.Errorf("report = %+v, want the late resource and its event", report)
	}
	if n := f.count("resources", "account_id = ?", "acct-harbor") + f.count("events", "aggregate_id = ?", "urn:recipe:late") +
		f.count("triples", "subject = ?", "urn:recipe:late"); n != 0 {
		t.Errorf("%d orphan row(s) remain after the sweep", n)
	}
	if left, _ := purger.Remains(context.Background(), "acct-harbor"); left {
		t.Error("Remains still true after the orphans were swept")
	}
}
