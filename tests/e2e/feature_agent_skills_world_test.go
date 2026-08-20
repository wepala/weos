package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/agents"
	"github.com/wepala/weos/v3/internal/config"
)

// TestFeatureAgentSkills runs the acceptance contract for story #485.
//
//	GODOG_TAGS=@story-485 go test ./tests/e2e/ -run TestFeatureAgentSkills
func TestFeatureAgentSkills(t *testing.T) {
	tags := "~@wip"
	if v := os.Getenv("GODOG_TAGS"); v != "" {
		tags = v
	}
	suite := godog.TestSuite{
		ScenarioInitializer: initFeatureAgentSkillsScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/feature_flag_agent_skills.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("agent skill gating acceptance scenarios failed")
	}
}

// --- the world -------------------------------------------------------------

// skillInstall is one agent-skill resource the Background installs.
type skillInstall struct {
	name    string
	gatedBy string
	tools   []string
}

// turnRecord is what one conversation turn observed, kept per person so a
// scenario can compare two people without re-running either turn.
type turnRecord struct {
	offered  []string
	reply    string
	err      error
	ranSkill string
}

type skillsWorld struct {
	app    *fx.App
	tmpDir string
	dsn    string
	boots  int

	registry     *application.FeatureRegistry
	features     *application.FeatureService
	resolver     *application.FeatureResolver
	client       *openfeature.Client
	orchestrator *appagents.Orchestrator
	skillReg     *application.SkillRegistry

	settings *toolsSettingsSpy
	grants   *toolsGrantSpy
	invSpy   *invalidatorSpy
	logs     *loggerSpy

	resources application.ResourceService
	rts       application.ResourceTypeService

	authService authapp.AuthenticationService
	accountRepo authrepos.AccountRepository

	declared    []entities.FeatureMeta
	installs    []skillInstall
	maxCacheAge time.Duration
	scriptPath  string
	scriptRules []map[string]any

	people   map[string]*featurePerson
	accounts map[string]string

	signedInOrder []string
	turns         map[string]*turnRecord
	invocations   map[string]error
	skillRuns     map[string]int

	restartBaseline int
	readsBefore     int
	readsAfterTurn  int
	invsBefore      int

	straightAwayOffered []string
	afterMomentErr      error
	momentSet           bool
	moment              time.Time
	windowStart         time.Time

	lastActor         string
	skillsInstalled   bool
	presets           []string
	toolCallAttempted bool
	toolCallAllowed   bool
}

func newSkillsWorld() *skillsWorld {
	return &skillsWorld{
		people:      map[string]*featurePerson{},
		accounts:    map[string]string{},
		turns:       map[string]*turnRecord{},
		invocations: map[string]error{},
		skillRuns:   map[string]int{},
	}
}

func (w *skillsWorld) teardown() {
	if w.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(ctx)
		w.app = nil
	}
	os.Unsetenv(agents.AgentScriptEnv)
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
	}
}

// defaultScript answers every turn with a plain reply. Scenarios that need
// routing replace it before the app boots.
func defaultSkillScript() []map[string]any {
	return []map[string]any{
		{"reply": "I looked at that for you."},
	}
}

func (w *skillsWorld) writeScript() error {
	rules := w.scriptRules
	if rules == nil {
		rules = defaultSkillScript()
	}
	raw, err := json.Marshal(map[string]any{"rules": rules})
	if err != nil {
		return err
	}
	w.scriptPath = filepath.Join(w.tmpDir, "agent-script.json")
	if err := os.WriteFile(w.scriptPath, raw, 0o600); err != nil {
		return err
	}
	os.Setenv(agents.AgentScriptEnv, w.scriptPath)
	return nil
}

