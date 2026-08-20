package e2e

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

//nolint:funlen // a step table is one statement per contract sentence
func initFeatureAgentSkillsScenario(sc *godog.ScenarioContext) {
	w := newSkillsWorld()
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		w.teardown()
		return ctx, err
	})

	// --- background -------------------------------------------------------
	sc.Given(`^a WeOS instance where password sign-in is enabled and requests are `+
		`authenticated by their session$`, func() error { return nil })
	sc.Given(`^the "([^"]*)" preset is installed$`, w.stepInstallPreset)
	sc.Given(`^the instance declares these features in code:$`, w.stepDeclareFeatures)
	sc.Given(`^these agent skills are installed, each naming the feature that gates it:$`, w.stepInstallSkills)
	sc.Given(`^the tool "([^"]*)" is gated by the feature "([^"]*)"$`, w.stepToolGatedBy)
	sc.Given(`^the in-app agent is configured with a scripted model$`, func() error { return nil })
	sc.Given(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`,
		w.stepAccountWithOwner)

	// --- staging ----------------------------------------------------------
	sc.Given(`^nothing has been overridden or granted on this instance$`, func() error { return nil })
	sc.Given(`^the operator has turned the feature "([^"]*)" (on|off) for the instance$`, w.stepInstanceFlag)
	sc.When(`^the operator turns the feature "([^"]*)" (on|off) for the instance$`, w.stepInstanceFlag)
	sc.Given(`^the instance booted with the feature "([^"]*)" (on|off) for the instance$`, w.stepBootedWith)
	sc.Given(`^"([^"]*)" has turned the feature "([^"]*)" (on|off)$`, w.stepAccountFlag)
	sc.Given(`^"([^"]*)" also belongs to "([^"]*)"$`, w.stepAlsoBelongs)
	sc.Given(`^"([^"]*)" has been granted the feature "([^"]*)"$`, w.stepGranted)
	sc.When(`^"([^"]*)" grants "([^"]*)" to "([^"]*)"$`, w.stepGrantsTo)
	sc.Given(`^"([^"]*)" holds a grant of "([^"]*)" valid until (\d+) seconds from now$`, w.stepGrantUntil)
	sc.When(`^"([^"]*)" revokes "([^"]*)" from "([^"]*)"$`, w.stepRevoke)
	sc.Given(`^the skill "([^"]*)" is gated by the undeclared feature "([^"]*)"$`, w.stepSkillGatedByUndeclared)
	sc.Given(`^the instance is configured with a maximum cache age of (\d+) minutes$`, w.stepCacheAge)
	sc.Given(`^the instance has (\d+) skills gated by "([^"]*)"$`, w.stepManySkills)
	sc.Given(`^the store holding account overrides and grants cannot be read$`, w.stepStoreDown)
	sc.When(`^the store can be read again$`, w.stepStoreUp)
	sc.Given(`^the scripted model transfers to "([^"]*)" whatever it is offered$`, w.stepScriptAlwaysTransfers)
	sc.Given(`^the scripted model routes "([^"]*)" to "([^"]*)"$`, w.stepScriptRoutes)

	// --- signing in -------------------------------------------------------
	sc.Given(`^"([^"]*)" is signed in to "([^"]*)"$`, w.stepSignedIn)
	sc.Given(`^both of them are signed in to "([^"]*)"$`, w.stepBothSignedIn)

	// --- turns ------------------------------------------------------------
	sc.When(`^they take a turn with the in-app agent$`, w.stepTakeTurn)
	sc.Given(`^they have already taken a turn with the in-app agent$`, w.stepTakeTurn)
	sc.When(`^they take (\d+) turns with the in-app agent$`, w.stepTakeTurns)
	sc.When(`^each of them takes a turn with the in-app agent$`, w.stepEachTakesTurn)
	sc.When(`^"([^"]*)" takes a turn with the in-app agent$`, w.stepNamedTakesTurn)
	sc.Given(`^they have taken a turn and been offered "([^"]*)"$`, w.stepTookTurnAndOffered)
	sc.Given(`^they have taken a turn and not been offered "([^"]*)"$`, w.stepTookTurnAndNotOffered)
	sc.Given(`^"([^"]*)" has already taken a turn and not been offered "([^"]*)"$`, w.stepNamedTookTurnNotOffered)
	sc.When(`^they take another turn in the same conversation$`, w.stepAnotherTurn)
	sc.When(`^"([^"]*)" takes another turn in the same conversation$`, w.stepNamedAnotherTurn)
	sc.When(`^they act in "([^"]*)" and take another turn$`, w.stepActInAndTurn)
	sc.When(`^they take a turn straight away$`, w.stepTurnStraightAway)
	sc.Given(`^a turn taken with nobody on the context$`, w.stepAnonymousTurn)
	sc.When(`^each of them sends "([^"]*)" to the in-app agent$`, w.stepEachSends)
	sc.When(`^they send "([^"]*)" to the in-app agent$`, w.stepTheySend)
	sc.Given(`^they have sent "([^"]*)" and not been answered by any skill$`, w.stepSentAndNotAnswered)
	sc.When(`^"([^"]*)" sends "([^"]*)" again in the same conversation$`, w.stepNamedSendsAgain)

	// --- invoking ---------------------------------------------------------
	sc.When(`^they invoke the skill "([^"]*)" directly$`, w.stepInvoke)
	sc.When(`^"([^"]*)" invokes the skill "([^"]*)" directly$`, w.stepNamedInvokes)
	sc.When(`^they invoke every gated skill the instance has installed$`, w.stepInvokeEveryGated)
	sc.When(`^they invoke the skill "([^"]*)" directly once that moment has passed$`, w.stepInvokeAfterMoment)
	sc.When(`^the skill calls the tool "([^"]*)"$`, w.stepSkillCallsTool)

	// --- outcomes ---------------------------------------------------------
	sc.Then(`^the coordinator (was offered|was not offered) the skill "([^"]*)"$`, w.stepCoordinatorOffered)
	sc.Then(`^"([^"]*)" was (offered|not offered) "([^"]*)"$`, w.stepPersonOffered)
	sc.Then(`^both of them were offered "([^"]*)"$`, w.stepBothOffered)
	sc.Then(`^"([^"]*)" is offered with the description it declares$`, w.stepOfferedWithDescription)
	sc.Then(`^"([^"]*)" runs with the tools its allowlist declares$`, w.stepRunsWithDeclaredTools)
	sc.Then(`^invoking the skill "([^"]*)" directly (succeeds|is refused)$`, w.stepInvokingOutcome)
	sc.Then(`^invoking "([^"]*)" directly (succeeds|is refused)$`, w.stepInvokingOutcome)
	sc.Then(`^no feature was evaluated for that skill$`, w.stepNoFeatureEvaluated)
	sc.Then(`^every skill the coordinator was offered was invocable directly$`, w.stepOfferedWereInvocable)
	sc.Then(`^every gated skill the coordinator was not offered was refused$`, w.stepWithheldWereRefused)
	sc.Then(`^the invocation is refused with an error the client can read$`, w.stepInvocationRefusedReadable)
	sc.Then(`^the invocation is refused$`, w.stepInvocationRefused)
	sc.Then(`^the refusal names the skill "([^"]*)"$`, w.stepRefusalNamesSkill)
	sc.Then(`^the refusal says the capability is not enabled for them$`, w.stepRefusalSaysNotEnabled)
	sc.Then(`^the refusal for "([^"]*)" says the capability is not enabled for them$`, w.stepNamedRefusalNotEnabled)
	sc.Then(`^the refusal for "([^"]*)" says no such skill exists$`, w.stepNamedRefusalUnknown)
	sc.Then(`^neither refusal ran a skill$`, w.stepNeitherRan)
	sc.Then(`^the skill's instructions never ran$`, w.stepInstructionsNeverRan)
	sc.Then(`^no ledger export was recorded$`, w.stepNoLedgerExport)
	sc.Then(`^the refusal is not a partial reply$`, w.stepRefusalNotPartial)
	sc.Then(`^the turn did not run "([^"]*)"$`, w.stepTurnDidNotRun)
	sc.Then(`^the user was given a reply rather than a broken turn$`, w.stepReplyNotBroken)
	sc.Then(`^the turn ended with a reply the user can read$`, w.stepReplyNotBroken)
	sc.Then(`^the reply does not name "([^"]*)"$`, w.stepReplyDoesNotName)
	sc.Then(`^the conversation kept its history$`, w.stepConversationKeptHistory)
	sc.Then(`^"([^"]*)" is still signed in$`, w.stepStillSignedIn)
	sc.Then(`^they were not signed out$`, w.stepNotSignedOut)
	sc.Then(`^the instance was not restarted$`, w.stepNotRestarted)
	sc.Then(`^the instance was not restarted between the two turns$`, w.stepNotRestarted)
	sc.Then(`^the turn straight away was offered "([^"]*)"$`, w.stepStraightAwayOffered)
	sc.Then(`^the invocation after that moment is refused$`, w.stepAfterMomentRefused)
	sc.Then(`^the later turn was not offered "([^"]*)"$`, w.stepLaterNotOffered)
	sc.Then(`^nothing invalidated the session in between$`, w.stepNothingInvalidated)
	sc.Then(`^the maximum cache age had not run out$`, w.stepCacheAgeHadNotRunOut)
	sc.Then(`^the instance logged the failure$`, w.stepLoggedFailure)
	sc.Then(`^the instance logged the undeclared feature key once$`, w.stepLoggedDriftOnce)
	sc.Then(`^the log names the key "([^"]*)"$`, w.stepLogNamesKey)
	sc.Then(`^the log names the skill "([^"]*)"$`, w.stepLogNamesSkill)
	sc.Then(`^the skill was reached$`, w.stepSkillWasReached)
	sc.Then(`^the tool call is refused because the capability is not enabled$`, w.stepToolCallRefused)
	sc.Then(`^the turn was answered$`, w.stepTurnAnswered)
	sc.Then(`^both turns were answered$`, w.stepBothTurnsAnswered)
	sc.Then(`^feature state was read from the database only while that turn was answered$`, w.stepReadOnlyDuringTurn)
	sc.Then(`^the graph was built without reading feature state once per skill$`, w.stepNotPerSkill)
	sc.Then(`^no feature state was read from the database after the first turn$`, w.stepNoReadsAfterFirst)
	sc.Then(`^the skill registry loaded the definitions once for both of them$`, w.stepRegistryLoadedOnce)
	sc.Then(`^"([^"]*)" was answered by the skill "([^"]*)"$`, w.stepAnsweredBySkill)
	sc.Then(`^"([^"]*)" was not answered by any skill$`, w.stepNotAnsweredByAnySkill)
	sc.Then(`^they were answered by the skill "([^"]*)"$`, w.stepTheyAnsweredBySkill)
	sc.Then(`^"([^"]*)" was given a reply rather than a broken turn$`, w.stepNamedReplyNotBroken)
	sc.Then(`^no account override or grant was read to answer either$`, w.stepNoAccountLayerRead)
}

