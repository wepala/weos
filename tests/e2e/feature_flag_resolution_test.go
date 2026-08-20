package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/open-feature/go-sdk/openfeature"
	"go.uber.org/fx"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

// TestFeatureFlagResolution runs the acceptance contract for story #481.
//
// The scenarios were promoted out of @wip once green, so they run in the
// default suite. GODOG_TAGS still overrides the filter when a subset is wanted:
//
//	GODOG_TAGS=@story-481 go test ./tests/e2e/ -run TestFeatureFlagResolution
func TestFeatureFlagResolution(t *testing.T) {
	tags := "~@wip"
	if v := os.Getenv("GODOG_TAGS"); v != "" {
		tags = v
	}
	suite := godog.TestSuite{
		ScenarioInitializer: initFeatureFlagScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/feature_flag_resolution.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("feature flag resolution acceptance scenarios failed")
	}
}

// --- spies -----------------------------------------------------------------

// settingsSpy counts reads of each override layer and can be made unreadable.
// The read counts are what make "read from the database only while the first
// evaluation was answered" and "no account override or grant was read"
// assertions rather than intentions.
type settingsSpy struct {
	inner repositories.FeatureSettingsRepository

	mu sync.Mutex
	// Counted per agent, because the contract asks whose evaluation caused a
	// read — "broker read no feature state to be answered" is false if it is
	// measured across everybody, since somebody else re-resolving in the same
	// scenario would mask it.
	instanceReads map[string]int
	accountReads  map[string]int
	broken        bool
}

// agentOf attributes a read to whoever is on the context. The empty string is
// the anonymous caller.
func agentOf(ctx context.Context) string {
	if id := auth.AgentFromCtx(ctx); id != nil {
		return id.AgentID
	}
	return ""
}

var errStoreUnreadable = fmt.Errorf("the feature store cannot be read")

func (s *settingsSpy) InstanceOverrides(ctx context.Context) (map[string]bool, error) {
	s.mu.Lock()
	if s.instanceReads == nil {
		s.instanceReads = map[string]int{}
	}
	s.instanceReads[agentOf(ctx)]++
	broken := s.broken
	s.mu.Unlock()
	if broken {
		return nil, errStoreUnreadable
	}
	return s.inner.InstanceOverrides(ctx)
}

func (s *settingsSpy) AccountOverrides(ctx context.Context, accountID string) (map[string]bool, error) {
	s.mu.Lock()
	if s.accountReads == nil {
		s.accountReads = map[string]int{}
	}
	s.accountReads[agentOf(ctx)]++
	broken := s.broken
	s.mu.Unlock()
	if broken {
		return nil, errStoreUnreadable
	}
	return s.inner.AccountOverrides(ctx, accountID)
}

func (s *settingsSpy) SetOverride(ctx context.Context, scopeType, scopeID, key string, enabled bool) error {
	return s.inner.SetOverride(ctx, scopeType, scopeID, key, enabled)
}

func (s *settingsSpy) ClearOverride(ctx context.Context, scopeType, scopeID, key string) error {
	return s.inner.ClearOverride(ctx, scopeType, scopeID, key)
}

func (s *settingsSpy) countsFor(agentID string) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instanceReads[agentID], s.accountReads[agentID]
}

func (s *settingsSpy) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instanceReads, s.accountReads = map[string]int{}, map[string]int{}
}

func (s *settingsSpy) setBroken(b bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broken = b
}

type grantsSpy struct {
	inner repositories.FeatureGrantRepository

	mu     sync.Mutex
	reads  map[string]int
	broken bool
}

func (g *grantsSpy) GrantsFor(
	ctx context.Context, accountID, agentID, roleID string,
) ([]entities.FeatureGrantRecord, error) {
	g.mu.Lock()
	if g.reads == nil {
		g.reads = map[string]int{}
	}
	g.reads[agentOf(ctx)]++
	broken := g.broken
	g.mu.Unlock()
	if broken {
		return nil, errStoreUnreadable
	}
	return g.inner.GrantsFor(ctx, accountID, agentID, roleID)
}

func (g *grantsSpy) ListByFeature(
	ctx context.Context, accountID, key string,
) ([]entities.FeatureGrantRecord, error) {
	return g.inner.ListByFeature(ctx, accountID, key)
}

func (g *grantsSpy) Grant(ctx context.Context, record entities.FeatureGrantRecord) error {
	return g.inner.Grant(ctx, record)
}