func (w *skillsWorld) boot() error {
	if w.app != nil {
		return nil
	}
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-feature-skills-e2e-")
		if err != nil {
			return fmt.Errorf("could not create a temp dir: %w", err)
		}
		w.tmpDir = dir
		w.dsn = filepath.Join(dir, "skills.db")
	}
	if err := w.writeScript(); err != nil {
		return err
	}
	pw := "true"
	os.Setenv("PASSWORD_AUTH_ENABLED", pw)
	os.Unsetenv("GOOGLE_CLIENT_ID")
	os.Unsetenv("GOOGLE_CLIENT_SECRET")

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	if w.maxCacheAge > 0 {
		cfg.Features.CacheMaxAge = w.maxCacheAge
	}
	cfg.Features.Declared = declarationsBeyondCore(w.declared)

	w.settings = &toolsSettingsSpy{}
	w.grants = &toolsGrantSpy{}
	w.invSpy = &invalidatorSpy{}
	w.logs = &loggerSpy{}

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Decorate(func(inner repositories.FeatureSettingsRepository) repositories.FeatureSettingsRepository {
			w.settings.inner = inner
			return w.settings
		}),
		fx.Decorate(func(inner repositories.FeatureGrantRepository) repositories.FeatureGrantRepository {
			w.grants.inner = inner
			return w.grants
		}),
		fx.Decorate(func(inner repositories.FeatureCacheInvalidator) repositories.FeatureCacheInvalidator {
			w.invSpy.inner = inner
			return w.invSpy
		}),
		fx.Decorate(func(inner entities.Logger) entities.Logger {
			w.logs.inner = inner
			return w.logs
		}),
		fx.Populate(&w.registry, &w.features, &w.resolver, &w.client),
		fx.Populate(&w.orchestrator, &w.skillReg),
		fx.Populate(&w.resources, &w.rts),
		fx.Populate(&w.authService, &w.accountRepo),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	w.boots++
	w.restartBaseline = w.boots
	// The skills are installed by whichever step first needs them: the
	// agent-skill resource type arrives with the preset, which the Background
	// installs after the application is up.
	w.skillsInstalled = false
	return nil
}

// ensureSkills installs the Background's skills the first time a step needs
// them, and after a reboot finds them already in the database.
func (w *skillsWorld) ensureSkills() error {
	if w.skillsInstalled {
		return nil
	}
	for _, name := range w.presets {
		if _, err := w.rts.InstallPreset(context.Background(), name, true); err != nil {
			return fmt.Errorf("could not install the %q preset: %w", name, err)
		}
	}
	existing, err := w.skillReg.Skills(context.Background())
	if err == nil && len(existing) >= len(w.installs) && len(w.installs) > 0 {
		w.skillsInstalled = true
		return nil
	}
	if err := w.installSkills(); err != nil {
		return err
	}
	w.skillsInstalled = true
	return nil
}

func (w *skillsWorld) reboot() error {
	if w.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		if err := w.app.Stop(ctx); err != nil {
			return err
		}
		w.app = nil
	}
	return w.boot()
}

// installSkills creates the agent-skill resources the Background names. The
// resources are created once; a reboot finds them already there.
func (w *skillsWorld) installSkills() error {
	ctx := context.Background()
	existing, err := w.skillReg.Skills(ctx)
	if err == nil && len(existing) >= len(w.installs) && len(w.installs) > 0 {
		return nil
	}
	for _, s := range w.installs {
		if err := w.installSkill(s); err != nil {
			return err
		}
	}
	w.skillReg.MarkDirty()
	return nil
}

func (w *skillsWorld) installSkill(s skillInstall) error {
	tools := s.tools
	if tools == nil {
		tools = []string{}
	}
	body := map[string]any{
		"schemaVersion": entities.SkillSchemaVersion,
		"name":          s.name,
		"description":   "Handles " + strings.ReplaceAll(s.name, "_", " ") + " requests",
		"instructions":  "You are the " + s.name + " skill. Answer briefly.",
		"tools":         tools,
		"mode":          entities.SkillModeTask,
	}
	if s.gatedBy != "" {
		body["gatedBy"] = s.gatedBy
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if _, err := w.resources.Create(context.Background(), application.CreateResourceCommand{
		TypeSlug: application.AgentSkillTypeSlug,
		Data:     raw,
	}); err != nil {
		return fmt.Errorf("could not install the skill %q: %w", s.name, err)
	}
	return nil
}

// --- people ---------------------------------------------------------------

func (w *skillsWorld) personFor(email, password string) (*featurePerson, error) {
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
			return nil, err
		}
	}
	w.people[email] = p
	return p, nil
}

func (w *skillsWorld) person(email string) (*featurePerson, error) {
	p, ok := w.people[email]
	if !ok {
		return nil, fmt.Errorf("no person named %q has been staged", email)
	}
	return p, nil
}

func (w *skillsWorld) accountFor(name string) (string, error) {
	id, ok := w.accounts[name]
	if !ok {
		return "", fmt.Errorf("no account named %q has been staged", name)
	}
	return id, nil
}

func (w *skillsWorld) addToAccount(email, accountName string) error {
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(
		context.Background(), accountID, p.agentID, authentities.RoleMember); err != nil {
		return err
	}
	w.resolver.InvalidateAgents(context.Background(), accountID, p.agentID)
	p.accountID = accountID
	return nil
}

func (w *skillsWorld) ctxFor(p *featurePerson) context.Context {
	if p == nil {
		return context.Background()
	}
	return auth.ContextWithAgent(context.Background(),
		&auth.Identity{AgentID: p.agentID, ActiveAccountID: p.accountID})
}