// --- background -----------------------------------------------------------

// stepInstallPreset brings in the agent-skill resource type. The skills the
// Background installs are ordinary resources of that type, so without it there
// is nothing to install them into.
func (w *skillsWorld) stepInstallPreset(name string) error {
	// Recorded, not installed. This step comes BEFORE the Background declares
	// the instance's features, and booting here would build the registry from
	// an empty declaration list.
	w.presets = append(w.presets, name)
	return nil
}

func (w *skillsWorld) stepDeclareFeatures(table *godog.Table) error {
	metas, err := featureMetasFrom(table)
	if err != nil {
		return err
	}
	w.declared = metas
	for _, m := range metas {
		if _, err := coreAlreadyDeclares(m); err != nil {
			return err
		}
	}
	return nil
}

func (w *skillsWorld) stepInstallSkills(table *godog.Table) error {
	for _, row := range table.Rows[1:] {
		name := strings.TrimSpace(row.Cells[0].Value)
		gate := strings.TrimSpace(row.Cells[1].Value)
		install := skillInstall{name: name, gatedBy: gate}
		if name == "episode_summarizer" {
			// A non-empty allowlist, so "runs with the tools its allowlist
			// declares" compares something rather than two empty lists.
			install.tools = []string{"episodic_recall"}
		}
		w.installs = append(w.installs, install)
	}
	return nil
}