func (g *grantsSpy) Revoke(
	ctx context.Context, subjectType, subjectID, accountID, key string,
) (bool, error) {
	return g.inner.Revoke(ctx, subjectType, subjectID, accountID, key)
}

func (g *grantsSpy) countFor(agentID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads[agentID]
}

func (g *grantsSpy) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reads = map[string]int{}
}

func (g *grantsSpy) setBroken(b bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.broken = b
}

// logSpy captures log lines so "logged once" and "the log names the key" are
// checked rather than assumed.
type logSpy struct {
	inner entities.Logger
	mu    sync.Mutex
	lines []string
}

func (l *logSpy) record(msg string, fields ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	parts := []string{msg}
	for _, f := range fields {
		parts = append(parts, fmt.Sprint(f))
	}
	l.lines = append(l.lines, strings.Join(parts, " "))
}

func (l *logSpy) Debug(ctx context.Context, msg string, f ...interface{}) {
	l.record(msg, f...)
	if l.inner != nil {
		l.inner.Debug(ctx, msg, f...)
	}
}

func (l *logSpy) Info(ctx context.Context, msg string, f ...interface{}) {
	l.record(msg, f...)
}

func (l *logSpy) Warn(ctx context.Context, msg string, f ...interface{}) {
	l.record(msg, f...)
}

func (l *logSpy) Error(ctx context.Context, msg string, f ...interface{}) {
	l.record(msg, f...)
}

func (l *logSpy) matching(substrs ...string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		all := true
		for _, s := range substrs {
			if !strings.Contains(line, s) {
				all = false
				break
			}
		}
		if all {
			out = append(out, line)
		}
	}
	return out
}

// --- world -----------------------------------------------------------------

// featurePerson is one human: the identity the instance knows them by, and the
// sessions they hold. Sessions are real AuthSessions so "they are still signed
// in" can be asserted rather than assumed — the property most at risk of being
// implemented as a forced logout.
type featurePerson struct {
	email      string
	password   string
	agentID    string
	accountID  string
	sessionIDs []string
}

type featureWorld struct {
	app    *fx.App
	tmpDir string
	dsn    string

	registry *application.FeatureRegistry
	resolver *application.FeatureResolver
	provider *application.FeatureProvider
	service  *application.FeatureService
	settings *settingsSpy
	grants   *grantsSpy
	logs     *logSpy
	client   *openfeature.Client

	authService authapp.AuthenticationService
	accountRepo authrepos.AccountRepository
	credRepo    authrepos.CredentialRepository

	maxCacheAge time.Duration

	// presets is the live registry the app was built with, so a scenario can
	// install a preset after boot and see its features immediately.
	presets *application.PresetRegistry

	accounts map[string]string         // scenario name -> account id
	people   map[string]*featurePerson // email -> person
	order    []string                  // who was introduced, for "each of them"
	listing  []entities.FeatureMeta    // last "registered features are listed"
	focused  *entities.FeatureMeta     // last "the listing includes X"
	answers  map[string][]bool         // email -> answers in order
	lastBool *bool                     // last single answer
	lastRsn  openfeature.Reason        // last reason
	lastAny  any                       // last non-boolean answer

	// current is who "they" means: the person the scenario last acted as.
	// Without it "they evaluate" resolves to whoever was introduced first,
	// which is usually the account owner rather than the person under test.
	current *featurePerson
	// group is who "both of them" / "all three of them" means. Set by the
	// membership steps that name people, so a Background-created owner is not
	// swept in.
	group []string

	// declared remembers the code declarations so a reboot can replay them.
	declared []entities.FeatureMeta

	// pendingPreset holds a preset a scenario staged but has not installed.
	pendingPreset  string
	pendingFeature entities.FeatureMeta

	// grantedKey remembers what was last granted, so "the grant is taken away
	// from X" knows which grant the scenario means.
	grantedKey string

	// straightAway and afterMaxAge separate the two evaluations the bounded
	// staleness scenario makes, since it asserts on both.
	straightAway bool
	afterMaxAge  bool

	suppliedNonBool string

	envBefore map[string]*string
}

// presetRegistry exposes the registry the app was built with.
func (w *featureWorld) presetRegistry() *application.PresetRegistry { return w.presets }