func (w *skillsWorld) staged() []string {
	emails := make([]string, 0, len(w.people))
	for email := range w.people {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	return emails
}

// --- turns ----------------------------------------------------------------

// takeTurn runs a real conversation turn and records what the coordinator was
// given for it. The turn goes through the orchestrator so the graph is really
// built; the offered list is read from the same filter buildRoot uses, which
// the routing scenarios then prove is the one the graph honors.
func (w *skillsWorld) takeTurn(p *featurePerson, message, conversationID string) *turnRecord {
	ctx := w.ctxFor(p)
	rec := &turnRecord{}
	if err := w.ensureSkills(); err != nil {
		rec.err = err
		return rec
	}

	offered, err := w.orchestrator.RoutableSkills(ctx)
	if err != nil {
		rec.err = err
	}
	for _, def := range offered {
		rec.offered = append(rec.offered, def.Name)
	}
	sort.Strings(rec.offered)

	userID := "anonymous"
	if p != nil {
		userID = p.agentID
	}
	var sb strings.Builder
	convErr := w.orchestrator.ConverseStream(ctx, conversationID, userID, message, "",
		func(e entities.AgentEvent) {
			if e.Type == entities.AgentEventText {
				sb.WriteString(e.Text)
			}
		})
	rec.reply = sb.String()
	if convErr != nil && rec.err == nil {
		rec.err = convErr
	}
	// Which skill answered is read from the reply, not from the event stream:
	// the stream carries no transfer event, and the script gives each skill a
	// marker of its own precisely so a scenario can tell. A transfer the
	// coordinator could not make leaves the marker absent, which is the
	// difference the routing scenarios turn on.
	rec.ranSkill = skillMarkerIn(rec.reply)

	key := "anonymous"
	if p != nil {
		key = p.email
		w.lastActor = p.email
	}
	w.turns[key] = rec
	return rec
}

// skillAnsweredMarker is what a skill's scripted reply says so a scenario can
// tell which agent produced the turn. The coordinator's own replies never
// carry it.
const skillAnsweredMarker = "ANSWERED-BY:"

// skillMarkerIn reports which skill answered, or "" when the coordinator did.
func skillMarkerIn(reply string) string {
	i := strings.Index(reply, skillAnsweredMarker)
	if i < 0 {
		return ""
	}
	rest := reply[i+len(skillAnsweredMarker):]
	// The reply reaches the harness as the turn's JSON widget payload, so the
	// marker is followed by a quote or a brace rather than whitespace.
	if j := strings.IndexAny(rest, " \n\t.,\"}\\"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// invokeSkill uses the direct door — the one a client uses to keep its flow
// deterministic, and the one a stale skill name arrives through.
func (w *skillsWorld) invokeSkill(p *featurePerson, name, conversationID string) error {
	if err := w.ensureSkills(); err != nil {
		return err
	}
	ctx := w.ctxFor(p)
	userID := "anonymous"
	if p != nil {
		userID = p.agentID
	}
	err := w.orchestrator.ConverseStream(ctx, conversationID, userID, "do it", name,
		func(e entities.AgentEvent) {
			if e.Type == entities.AgentEventText && strings.TrimSpace(e.Text) != "" {
				w.skillRuns[name]++
			}
		})
	key := name
	if p != nil {
		key = p.email + "|" + name
	}
	w.invocations[key] = err
	return err
}

func (w *skillsWorld) invocationFor(email, name string) (error, bool) {
	key := name
	if email != "" {
		key = email + "|" + name
	}
	err, ok := w.invocations[key]
	return err, ok
}

// refusedByGate separates "your features do not reach this" from "no such
// skill" and from a real failure. The contract needs all three apart.
func refusedBySkillGate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "is not available: the")
}

func refusedAsUnknown(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown skill")
}

func (w *skillsWorld) turnFor(email string) (*turnRecord, error) {
	rec, ok := w.turns[email]
	if !ok {
		return nil, fmt.Errorf("%q has taken no turn with the in-app agent", email)
	}
	return rec, nil
}

func (w *skillsWorld) soleTurn() (*turnRecord, error) {
	if w.lastActor != "" {
		if rec, ok := w.turns[w.lastActor]; ok {
			return rec, nil
		}
	}
	if rec, ok := w.turns["anonymous"]; ok {
		return rec, nil
	}
	return nil, fmt.Errorf("no turn has been taken")
}

func (w *skillsWorld) soleActor() (*featurePerson, error) {
	if len(w.signedInOrder) != 1 {
		return nil, fmt.Errorf("%d people are signed in, so \"they\" is ambiguous", len(w.signedInOrder))
	}
	return w.person(w.signedInOrder[0])
}

var _ = testing.Verbose