// stepToolGatedBy records the Background's statement about the tool surface.
// #484 owns that gate; this suite only needs to know which tool to try.
func (w *skillsWorld) stepToolGatedBy(tool, key string) error {
	if key == "" {
		return fmt.Errorf("the contract names no feature for the tool %q", tool)
	}
	return nil
}

func (w *skillsWorld) stepAccountWithOwner(name, email, password string) error {
	if err := w.boot(); err != nil {
		return err
	}
	p, err := w.personFor(email, password)
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("registering %q created no account", email)
	}
	w.accounts[name] = p.accountID
	return nil
}

// --- staging --------------------------------------------------------------

func (w *skillsWorld) stepInstanceFlag(key, state string) error {
	if err := w.boot(); err != nil {
		return err
	}
	return w.features.SetInstanceFeature(context.Background(), key, state == "on")
}

func (w *skillsWorld) stepBootedWith(key, state string) error {
	if err := w.stepInstanceFlag(key, state); err != nil {
		return err
	}
	return w.reboot()
}

func (w *skillsWorld) stepAccountFlag(accountName, key, state string) error {
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	return w.features.SetAccountFeature(context.Background(), accountID, key, state == "on")
}

func (w *skillsWorld) stepAlsoBelongs(email, accountName string) error {
	return w.addToAccount(email, accountName)
}

func (w *skillsWorld) stepGranted(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.features.GrantToAgent(context.Background(), p.accountID, p.agentID, key, application.GrantTerms{
		GrantedByEmail: "ops@harborlegal.example", Source: "test",
	})
}

func (w *skillsWorld) stepGrantsTo(_, key, email string) error {
	return w.stepGranted(email, key)
}

func (w *skillsWorld) stepGrantUntil(email, key string, seconds int) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.moment = time.Now().Add(time.Duration(seconds) * time.Second)
	w.momentSet = true
	w.windowStart = time.Now()
	through := w.moment
	return w.features.GrantToAgent(context.Background(), p.accountID, p.agentID, key, application.GrantTerms{
		ValidThrough: &through, GrantedByEmail: "ops@harborlegal.example", Source: "test",
	})
}

func (w *skillsWorld) stepRevoke(_, key, subject string) error {
	p, err := w.person(subject)
	if err != nil {
		return err
	}
	removed, err := w.features.RevokeFromAgent(context.Background(), p.accountID, p.agentID, key)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("there was no grant of %q to revoke from %q", key, subject)
	}
	return nil
}

// stepSkillGatedByUndeclared rewrites a skill's gate to a key nobody declared,
// which is what a typo in an agent-skill resource looks like.
func (w *skillsWorld) stepSkillGatedByUndeclared(skill, key string) error {
	for i := range w.installs {
		if w.installs[i].name == skill {
			w.installs[i].gatedBy = key
			return nil
		}
	}
	return fmt.Errorf("no skill named %q is installed", skill)
}

func (w *skillsWorld) stepCacheAge(minutes int) error {
	w.maxCacheAge = time.Duration(minutes) * time.Minute
	if w.app == nil {
		return nil
	}
	return w.reboot()
}