func (w *featureWorld) teardown() {
	if w.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(ctx)
		w.app = nil
	}
	for k, v := range w.envBefore {
		if v == nil {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, *v)
		}
	}
	if w.tmpDir != "" {
		os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

func (w *featureWorld) setEnv(key string, value *string) {
	if w.envBefore == nil {
		w.envBefore = map[string]*string{}
	}
	if _, seen := w.envBefore[key]; !seen {
		if old, ok := os.LookupEnv(key); ok {
			w.envBefore[key] = &old
		} else {
			w.envBefore[key] = nil
		}
	}
	if value == nil {
		os.Unsetenv(key)
		return
	}
	os.Setenv(key, *value)
}

// boot starts the real application. Repositories are decorated with spies so
// read counts and store failures are observable; the contract asks for both.
func (w *featureWorld) boot() error {
	if w.app != nil {
		return nil
	}
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-feature-flags-e2e-")
		if err != nil {
			return fmt.Errorf("could not create a temp dir: %w", err)
		}
		w.tmpDir = dir
		w.dsn = filepath.Join(dir, "features.db")
	}
	pw := "true"
	w.setEnv("PASSWORD_AUTH_ENABLED", &pw)
	w.setEnv("GOOGLE_CLIENT_ID", nil)
	w.setEnv("GOOGLE_CLIENT_SECRET", nil)

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	if w.maxCacheAge > 0 {
		cfg.Features.CacheMaxAge = w.maxCacheAge
	}

	w.settings = &settingsSpy{}
	w.grants = &grantsSpy{}
	w.logs = &logSpy{}

	w.presets = presets.NewDefaultRegistry()

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, w.presets),
		fx.Decorate(func(inner repositories.FeatureSettingsRepository) repositories.FeatureSettingsRepository {
			w.settings.inner = inner
			return w.settings
		}),
		fx.Decorate(func(inner repositories.FeatureGrantRepository) repositories.FeatureGrantRepository {
			w.grants.inner = inner
			return w.grants
		}),
		fx.Decorate(func(inner entities.Logger) entities.Logger {
			w.logs.inner = inner
			return w.logs
		}),
		fx.Populate(&w.registry, &w.resolver, &w.provider, &w.service),
		fx.Populate(&w.authService, &w.accountRepo, &w.credRepo),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app

	// Evaluate through the OpenFeature client the way a call site will, on a
	// named domain rather than the global slot — each scenario boots its own
	// application in this one binary, and a stale global registration would
	// evaluate against a previous application's resolver.
	if err := openfeature.SetNamedProviderAndWait(application.FeatureProviderDomain, w.provider); err != nil {
		return fmt.Errorf("could not register the feature provider: %w", err)
	}
	w.client = openfeature.NewClient(application.FeatureProviderDomain)

	// Replay code declarations after a reboot. The database survives (same
	// DSN), but the registry is rebuilt from the binary by design.
	for _, m := range w.declared {
		if _, known := w.registry.Lookup(m.Key); !known {
			if err := w.registry.Register(m); err != nil {
				return fmt.Errorf("could not re-declare %q after reboot: %w", m.Key, err)
			}
		}
	}
	return nil
}

// reboot restarts the application against the same database, which is how a
// scenario changes configuration that is only read at construction.
func (w *featureWorld) reboot() error {
	if w.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		if err := w.app.Stop(ctx); err != nil {
			return fmt.Errorf("could not stop the app to reconfigure it: %w", err)
		}
		w.app = nil
	}
	return w.boot()
}

// --- staging ---------------------------------------------------------------

func (w *featureWorld) declareFeatures(table *godog.Table) error {
	if err := w.boot(); err != nil {
		return err
	}
	for i, row := range table.Rows {
		if i == 0 {
			continue // header
		}
		cell := func(n int) string { return strings.TrimSpace(row.Cells[n].Value) }
		meta := entities.FeatureMeta{
			Key:         cell(0),
			DisplayName: cell(1),
			Description: cell(2),
			Default:     cell(3) == "on",
			Manageable:  cell(4) == "yes",
			Grantable:   cell(5) == "yes",
		}
		if err := w.registry.Register(meta); err != nil {
			return fmt.Errorf("could not declare %q: %w", meta.Key, err)
		}
		w.declared = append(w.declared, meta)
	}
	return nil
}

func (w *featureWorld) personFor(email, password string) (*featurePerson, error) {
	if p, ok := w.people[email]; ok {
		return p, nil
	}
	ctx := context.Background()
	agent, _, account, err := w.authService.RegisterPassword(ctx, email, displayNameForFeature(email), password)
	if err != nil {
		return nil, fmt.Errorf("could not create %q: %w", email, err)
	}
	p := &featurePerson{email: email, password: password, agentID: agent.GetID()}
	if account != nil {
		p.accountID = account.GetID()
		if err := w.accountRepo.SaveMember(ctx, account.GetID(), agent.GetID(), authentities.RoleOwner); err != nil {
			return nil, fmt.Errorf("could not record %q as owner: %w", email, err)
		}
	}
	w.people[email] = p
	w.order = append(w.order, email)
	return p, nil
}