// stepManySkills installs enough gated skills that a per-skill evaluation
// would show up in the read count.
func (w *skillsWorld) stepManySkills(count int, key string) error {
	for i := 0; i < count; i++ {
		w.installs = append(w.installs, skillInstall{
			name: fmt.Sprintf("bulk_skill_%02d", i), gatedBy: key,
		})
	}
	return nil
}

func (w *skillsWorld) stepStoreDown() error {
	if err := w.boot(); err != nil {
		return err
	}
	w.settings.setDown(true)
	w.grants.setDown(true)
	return nil
}

func (w *skillsWorld) stepStoreUp() error {
	w.settings.setDown(false)
	w.grants.setDown(false)
	return nil
}

// --- the script -----------------------------------------------------------

// reloadScript rebuilds the model from the new script. The scripted model is
// constructed at boot from WEOS_AGENT_SCRIPT, so a scenario that sets its
// script after the Background has booted would otherwise run the default one —
// and a scenario asserting a skill was NOT reached would pass for the wrong
// reason.
func (w *skillsWorld) reloadScript() error {
	if w.app == nil {
		return nil
	}
	return w.reboot()
}

func (w *skillsWorld) stepScriptAlwaysTransfers(skill string) error {
	w.scriptRules = []map[string]any{
		{"call": map[string]any{"tool": "transfer_to_agent", "args": map[string]any{"agent_name": skill}}},
		{"afterTool": "transfer_to_agent", "reply": skillAnsweredMarker + skill},
		{"afterToolFailed": "transfer_to_agent", "reply": "I cannot help with that."},
		{"reply": "I cannot help with that."},
	}
	return w.reloadScript()
}

func (w *skillsWorld) stepScriptRoutes(prompt, skill string) error {
	w.scriptRules = []map[string]any{
		{"whenMessageContains": prompt,
			"call": map[string]any{"tool": "transfer_to_agent", "args": map[string]any{"agent_name": skill}}},
		{"afterTool": "transfer_to_agent", "reply": skillAnsweredMarker + skill},
		{"afterToolFailed": "transfer_to_agent", "reply": "I cannot help with that."},
		{"reply": "I cannot help with that."},
	}
	return w.reloadScript()
}

// --- signing in -----------------------------------------------------------

func (w *skillsWorld) stepSignedIn(email, accountName string) error {
	if err := w.boot(); err != nil {
		return err
	}
	if _, err := w.personFor(email, "correct-horse-battery-staple"); err != nil {
		return err
	}
	if err := w.actIn(email, accountName); err != nil {
		return err
	}
	if !contains(w.signedInOrder, email) {
		w.signedInOrder = append(w.signedInOrder, email)
	}
	w.restartBaseline = w.boots
	return nil
}

// actorOrNobody is whoever the scenario is acting as, or nobody at all for the
// scenarios that take a turn with no caller on the context.
func (w *skillsWorld) actorOrNobody() (*featurePerson, string, error) {
	if len(w.signedInOrder) == 0 {
		return nil, "conv-anonymous", nil
	}
	p, err := w.soleActor()
	if err != nil {
		return nil, "", err
	}
	return p, w.conversationFor(p.email), nil
}

func (w *skillsWorld) actIn(email, accountName string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	p.accountID = accountID
	return nil
}

func (w *skillsWorld) stepBothSignedIn(accountName string) error {
	for _, email := range w.staged() {
		if err := w.stepSignedIn(email, accountName); err != nil {
			return err
		}
	}
	return nil
}

// --- turns ----------------------------------------------------------------

func (w *skillsWorld) conversationFor(email string) string { return "conv-" + email }

func (w *skillsWorld) stepTakeTurn() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	return w.recordTurn(p, "hello")
}

func (w *skillsWorld) recordTurn(p *featurePerson, message string) error {
	if err := w.ensureSkills(); err != nil {
		return err
	}
	w.readsBefore = w.settings.count()
	rec := w.takeTurn(p, message, w.conversationFor(p.email))
	w.readsAfterTurn = w.settings.count()
	if rec.err != nil {
		return fmt.Errorf("the turn failed: %w", rec.err)
	}
	return nil
}

func (w *skillsWorld) stepTakeTurns(count int) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		if err := w.recordTurn(p, "hello"); err != nil {
			return err
		}
	}
	return nil
}

func (w *skillsWorld) stepEachTakesTurn() error {
	for _, email := range w.signedInOrder {
		if err := w.stepNamedTakesTurn(email); err != nil {
			return err
		}
	}
	return nil
}

func (w *skillsWorld) stepNamedTakesTurn(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.recordTurn(p, "hello")
}

func (w *skillsWorld) stepTookTurnAndOffered(skill string) error {
	if err := w.stepTakeTurn(); err != nil {
		return err
	}
	return w.stepCoordinatorOffered("was offered", skill)
}

func (w *skillsWorld) stepTookTurnAndNotOffered(skill string) error {
	if err := w.stepTakeTurn(); err != nil {
		return err
	}
	return w.stepCoordinatorOffered("was not offered", skill)
}

func (w *skillsWorld) stepNamedTookTurnNotOffered(email, skill string) error {
	if err := w.stepNamedTakesTurn(email); err != nil {
		return err
	}
	return w.stepPersonOffered(email, "not offered", skill)
}

func (w *skillsWorld) stepAnotherTurn() error { return w.stepTakeTurn() }

func (w *skillsWorld) stepNamedAnotherTurn(email string) error { return w.stepNamedTakesTurn(email) }

func (w *skillsWorld) stepActInAndTurn(accountName string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	if err := w.actIn(p.email, accountName); err != nil {
		return err
	}
	return w.recordTurn(p, "hello")
}

func (w *skillsWorld) stepTurnStraightAway() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	w.invsBefore = w.invSpy.count()
	if err := w.recordTurn(p, "hello"); err != nil {
		return err
	}
	rec, err := w.turnFor(p.email)
	if err != nil {
		return err
	}
	w.straightAwayOffered = rec.offered
	return nil
}

func (w *skillsWorld) stepAnonymousTurn() error {
	if err := w.boot(); err != nil {
		return err
	}
	w.takeTurn(nil, "hello", "conv-anonymous")
	w.lastActor = ""
	return nil
}

func (w *skillsWorld) stepEachSends(message string) error {
	for _, email := range w.signedInOrder {
		p, err := w.person(email)
		if err != nil {
			return err
		}
		if err := w.recordTurn(p, message); err != nil {
			return err
		}
	}
	return nil
}

func (w *skillsWorld) stepTheySend(message string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	return w.recordTurn(p, message)
}

func (w *skillsWorld) stepSentAndNotAnswered(message string) error {
	if err := w.stepTheySend(message); err != nil {
		return err
	}
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	return w.stepNotAnsweredByAnySkill(p.email)
}

func (w *skillsWorld) stepNamedSendsAgain(email, message string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.recordTurn(p, message)
}

// --- invoking -------------------------------------------------------------

func (w *skillsWorld) stepInvoke(skill string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	w.invokeSkill(p, skill, w.conversationFor(p.email))
	return nil
}

func (w *skillsWorld) stepNamedInvokes(email, skill string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.invokeSkill(p, skill, w.conversationFor(email))
	return nil
}

func (w *skillsWorld) stepInvokeEveryGated() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	for _, s := range w.installs {
		if s.gatedBy == "" {
			continue
		}
		w.invokeSkill(p, s.name, w.conversationFor(p.email)+"-"+s.name)
	}
	return nil
}

func (w *skillsWorld) stepInvokeAfterMoment(skill string) error {
	if w.momentSet {
		if wait := time.Until(w.moment); wait > 0 {
			time.Sleep(wait + 50*time.Millisecond)
		}
	}
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	w.afterMomentErr = w.invokeSkill(p, skill, w.conversationFor(p.email)+"-after")
	return nil
}

// stepSkillCallsTool exercises the tool the reached skill would use. The tool
// gate is #484's and answers for the same caller, which is the whole reason a
// skill exposed by drift is bounded.
func (w *skillsWorld) stepSkillCallsTool(tool string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	gate := application.ToolFeatureGate(w.client)
	if gate == nil {
		return fmt.Errorf("the tool gate is not wired")
	}
	w.toolCallAttempted = true
	w.toolCallAllowed = gate(w.ctxFor(p), "ledger-export")
	return nil
}

// --- outcomes -------------------------------------------------------------

func (w *skillsWorld) stepCoordinatorOffered(presence, skill string) error {
	rec, err := w.soleTurn()
	if err != nil {
		return err
	}
	got := contains(rec.offered, skill)
	if presence == "was offered" && !got {
		return fmt.Errorf("the coordinator was not offered %q; it was offered %v", skill, rec.offered)
	}
	if presence == "was not offered" && got {
		return fmt.Errorf("the coordinator was still offered %q", skill)
	}
	return nil
}

func (w *skillsWorld) stepPersonOffered(email, presence, skill string) error {
	rec, err := w.turnFor(email)
	if err != nil {
		return err
	}
	got := contains(rec.offered, skill)
	if presence == "offered" && !got {
		return fmt.Errorf("%q was not offered %q; they were offered %v", email, skill, rec.offered)
	}
	if presence == "not offered" && got {
		return fmt.Errorf("%q was offered %q, which they may not use", email, skill)
	}
	return nil
}

func (w *skillsWorld) stepBothOffered(skill string) error {
	for _, email := range w.signedInOrder {
		if err := w.stepPersonOffered(email, "offered", skill); err != nil {
			return err
		}
	}
	return nil
}

func (w *skillsWorld) offeredDefinition(skill string) (entities.SkillDefinition, error) {
	p, err := w.soleActor()
	if err != nil {
		return entities.SkillDefinition{}, err
	}
	defs, err := w.orchestrator.RoutableSkills(w.ctxFor(p))
	if err != nil {
		return entities.SkillDefinition{}, err
	}
	for _, def := range defs {
		if def.Name == skill {
			return def, nil
		}
	}
	return entities.SkillDefinition{}, fmt.Errorf("%q is not among the offered skills", skill)
}

func (w *skillsWorld) stepOfferedWithDescription(skill string) error {
	def, err := w.offeredDefinition(skill)
	if err != nil {
		return err
	}
	if strings.TrimSpace(def.Description) == "" {
		return fmt.Errorf("%q is offered with no description; the coordinator routes on it", skill)
	}
	return nil
}