func displayNameForFeature(email string) string {
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}

func (w *featureWorld) accountWithOwner(name, email, password string) error {
	if err := w.boot(); err != nil {
		return err
	}
	p, err := w.personFor(email, password)
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("%q has no account to name %q", email, name)
	}
	w.accounts[name] = p.accountID
	return nil
}

// alsoBelongsTo puts a second person into an existing account with a role.
func (w *featureWorld) alsoBelongsTo(email, accountName, role string) error {
	accountID, ok := w.accounts[accountName]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", accountName)
	}
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	if role == "" {
		role = authentities.RoleMember
	}
	if err := w.accountRepo.SaveMember(context.Background(), accountID, p.agentID, role); err != nil {
		return fmt.Errorf("could not add %q to %q: %w", email, accountName, err)
	}
	p.accountID = accountID
	return nil
}

func (w *featureWorld) person(email string) (*featurePerson, error) {
	p, ok := w.people[email]
	if !ok {
		return nil, fmt.Errorf("no person named %q has been staged", email)
	}
	return p, nil
}

func (w *featureWorld) accountFor(name string) (string, error) {
	id, ok := w.accounts[name]
	if !ok {
		return "", fmt.Errorf("no account named %q has been staged", name)
	}
	return id, nil
}

// signIn creates a real AuthSession, so a later "still signed in" assertion
// means something.
func (w *featureWorld) signIn(p *featurePerson) error {
	ctx := context.Background()
	creds, err := w.credRepo.FindByAgent(ctx, p.agentID)
	if err != nil || len(creds) == 0 {
		return fmt.Errorf("could not find credentials for %q: %w", p.email, err)
	}
	s, err := w.authService.CreateSession(ctx, p.agentID, p.accountID, creds[0].GetID(),
		"127.0.0.1", "godog", time.Hour)
	if err != nil {
		return fmt.Errorf("could not sign %q in: %w", p.email, err)
	}
	p.sessionIDs = append(p.sessionIDs, s.GetID())
	w.current = p
	return nil
}

func (w *featureWorld) ctxFor(p *featurePerson) context.Context {
	return auth.ContextWithAgent(context.Background(),
		&auth.Identity{AgentID: p.agentID, ActiveAccountID: p.accountID})
}

// --- evaluation ------------------------------------------------------------

func (w *featureWorld) evaluate(p *featurePerson, key string, def bool) bool {
	ctx := context.Background()
	if p != nil {
		ctx = w.ctxFor(p)
	}
	detail, _ := w.client.BooleanValueDetails(ctx, application.FeatureFlagPrefix+key, def,
		openfeature.EvaluationContext{})
	w.lastBool = &detail.Value
	w.lastRsn = detail.Reason
	if p != nil {
		w.answers[p.email] = append(w.answers[p.email], detail.Value)
	}
	return detail.Value
}

func (w *featureWorld) evaluateRawKey(p *featurePerson, flag string, def bool) bool {
	detail, _ := w.client.BooleanValueDetails(w.ctxFor(p), flag, def, openfeature.EvaluationContext{})
	w.lastBool = &detail.Value
	w.lastRsn = detail.Reason
	return detail.Value
}

// actor is who "they" refers to: the person the scenario last signed in or
// named, falling back to the only person there is.
func (w *featureWorld) actor() (*featurePerson, error) {
	if w.current != nil {
		return w.current, nil
	}
	if len(w.order) == 0 {
		return nil, fmt.Errorf("no person has been staged")
	}
	return w.people[w.order[0]], nil
}

// cohort is who "both of them" / "all three of them" refers to.
//
// A scenario that names two or more people in its Givens means those people —
// "counsel and clerk both hold the role admin ... both of them are signed in"
// must not sweep in the Background's account owner. A scenario that names one
// person means them plus the owner already in play — "counsel also belongs ...
// both of them are signed in" is ops and counsel. So two or more named wins,
// otherwise everyone introduced.
func (w *featureWorld) cohort() []string {
	if len(w.group) >= 2 {
		return w.group
	}
	return w.order
}

func boolWord(s string) bool { return strings.TrimSpace(s) == "on" }