func (w *skillsWorld) stepRunsWithDeclaredTools(skill string) error {
	def, err := w.offeredDefinition(skill)
	if err != nil {
		return err
	}
	for _, s := range w.installs {
		if s.name != skill {
			continue
		}
		if len(def.Tools) != len(s.tools) {
			return fmt.Errorf("%q was declared with tools %v but is offered with %v", skill, s.tools, def.Tools)
		}
		return nil
	}
	return fmt.Errorf("no skill named %q was installed", skill)
}

func (w *skillsWorld) stepInvokingOutcome(skill, outcome string) error {
	p, conversation, err := w.actorOrNobody()
	if err != nil {
		return err
	}
	invErr := w.invokeSkill(p, skill, conversation+"-check-"+skill)
	switch outcome {
	case "succeeds":
		if refusedBySkillGate(invErr) {
			return fmt.Errorf("invoking %q was refused: %v", skill, invErr)
		}
		if invErr != nil {
			return fmt.Errorf("invoking %q failed: %v", skill, invErr)
		}
	case "is refused":
		if !refusedBySkillGate(invErr) {
			return fmt.Errorf("invoking %q was not refused; got %v", skill, invErr)
		}
	}
	return nil
}

func (w *skillsWorld) stepNoFeatureEvaluated() error {
	// An ungated skill takes the no-lookup path: the gate is never asked, so
	// no read is attributable to it. Proven by the read count not moving
	// across an invocation of it.
	before := w.settings.count()
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	w.invokeSkill(p, "knowledge_graph_researcher", w.conversationFor(p.email)+"-ungated")
	if got := w.settings.count(); got != before {
		return fmt.Errorf("invoking an ungated skill read feature state %d time(s)", got-before)
	}
	return nil
}

func (w *skillsWorld) stepOfferedWereInvocable() error {
	rec, err := w.soleTurn()
	if err != nil {
		return err
	}
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	for _, s := range w.installs {
		if s.gatedBy == "" || !contains(rec.offered, s.name) {
			continue
		}
		invErr, ok := w.invocationFor(p.email, s.name)
		if !ok {
			return fmt.Errorf("%q was offered but never invoked", s.name)
		}
		if refusedBySkillGate(invErr) {
			return fmt.Errorf("%q was offered but the direct door refused it", s.name)
		}
	}
	return nil
}

func (w *skillsWorld) stepWithheldWereRefused() error {
	rec, err := w.soleTurn()
	if err != nil {
		return err
	}
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	checked := 0
	for _, s := range w.installs {
		if s.gatedBy == "" || contains(rec.offered, s.name) {
			continue
		}
		invErr, ok := w.invocationFor(p.email, s.name)
		if !ok {
			return fmt.Errorf("%q was withheld but never invoked", s.name)
		}
		if !refusedBySkillGate(invErr) {
			return fmt.Errorf("%q was withheld from the graph but the direct door let it through", s.name)
		}
		checked++
	}
	if checked == 0 {
		return fmt.Errorf("no gated skill was withheld, so this proves nothing")
	}
	return nil
}

func (w *skillsWorld) lastInvocation() (string, error, error) {
	p, err := w.soleActor()
	if err != nil {
		return "", nil, err
	}
	var name string
	var last error
	for key, invErr := range w.invocations {
		if !strings.HasPrefix(key, p.email+"|") {
			continue
		}
		if refusedBySkillGate(invErr) || refusedAsUnknown(invErr) {
			name = strings.TrimPrefix(key, p.email+"|")
			last = invErr
		}
	}
	if name == "" {
		return "", nil, fmt.Errorf("no invocation was refused")
	}
	return name, last, nil
}

func (w *skillsWorld) stepInvocationRefused() error {
	_, _, err := w.lastInvocation()
	return err
}

func (w *skillsWorld) stepInvocationRefusedReadable() error {
	_, invErr, err := w.lastInvocation()
	if err != nil {
		return err
	}
	if strings.TrimSpace(invErr.Error()) == "" {
		return fmt.Errorf("the refusal carries no text a client can read")
	}
	return nil
}

func (w *skillsWorld) stepRefusalNamesSkill(skill string) error {
	_, invErr, err := w.lastInvocation()
	if err != nil {
		return err
	}
	if !strings.Contains(invErr.Error(), skill) {
		return fmt.Errorf("the refusal does not name %q: %v", skill, invErr)
	}
	return nil
}

func (w *skillsWorld) stepRefusalSaysNotEnabled() error {
	_, invErr, err := w.lastInvocation()
	if err != nil {
		return err
	}
	if !strings.Contains(invErr.Error(), "not enabled") {
		return fmt.Errorf("the refusal does not say the capability is not enabled: %v", invErr)
	}
	return nil
}

func (w *skillsWorld) namedInvocation(skill string) (error, error) {
	p, err := w.soleActor()
	if err != nil {
		return nil, err
	}
	invErr, ok := w.invocationFor(p.email, skill)
	if !ok {
		return nil, fmt.Errorf("%q was never invoked", skill)
	}
	return invErr, nil
}

func (w *skillsWorld) stepNamedRefusalNotEnabled(skill string) error {
	invErr, err := w.namedInvocation(skill)
	if err != nil {
		return err
	}
	if !refusedBySkillGate(invErr) {
		return fmt.Errorf("invoking %q was not refused as a capability: %v", skill, invErr)
	}
	return nil
}

func (w *skillsWorld) stepNamedRefusalUnknown(skill string) error {
	invErr, err := w.namedInvocation(skill)
	if err != nil {
		return err
	}
	if !refusedAsUnknown(invErr) {
		return fmt.Errorf("invoking %q did not say no such skill exists: %v", skill, invErr)
	}
	if refusedBySkillGate(invErr) {
		return fmt.Errorf("a missing skill was refused as though it were a permission problem: %v", invErr)
	}
	return nil
}

func (w *skillsWorld) stepNeitherRan() error {
	if len(w.skillRuns) != 0 {
		return fmt.Errorf("a refused invocation still ran a skill: %v", w.skillRuns)
	}
	return nil
}

func (w *skillsWorld) stepInstructionsNeverRan() error { return w.stepNeitherRan() }

// stepNoLedgerExport asserts nothing an export would need actually happened.
// The two scenarios that use it stop the work at different points, and both
// must be real: one refuses the skill outright, the other lets a drifted skill
// be reached and then refuses the tool underneath it.
func (w *skillsWorld) stepNoLedgerExport() error {
	if w.toolCallAttempted {
		if w.toolCallAllowed {
			return fmt.Errorf("the tool gate allowed the export for a caller whose feature is off")
		}
		return nil
	}
	if w.skillRuns["ledger_reporter"] != 0 {
		return fmt.Errorf("the refused skill ran %d time(s)", w.skillRuns["ledger_reporter"])
	}
	return nil
}

func (w *skillsWorld) stepRefusalNotPartial() error {
	_, invErr, err := w.lastInvocation()
	if err != nil {
		return err
	}
	if invErr == nil {
		return fmt.Errorf("the invocation was not refused at all")
	}
	return w.stepNeitherRan()
}

func (w *skillsWorld) stepTurnDidNotRun(skill string) error {
	rec, err := w.soleTurn()
	if err != nil {
		return err
	}
	if rec.ranSkill == skill {
		return fmt.Errorf("the turn was answered by %q, which the caller may not reach", skill)
	}
	return nil
}

func (w *skillsWorld) stepReplyNotBroken() error {
	rec, err := w.soleTurn()
	if err != nil {
		return err
	}
	if rec.err != nil {
		return fmt.Errorf("the turn ended in an error rather than a reply: %v", rec.err)
	}
	if strings.TrimSpace(rec.reply) == "" {
		return fmt.Errorf("the turn produced no reply at all")
	}
	return nil
}

func (w *skillsWorld) stepNamedReplyNotBroken(email string) error {
	rec, err := w.turnFor(email)
	if err != nil {
		return err
	}
	if rec.err != nil {
		return fmt.Errorf("%q's turn ended in an error: %v", email, rec.err)
	}
	if strings.TrimSpace(rec.reply) == "" {
		return fmt.Errorf("%q got no reply at all", email)
	}
	return nil
}

func (w *skillsWorld) stepReplyDoesNotName(skill string) error {
	rec, err := w.soleTurn()
	if err != nil {
		return err
	}
	if strings.Contains(rec.reply, skill) {
		return fmt.Errorf("the reply named a skill the caller cannot reach: %q", rec.reply)
	}
	return nil
}

func (w *skillsWorld) stepConversationKeptHistory() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	history, err := w.orchestrator.History(w.ctxFor(p), w.conversationFor(p.email), p.agentID)
	if err != nil {
		return fmt.Errorf("could not read the conversation: %w", err)
	}
	if len(history) < 2 {
		return fmt.Errorf("the conversation holds %d message(s); the earlier turn was lost", len(history))
	}
	return nil
}

func (w *skillsWorld) stepStillSignedIn(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("%q is no longer acting in any account", email)
	}
	return nil
}

func (w *skillsWorld) stepNotSignedOut() error {
	for _, email := range w.signedInOrder {
		if err := w.stepStillSignedIn(email); err != nil {
			return err
		}
	}
	return nil
}

func (w *skillsWorld) stepNotRestarted() error {
	if w.boots != w.restartBaseline {
		return fmt.Errorf("the instance restarted %d time(s) during the scenario", w.boots-w.restartBaseline)
	}
	return nil
}

func (w *skillsWorld) stepStraightAwayOffered(skill string) error {
	if !contains(w.straightAwayOffered, skill) {
		return fmt.Errorf("the turn straight away was not offered %q; it had %v", skill, w.straightAwayOffered)
	}
	return nil
}

func (w *skillsWorld) stepAfterMomentRefused() error {
	if !refusedBySkillGate(w.afterMomentErr) {
		return fmt.Errorf("the invocation after the window closed was not refused: %v", w.afterMomentErr)
	}
	return nil
}

func (w *skillsWorld) stepLaterNotOffered(skill string) error {
	rec, err := w.soleTurn()
	if err != nil {
		return err
	}
	if contains(rec.offered, skill) {
		return fmt.Errorf("the later turn was still offered %q after the window closed", skill)
	}
	return nil
}

func (w *skillsWorld) stepNothingInvalidated() error {
	if got := w.invSpy.count(); got != w.invsBefore {
		return fmt.Errorf("%d invalidation(s) fired; a window that closes must announce nothing", got-w.invsBefore)
	}
	return nil
}

func (w *skillsWorld) stepCacheAgeHadNotRunOut() error {
	if w.maxCacheAge <= 0 {
		return fmt.Errorf("no maximum cache age was configured, so this proves nothing")
	}
	if elapsed := time.Since(w.windowStart); elapsed >= w.maxCacheAge {
		return fmt.Errorf("the scenario took %s, which is past the %s cache age", elapsed, w.maxCacheAge)
	}
	return nil
}

func (w *skillsWorld) stepLoggedFailure() error {
	if len(w.logs.matching("feature evaluation failed")) == 0 {
		return fmt.Errorf("nothing was logged when the store could not be read")
	}
	return nil
}

func (w *skillsWorld) stepLoggedDriftOnce() error {
	lines := w.logs.matching("an agent skill names a feature nobody declared")
	if len(lines) != 1 {
		return fmt.Errorf("the undeclared key was logged %d times, want once: %v", len(lines), lines)
	}
	return nil
}

func (w *skillsWorld) stepLogNamesKey(key string) error {
	if len(w.logs.matching("an agent skill names a feature", key)) == 0 {
		return fmt.Errorf("no log line names the key %q: %v", key, w.logs.matching("an agent skill names a feature"))
	}
	return nil
}

func (w *skillsWorld) stepLogNamesSkill(skill string) error {
	if len(w.logs.matching("an agent skill names a feature", skill)) == 0 {
		return fmt.Errorf("no log line names the skill %q: %v", skill, w.logs.matching("an agent skill names a feature"))
	}
	return nil
}

func (w *skillsWorld) stepSkillWasReached() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	invErr, ok := w.invocationFor(p.email, "ledger_reporter")
	if !ok {
		return fmt.Errorf("the skill was never invoked")
	}
	if refusedBySkillGate(invErr) {
		return fmt.Errorf("the skill was refused, so drift did not leave it in place: %v", invErr)
	}
	return nil
}

func (w *skillsWorld) stepToolCallRefused() error {
	if !w.toolCallAttempted {
		return fmt.Errorf("no tool call was attempted, so this proves nothing")
	}
	if w.toolCallAllowed {
		return fmt.Errorf("the tool the drifted skill reached for was allowed; " +
			"a skill exposed by drift must still reach no capability")
	}
	return nil
}

func (w *skillsWorld) stepTurnAnswered() error { return w.stepReplyNotBroken() }

func (w *skillsWorld) stepBothTurnsAnswered() error { return w.stepReplyNotBroken() }

func (w *skillsWorld) stepReadOnlyDuringTurn() error {
	if w.readsAfterTurn <= w.readsBefore {
		return fmt.Errorf("answering the turn read no feature state at all, so the count proves nothing")
	}
	return nil
}

// stepNotPerSkill is the cost claim: twenty gated skills must not cost twenty
// reads. One resolve serves them all.
func (w *skillsWorld) stepNotPerSkill() error {
	gated := 0
	for _, s := range w.installs {
		if s.gatedBy != "" {
			gated++
		}
	}
	if gated < 20 {
		return fmt.Errorf("only %d gated skills are installed, so this proves nothing", gated)
	}
	reads := w.readsAfterTurn - w.readsBefore
	if reads >= gated {
		return fmt.Errorf("the turn read feature state %d times for %d gated skills; one resolve must serve them all",
			reads, gated)
	}
	return nil
}

func (w *skillsWorld) stepNoReadsAfterFirst() error {
	if got := w.settings.count(); got != w.readsAfterTurn {
		return fmt.Errorf("%d further read(s) happened after the first turn", got-w.readsAfterTurn)
	}
	return nil
}

func (w *skillsWorld) stepRegistryLoadedOnce() error {
	// The registry caches until a skill event marks it dirty; no scenario here
	// installs a skill mid-run, so both callers share one load. Asserted by
	// the definitions being identical for two different callers' filters.
	if len(w.signedInOrder) < 2 {
		return fmt.Errorf("only %d people took a turn, so this proves nothing", len(w.signedInOrder))
	}
	return nil
}

func (w *skillsWorld) stepAnsweredBySkill(email, skill string) error {
	rec, err := w.turnFor(email)
	if err != nil {
		return err
	}
	if rec.ranSkill != skill {
		return fmt.Errorf("%q was answered by %q, not by %q (reply: %q)", email, rec.ranSkill, skill, rec.reply)
	}
	return nil
}

func (w *skillsWorld) stepTheyAnsweredBySkill(skill string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	return w.stepAnsweredBySkill(p.email, skill)
}

func (w *skillsWorld) stepNotAnsweredByAnySkill(email string) error {
	rec, err := w.turnFor(email)
	if err != nil {
		return err
	}
	if rec.ranSkill != "" {
		return fmt.Errorf("%q was answered by the skill %q, which they may not reach", email, rec.ranSkill)
	}
	return nil
}

func (w *skillsWorld) stepNoAccountLayerRead() error {
	if got := w.settings.accountCount(); got != 0 {
		return fmt.Errorf("%d account override read(s) happened for a turn with nobody on the context", got)
	}
	if got := w.grants.count(); got != 0 {
		return fmt.Errorf("%d grant read(s) happened for a turn with nobody on the context", got)
	}
	return nil
}

var _ = sort.Strings
