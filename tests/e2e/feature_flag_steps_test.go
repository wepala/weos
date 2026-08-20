package e2e

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/open-feature/go-sdk/openfeature"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// initFeatureFlagScenario wires the contract's steps to the world. One world
// per scenario, torn down after, so no scenario can see another's state.
func initFeatureFlagScenario(sc *godog.ScenarioContext) {
	w := &featureWorld{
		accounts: map[string]string{},
		people:   map[string]*featurePerson{},
		answers:  map[string][]bool{},
	}
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		w.teardown()
		return ctx, err
	})

	// --- background and declarations ---
	sc.Step(`^a WeOS instance where password sign-in is enabled and requests are authenticated by their session$`,
		w.stepInstance)
	sc.Step(`^the instance is configured with a maximum cache age of (\d+) seconds$`, w.stepMaxCacheAge)
	sc.Step(`^the instance declares these features in code:$`, w.declareFeatures)
	sc.Step(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`, w.stepAccountWithOwner)
	sc.Step(`^a preset "([^"]*)" that declares the feature "([^"]*)", defaulting off$`, w.stepPresetDeclares)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.stepInstallPreset)

	// --- listing ---
	sc.Step(`^the registered features are listed$`, w.stepListFeatures)
	sc.Step(`^the listing includes the feature "([^"]*)"$`, w.stepListingIncludes)
	sc.Step(`^the listing still includes the feature "([^"]*)"$`, w.stepListingIncludes)
	sc.Step(`^that feature reports the display name "([^"]*)"$`, w.stepReportsDisplayName)
	sc.Step(`^that feature reports a description an operator can read$`, w.stepReportsDescription)
	sc.Step(`^that feature reports it defaults on$`, w.stepReportsDefaultOn)
	sc.Step(`^that feature reports an account admin may change it$`, w.stepReportsManageable(true))
	sc.Step(`^that feature reports an account admin may not change it$`, w.stepReportsManageable(false))
	sc.Step(`^that feature reports it may be granted to one person$`, w.stepReportsGrantable(true))
	sc.Step(`^that feature reports it may not be granted to one person$`, w.stepReportsGrantable(false))

	// --- membership ---
	sc.Step(`^"([^"]*)" also belongs to "([^"]*)"$`, w.stepAlsoBelongs)
	sc.Step(`^"([^"]*)" also belongs to "([^"]*)" with the role "([^"]*)"$`, w.stepAlsoBelongsWithRole)
	sc.Step(`^"([^"]*)" and "([^"]*)" both belong to "([^"]*)" with the role "([^"]*)"$`, w.stepTwoBelongWithRole)
	sc.Step(`^"([^"]*)" and "([^"]*)" both hold the role "([^"]*)" in "([^"]*)"$`, w.stepTwoHoldRole)
	sc.Step(`^"([^"]*)" belongs to "([^"]*)" without that role$`, w.stepBelongsWithoutRole)

	// --- staged state ---
	sc.Step(`^nothing has been overridden or granted on this instance$`, w.stepNothingStored)
	sc.Step(`^"([^"]*)" has turned the feature "([^"]*)" (on|off)$`, w.stepAccountTurns)
	sc.Step(`^the operator has turned the feature "([^"]*)" off for the instance$`, w.stepInstanceOff)
	sc.Step(`^the feature "([^"]*)" is off for the instance$`, w.stepInstanceOff)
	sc.Step(`^"([^"]*)" has been granted the feature "([^"]*)"$`, w.stepAgentGranted)
	sc.Step(`^the role "([^"]*)" has been granted the feature "([^"]*)"$`, w.stepRoleGranted)
	sc.Step(`^"([^"]*)" has a stored override turning the feature "([^"]*)" off$`, w.stepStoredAccountOverride)
	sc.Step(`^"([^"]*)" holds a stored grant for the feature "([^"]*)"$`, w.stepStoredGrant)
	sc.Step(`^the store holding account overrides and grants cannot be read$`, w.stepBreakStore)
	sc.Step(`^the store can be read again$`, w.stepFixStore)
	sc.Step(`^nothing is changed$`, func() error { return nil })

	// --- sign-in ---
	sc.Step(`^"([^"]*)" is signed in to "([^"]*)"$`, w.stepSignedInTo)
	sc.Step(`^both of them are signed in$`, w.stepBothSignedIn)
	sc.Step(`^both of them are signed in to "([^"]*)"$`, w.stepBothSignedInTo)
	sc.Step(`^they are signed in on two devices, each with its own session$`, w.stepTwoDevices)
	sc.Step(`^they are signed in and have been answered (on|off) for "([^"]*)"$`, w.stepSignedInAndAnswered)
	sc.Step(`^both of them are signed in and have been answered on for "([^"]*)"$`, w.stepBothSignedInAndAnswered)
	sc.Step(`^both owners are signed in and have been answered on for "([^"]*)"$`, w.stepBothSignedInAndAnswered)
	sc.Step(`^all three of them are signed in and have been answered off for "([^"]*)"$`, w.stepAllSignedInAndAnswered)
	sc.Step(`^both sessions have already evaluated "([^"]*)" and been answered on$`, w.stepBothSessionsAnswered)
	sc.Step(`^they have already evaluated "([^"]*)" and been answered on$`, w.stepAlreadyAnswered)
	sc.Step(`^both of them have already evaluated "([^"]*)" and been answered on$`, w.stepBothAlreadyAnswered)
	sc.Step(`^they evaluated "([^"]*)" with default on and were answered off$`, w.stepEvaluatedAndAnsweredOff)

	// --- changes mid-session ---
	sc.Step(`^the grant is taken away from "([^"]*)"$`, w.stepRevokeAgent)
	sc.Step(`^the grant is taken away from the role "([^"]*)"$`, w.stepRevokeRole)
	sc.Step(`^the role "([^"]*)" is granted the feature "([^"]*)"$`, w.stepRoleGranted)
	sc.Step(`^"([^"]*)" turns the feature "([^"]*)" off$`, w.stepAccountTurnsOff)
	sc.Step(`^"([^"]*)" turns the feature "([^"]*)" off and no invalidation reaches the session$`,
		w.stepAccountTurnsOffSilently)
	sc.Step(`^the operator turns the feature "([^"]*)" off for the instance$`, w.stepOperatorTurnsInstanceOff)

	// --- evaluation ---
	sc.Step(`^they evaluate "([^"]*)" with default (on|off)$`, w.stepTheyEvaluate)
	sc.Step(`^"([^"]*)" evaluates "([^"]*)" with default (on|off)$`, w.stepPersonEvaluates)
	sc.Step(`^"([^"]*)" evaluates "([^"]*)" with default (on|off) again$`, w.stepPersonEvaluates)
	sc.Step(`^each of them evaluates "([^"]*)" with default (on|off)$`, w.stepEachEvaluates)
	sc.Step(`^a caller with no identity on the context evaluates "([^"]*)" with default (on|off)$`,
		w.stepAnonymousEvaluates)
	sc.Step(`^"([^"]*)" signs in and evaluates "([^"]*)" with default (on|off)$`, w.stepSignsInAndEvaluates)
	sc.Step(`^they evaluate "([^"]*)" (\d+) times in that session$`, w.stepEvaluateNTimes)
	sc.Step(`^they evaluate "([^"]*)" and then "([^"]*)" in the same session$`, w.stepEvaluateTwoKeys)
	sc.Step(`^they evaluate "([^"]*)" again in the same session$`, w.stepEvaluateAgain)
	sc.Step(`^they evaluate "([^"]*)" with default on in the same session$`, w.stepEvaluateAgainDefaultOn)
	sc.Step(`^they evaluate "([^"]*)" again on each device$`, w.stepEvaluateOnEachDevice)
	sc.Step(`^they evaluate "([^"]*)" again straight away$`, w.stepEvaluateStraightAway)
	sc.Step(`^they evaluate "([^"]*)" again after the maximum cache age has passed$`, w.stepEvaluateAfterMaxAge)
	sc.Step(`^each of them evaluates "([^"]*)" again in the session they already held$`, w.stepEachEvaluatesAgain)
	sc.Step(`^they evaluate the undeclared feature "([^"]*)" with default (on|off)$`, w.stepTheyEvaluate)
	sc.Step(`^they evaluate the undeclared feature "([^"]*)" (\d+) times with default off$`, w.stepEvaluateUndeclaredN)
	sc.Step(`^they evaluate the flag key "([^"]*)" with default (on|off)$`, w.stepEvaluateRawKey)
	sc.Step(`^they evaluate "([^"]*)" as (a string|an integer|a number|an object) with default "?([^"]*?)"?$`,
		w.stepEvaluateNonBoolean)

	// --- assertions ---
	sc.Step(`^the feature answers (on|off)$`, w.stepFeatureAnswers)
	sc.Step(`^the flag answers (on|off)$`, w.stepFeatureAnswers)
	sc.Step(`^the feature answers on every time$`, w.stepAnsweredOnEveryTime)
	sc.Step(`^"([^"]*)" is answered (on|off)$`, w.stepPersonAnswered)
	sc.Step(`^"([^"]*)" was answered (on|off)$`, w.stepPersonAnswered)
	sc.Step(`^"([^"]*)" was answered on both times$`, w.stepAnsweredOnBothTimes)
	sc.Step(`^both of them are answered off$`, w.stepBothAnsweredOff)
	sc.Step(`^both sessions are answered off$`, w.stepFeatureAnswersOffOnly)
	sc.Step(`^the evaluation reason is "([^"]*)"$`, w.stepReasonIs)
	sc.Step(`^the answer is the (.+) they supplied$`, w.stepNonBooleanAnswer)
	sc.Step(`^the evaluation straight away is answered on$`, w.stepStraightAwayOn)
	sc.Step(`^the evaluation after the maximum cache age is answered off$`, w.stepAfterMaxAgeOff)

	// --- read counting ---
	sc.Step(`^feature state was read from the database only while the first evaluation was answered$`,
		w.stepReadOnce)
	sc.Step(`^feature state was read from the database once more, to re-resolve the set$`, w.stepReadOnceMore)
	sc.Step(`^no account override or grant was read for that evaluation$`, w.stepNoAccountOrGrantRead)
	sc.Step(`^"([^"]*)" read no feature state from the database to be answered$`, w.stepPersonReadNothing)

	// --- sign-out assertions ---
	sc.Step(`^they are still signed in$`, w.stepStillSignedIn)
	sc.Step(`^neither session was signed out$`, w.stepAllStillSignedIn)
	sc.Step(`^none of them was signed out$`, w.stepAllStillSignedIn)
	sc.Step(`^they were not signed out by the failure$`, w.stepStillSignedIn)
	sc.Step(`^they were signed out by neither evaluation$`, w.stepStillSignedIn)
	sc.Step(`^the request they make next is served$`, w.stepNextRequestServed)

	// --- logging ---
	sc.Step(`^the instance logged the undeclared feature key once$`, w.stepLoggedUndeclaredOnce)
	sc.Step(`^the log names the key "([^"]*)"$`, w.stepLogNamesKey)
	sc.Step(`^the instance logged the failure$`, w.stepLoggedFailure)
}

// --- background ------------------------------------------------------------

func (w *featureWorld) stepInstance() error { return w.boot() }

func (w *featureWorld) stepMaxCacheAge(seconds int) error {
	w.maxCacheAge = time.Duration(seconds) * time.Second
	// The resolver reads its maximum age at construction, and the Background
	// has already booted, so the app is restarted against the same database.
	// Accounts, people and sessions persist; the registry is replayed.
	return w.reboot()
}

func (w *featureWorld) stepAccountWithOwner(name, email, password string) error {
	return w.accountWithOwner(name, email, password)
}

func (w *featureWorld) stepPresetDeclares(presetName, featureKey string) error {
	if err := w.boot(); err != nil {
		return err
	}
	w.pendingPreset = presetName
	w.pendingFeature = entities.FeatureMeta{
		Key: featureKey, DisplayName: featureKey, Default: false,
	}
	return nil
}

func (w *featureWorld) stepInstallPreset(presetName string) error {
	if presetName != w.pendingPreset {
		return fmt.Errorf("no preset named %q was staged", presetName)
	}
	// The preset registry is the live source of preset declarations, so
	// registering the preset IS installing its features — no persistence, no
	// hook in InstallPreset.
	return w.presetRegistry().Add(application.PresetDefinition{
		Name:     w.pendingPreset,
		Features: map[string]entities.FeatureMeta{w.pendingFeature.Key: w.pendingFeature},
	})
}

// --- listing ---------------------------------------------------------------

func (w *featureWorld) stepListFeatures() error {
	w.listing = w.service.List()
	return nil
}

func (w *featureWorld) stepListingIncludes(key string) error {
	for i := range w.listing {
		if w.listing[i].Key == key {
			w.focused = &w.listing[i]
			return nil
		}
	}
	return fmt.Errorf("the listing does not include %q", key)
}

func (w *featureWorld) stepReportsDisplayName(want string) error {
	if w.focused == nil {
		return fmt.Errorf("no feature is in focus")
	}
	if w.focused.DisplayName != want {
		return fmt.Errorf("display name is %q, want %q", w.focused.DisplayName, want)
	}
	return nil
}

func (w *featureWorld) stepReportsDescription() error {
	if w.focused == nil || strings.TrimSpace(w.focused.Description) == "" {
		return fmt.Errorf("the feature reports no description")
	}
	return nil
}

func (w *featureWorld) stepReportsDefaultOn() error {
	if w.focused == nil || !w.focused.Default {
		return fmt.Errorf("the feature does not default on")
	}
	return nil
}

func (w *featureWorld) stepReportsManageable(want bool) func() error {
	return func() error {
		if w.focused == nil {
			return fmt.Errorf("no feature is in focus")
		}
		if w.focused.Manageable != want {
			return fmt.Errorf("manageable is %v, want %v", w.focused.Manageable, want)
		}
		return nil
	}
}

func (w *featureWorld) stepReportsGrantable(want bool) func() error {
	return func() error {
		if w.focused == nil {
			return fmt.Errorf("no feature is in focus")
		}
		if w.focused.Grantable != want {
			return fmt.Errorf("grantable is %v, want %v", w.focused.Grantable, want)
		}
		return nil
	}
}

// --- membership ------------------------------------------------------------

func (w *featureWorld) stepAlsoBelongs(email, accountName string) error {
	w.group = append(w.group, email)
	return w.alsoBelongsTo(email, accountName, "")
}

func (w *featureWorld) stepAlsoBelongsWithRole(email, accountName, role string) error {
	w.group = append(w.group, email)
	return w.alsoBelongsTo(email, accountName, role)
}

func (w *featureWorld) stepTwoBelongWithRole(a, b, accountName, role string) error {
	w.group = append(w.group, a, b)
	if err := w.alsoBelongsTo(a, accountName, role); err != nil {
		return err
	}
	return w.alsoBelongsTo(b, accountName, role)
}

func (w *featureWorld) stepTwoHoldRole(a, b, role, accountName string) error {
	return w.stepTwoBelongWithRole(a, b, accountName, role)
}

func (w *featureWorld) stepBelongsWithoutRole(email, accountName string) error {
	w.group = append(w.group, email)
	return w.alsoBelongsTo(email, accountName, "")
}

// --- staged state ----------------------------------------------------------

func (w *featureWorld) stepNothingStored() error { return nil }

func (w *featureWorld) stepAccountTurns(accountName, key, onOff string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	return w.service.SetAccountFeature(context.Background(), id, key, onOff == "on")
}

func (w *featureWorld) stepAccountTurnsOff(accountName, key string) error {
	return w.stepAccountTurns(accountName, key, "off")
}

// stepAccountTurnsOffSilently writes the store directly, bypassing the service
// and therefore the invalidation. That is how the contract stages "a change
// that no invalidation announced" — the write/invalidate seam is the harness
// seam.
func (w *featureWorld) stepAccountTurnsOffSilently(accountName, key string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	return w.settings.SetOverride(context.Background(), repositories.FeatureScopeAccount, id, key, false)
}

func (w *featureWorld) stepInstanceOff(key string) error {
	return w.service.SetInstanceFeature(context.Background(), key, false)
}

func (w *featureWorld) stepOperatorTurnsInstanceOff(key string) error {
	return w.stepInstanceOff(key)
}

func (w *featureWorld) stepAgentGranted(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.grantedKey = key
	return w.service.GrantToAgent(context.Background(), p.accountID, p.agentID, key,
		application.GrantTerms{})
}

func (w *featureWorld) stepRoleGranted(role, key string) error {
	accountID, err := w.soleAccount()
	if err != nil {
		return err
	}
	w.grantedKey = key
	return w.service.GrantToRole(context.Background(), accountID, role, key,
		application.GrantTerms{})
}

func (w *featureWorld) stepRevokeAgent(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	key, err := w.lastGrantedKey()
	if err != nil {
		return err
	}
	_, err = w.service.RevokeFromAgent(context.Background(), p.accountID, p.agentID, key)
	return err
}

func (w *featureWorld) stepRevokeRole(role string) error {
	accountID, err := w.soleAccount()
	if err != nil {
		return err
	}
	key, err := w.lastGrantedKey()
	if err != nil {
		return err
	}
	_, err = w.service.RevokeFromRole(context.Background(), accountID, role, key)
	return err
}

// stepStoredAccountOverride and stepStoredGrant write the store directly,
// bypassing the service's eligibility refusal, because the contract stages rows
// the service would never write and asserts resolution ignores them anyway.
func (w *featureWorld) stepStoredAccountOverride(accountName, key string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	return w.settings.SetOverride(context.Background(), repositories.FeatureScopeAccount, id, key, false)
}

func (w *featureWorld) stepStoredGrant(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.grants.Grant(context.Background(), entities.FeatureGrantRecord{
		SubjectType: repositories.FeatureSubjectAgent,
		SubjectID:   p.agentID,
		AccountID:   p.accountID,
		FeatureKey:  key,
	})
}

func (w *featureWorld) stepBreakStore() error {
	w.settings.setBroken(true)
	w.grants.setBroken(true)
	return nil
}

func (w *featureWorld) stepFixStore() error {
	w.settings.setBroken(false)
	w.grants.setBroken(false)
	return nil
}

// --- sign-in ---------------------------------------------------------------

func (w *featureWorld) stepSignedInTo(email, accountName string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	p, err := w.person(email)
	if err != nil {
		return err
	}
	p.accountID = id
	return w.signIn(p)
}

func (w *featureWorld) stepBothSignedIn() error {
	for _, email := range w.cohort() {
		if err := w.signIn(w.people[email]); err != nil {
			return err
		}
	}
	return nil
}

func (w *featureWorld) stepBothSignedInTo(accountName string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	for _, email := range w.cohort() {
		p := w.people[email]
		p.accountID = id
		if err := w.signIn(p); err != nil {
			return err
		}
	}
	return nil
}

func (w *featureWorld) stepTwoDevices() error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	return w.signIn(p)
}

func (w *featureWorld) stepSignedInAndAnswered(onOff, key string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	got := w.evaluate(p, key, onOff == "off")
	if got != (onOff == "on") {
		return fmt.Errorf("%q was answered %v, want %s", p.email, got, onOff)
	}
	// Staging, not the assertion: clear the read counters so a later
	// "read no feature state" step measures the evaluation under test.
	w.settings.reset()
	w.grants.reset()
	return nil
}

func (w *featureWorld) stepBothSignedInAndAnswered(key string) error {
	for _, email := range w.cohort() {
		p := w.people[email]
		if err := w.signIn(p); err != nil {
			return err
		}
		if got := w.evaluate(p, key, false); !got {
			return fmt.Errorf("%q was answered off, want on", email)
		}
	}
	// Staging, not the assertion: clear the read counters so a later
	// "read no feature state" step measures the evaluation under test.
	w.settings.reset()
	w.grants.reset()
	return nil
}

func (w *featureWorld) stepAllSignedInAndAnswered(key string) error {
	for _, email := range w.cohort() {
		p := w.people[email]
		if err := w.signIn(p); err != nil {
			return err
		}
		if got := w.evaluate(p, key, false); got {
			return fmt.Errorf("%q was answered on, want off", email)
		}
	}
	// Staging, not the assertion: clear the read counters so a later
	// "read no feature state" step measures the evaluation under test.
	w.settings.reset()
	w.grants.reset()
	return nil
}

func (w *featureWorld) stepBothSessionsAnswered(key string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	for range p.sessionIDs {
		if got := w.evaluate(p, key, false); !got {
			return fmt.Errorf("a session was answered off, want on")
		}
	}
	// Staging, not the assertion: clear the read counters so a later
	// "read no feature state" step measures the evaluation under test.
	w.settings.reset()
	w.grants.reset()
	return nil
}

func (w *featureWorld) stepAlreadyAnswered(key string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	if got := w.evaluate(p, key, false); !got {
		return fmt.Errorf("%q was answered off, want on", p.email)
	}
	w.settings.reset()
	w.grants.reset()
	return nil
}

func (w *featureWorld) stepBothAlreadyAnswered(key string) error {
	for _, email := range w.cohort() {
		if got := w.evaluate(w.people[email], key, false); !got {
			return fmt.Errorf("%q was answered off, want on", email)
		}
	}
	w.settings.reset()
	w.grants.reset()
	return nil
}

func (w *featureWorld) stepEvaluatedAndAnsweredOff(key string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	if got := w.evaluate(p, key, true); got {
		return fmt.Errorf("%q was answered on while the store was unreadable", p.email)
	}
	return nil
}

// --- evaluation ------------------------------------------------------------

func (w *featureWorld) stepTheyEvaluate(key, def string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	w.evaluate(p, key, boolWord(def))
	return nil
}

func (w *featureWorld) stepPersonEvaluates(email, key, def string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.current = p
	w.evaluate(p, key, boolWord(def))
	return nil
}

func (w *featureWorld) stepEachEvaluates(key, def string) error {
	for _, email := range w.cohort() {
		w.evaluate(w.people[email], key, boolWord(def))
	}
	return nil
}

func (w *featureWorld) stepEachEvaluatesAgain(key string) error {
	for _, email := range w.cohort() {
		w.evaluate(w.people[email], key, false)
	}
	return nil
}

func (w *featureWorld) stepAnonymousEvaluates(key, def string) error {
	w.settings.reset()
	w.grants.reset()
	w.evaluate(nil, key, boolWord(def))
	return nil
}

func (w *featureWorld) stepSignsInAndEvaluates(email, key, def string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	w.evaluate(p, key, boolWord(def))
	return nil
}

func (w *featureWorld) stepEvaluateNTimes(key string, n int) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	w.settings.reset()
	w.grants.reset()
	for i := 0; i < n; i++ {
		w.evaluate(p, key, false)
	}
	return nil
}

func (w *featureWorld) stepEvaluateTwoKeys(first, second string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	w.settings.reset()
	w.grants.reset()
	w.evaluate(p, first, false)
	w.evaluate(p, second, false)
	return nil
}

func (w *featureWorld) stepEvaluateAgain(key string) error {
	return w.stepTheyEvaluate(key, "off")
}

func (w *featureWorld) stepEvaluateAgainDefaultOn(key string) error {
	return w.stepTheyEvaluate(key, "on")
}

func (w *featureWorld) stepEvaluateOnEachDevice(key string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	w.answers[p.email] = nil
	for range p.sessionIDs {
		w.evaluate(p, key, false)
	}
	return nil
}

func (w *featureWorld) stepEvaluateStraightAway(key string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	w.straightAway = w.evaluate(p, key, false)
	return nil
}

// stepEvaluateAfterMaxAge waits out the configured age. The contract sets a
// short one for exactly this reason; production leaves it long.
func (w *featureWorld) stepEvaluateAfterMaxAge(key string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	time.Sleep(w.maxCacheAge + 250*time.Millisecond)
	w.settings.reset()
	w.grants.reset()
	w.afterMaxAge = w.evaluate(p, key, false)
	return nil
}

func (w *featureWorld) stepEvaluateUndeclaredN(key string, n int) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		w.evaluate(p, key, false)
	}
	return nil
}

func (w *featureWorld) stepEvaluateRawKey(flag, def string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	w.evaluateRawKey(p, flag, boolWord(def))
	return nil
}

func (w *featureWorld) stepEvaluateNonBoolean(key, kind, supplied string) error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	ctx := w.ctxFor(p)
	flag := application.FeatureFlagPrefix + key
	switch kind {
	case "a string":
		d, _ := w.client.StringValueDetails(ctx, flag, supplied, openfeature.EvaluationContext{})
		w.lastAny, w.lastRsn = d.Value, d.Reason
	case "an integer":
		var n int64
		_, _ = fmt.Sscanf(supplied, "%d", &n)
		d, _ := w.client.IntValueDetails(ctx, flag, n, openfeature.EvaluationContext{})
		w.lastAny, w.lastRsn = d.Value, d.Reason
	case "a number":
		var f float64
		_, _ = fmt.Sscanf(supplied, "%f", &f)
		d, _ := w.client.FloatValueDetails(ctx, flag, f, openfeature.EvaluationContext{})
		w.lastAny, w.lastRsn = d.Value, d.Reason
	case "an object":
		d, _ := w.client.ObjectValueDetails(ctx, flag, supplied, openfeature.EvaluationContext{})
		w.lastAny, w.lastRsn = d.Value, d.Reason
	default:
		return fmt.Errorf("unknown evaluation kind %q", kind)
	}
	w.suppliedNonBool = supplied
	return nil
}

// --- assertions ------------------------------------------------------------

func (w *featureWorld) stepFeatureAnswers(onOff string) error {
	if w.lastBool == nil {
		return fmt.Errorf("nothing has been evaluated")
	}
	if *w.lastBool != boolWord(onOff) {
		return fmt.Errorf("the feature answered %v, want %s", *w.lastBool, onOff)
	}
	return nil
}

func (w *featureWorld) stepFeatureAnswersOffOnly() error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	answers := w.answers[p.email]
	if len(answers) == 0 {
		return fmt.Errorf("%q was never answered, so there is nothing to assert — "+
			"an earlier step evaluated as the wrong person", p.email)
	}
	for i, got := range answers {
		if got {
			return fmt.Errorf("session %d was answered on, want off", i+1)
		}
	}
	return nil
}

func (w *featureWorld) stepAnsweredOnEveryTime() error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	answers := w.answers[p.email]
	if len(answers) == 0 {
		return fmt.Errorf("%q was never answered, so there is nothing to assert — "+
			"an earlier step evaluated as the wrong person", p.email)
	}
	for i, got := range answers {
		if !got {
			return fmt.Errorf("evaluation %d answered off, want on", i+1)
		}
	}
	return nil
}

func (w *featureWorld) stepPersonAnswered(email, onOff string) error {
	got, ok := w.lastAnswerFor(email)
	if !ok {
		return fmt.Errorf("%q has not evaluated anything", email)
	}
	if got != boolWord(onOff) {
		return fmt.Errorf("%q was answered %v, want %s", email, got, onOff)
	}
	return nil
}

func (w *featureWorld) stepAnsweredOnBothTimes(email string) error {
	answers := w.answers[email]
	if len(answers) < 2 {
		return fmt.Errorf("%q evaluated %d times, want at least 2", email, len(answers))
	}
	for i, got := range answers {
		if !got {
			return fmt.Errorf("%q was answered off on evaluation %d", email, i+1)
		}
	}
	return nil
}

func (w *featureWorld) stepBothAnsweredOff() error {
	for _, email := range w.cohort() {
		got, ok := w.lastAnswerFor(email)
		if !ok {
			return fmt.Errorf("%q has not evaluated anything", email)
		}
		if got {
			return fmt.Errorf("%q was answered on, want off", email)
		}
	}
	return nil
}

func (w *featureWorld) stepReasonIs(want string) error {
	if string(w.lastRsn) != want {
		return fmt.Errorf("reason was %q, want %q", w.lastRsn, want)
	}
	return nil
}

func (w *featureWorld) stepNonBooleanAnswer(_ string) error {
	if w.lastRsn != openfeature.DefaultReason {
		return fmt.Errorf("reason was %q, want DEFAULT", w.lastRsn)
	}
	if fmt.Sprint(w.lastAny) != w.suppliedNonBool &&
		fmt.Sprintf("%v", w.lastAny) != w.suppliedNonBool {
		return fmt.Errorf("the answer was %v, want the supplied %q", w.lastAny, w.suppliedNonBool)
	}
	return nil
}

func (w *featureWorld) stepStraightAwayOn() error {
	if !w.straightAway {
		return fmt.Errorf("the evaluation straight away answered off, want on")
	}
	return nil
}

func (w *featureWorld) stepAfterMaxAgeOff() error {
	if w.afterMaxAge {
		return fmt.Errorf("the evaluation after the maximum cache age answered on, want off")
	}
	return nil
}

// --- read counting ---------------------------------------------------------

func (w *featureWorld) stepReadOnce() error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	instance, account := w.settings.countsFor(p.agentID)
	if instance != 1 {
		return fmt.Errorf("the instance layer was read %d times for %q, want 1", instance, p.email)
	}
	if account > 1 {
		return fmt.Errorf("the account layer was read %d times for %q, want at most 1", account, p.email)
	}
	if grants := w.grants.countFor(p.agentID); grants > 1 {
		return fmt.Errorf("grants were read %d times for %q, want at most 1", grants, p.email)
	}
	return nil
}

func (w *featureWorld) stepReadOnceMore() error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	instance, _ := w.settings.countsFor(p.agentID)
	if instance != 1 {
		return fmt.Errorf("the set was re-resolved with %d instance reads, want exactly 1", instance)
	}
	return nil
}

// stepNoAccountOrGrantRead measures the anonymous caller specifically — the
// empty agent id — so another person resolving in the same scenario cannot mask
// the assertion.
func (w *featureWorld) stepNoAccountOrGrantRead() error {
	_, account := w.settings.countsFor("")
	if account != 0 {
		return fmt.Errorf("the anonymous caller read the account layer %d times, want 0", account)
	}
	if grants := w.grants.countFor(""); grants != 0 {
		return fmt.Errorf("the anonymous caller read grants %d times, want 0", grants)
	}
	return nil
}

// stepPersonReadNothing proves this person's answer came from memory. Counted
// per agent: somebody else re-resolving in the same scenario is exactly what
// the isolation scenario stages, and a global count would be masked by it.
func (w *featureWorld) stepPersonReadNothing(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	instance, account := w.settings.countsFor(p.agentID)
	if instance != 0 || account != 0 {
		return fmt.Errorf("%q read feature state %d/%d times, want none — their session "+
			"was not invalidated, so the answer had to come from memory", email, instance, account)
	}
	return nil
}

// --- sign-out --------------------------------------------------------------

func (w *featureWorld) stepStillSignedIn() error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	return w.assertSessionsValid(p)
}

func (w *featureWorld) stepAllStillSignedIn() error {
	for _, email := range w.cohort() {
		if err := w.assertSessionsValid(w.people[email]); err != nil {
			return err
		}
	}
	return nil
}

func (w *featureWorld) assertSessionsValid(p *featurePerson) error {
	for i, id := range p.sessionIDs {
		info, err := w.authService.ValidateSession(context.Background(), id)
		if err != nil || info == nil {
			return fmt.Errorf("session %d of %q is no longer valid: %w — "+
				"clearing a resolved feature set must never sign anyone out", i+1, p.email, err)
		}
	}
	return nil
}

func (w *featureWorld) stepNextRequestServed() error {
	p, err := w.actor()
	if err != nil {
		return err
	}
	if err := w.assertSessionsValid(p); err != nil {
		return err
	}
	// A further evaluation must still be answered rather than refused.
	w.evaluate(p, "episodic-recall", false)
	return nil
}

// --- logging ---------------------------------------------------------------

func (w *featureWorld) stepLoggedUndeclaredOnce() error {
	lines := w.logs.matching("nobody declared")
	if len(lines) != 1 {
		return fmt.Errorf("the undeclared key was logged %d times, want exactly 1", len(lines))
	}
	return nil
}

func (w *featureWorld) stepLogNamesKey(key string) error {
	if len(w.logs.matching("nobody declared", key)) == 0 {
		return fmt.Errorf("no log line names the key %q", key)
	}
	return nil
}

func (w *featureWorld) stepLoggedFailure() error {
	if len(w.logs.matching("feature evaluation failed")) == 0 {
		return fmt.Errorf("the store failure was not logged")
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func (w *featureWorld) lastAnswerFor(email string) (bool, bool) {
	answers := w.answers[email]
	if len(answers) == 0 {
		return false, false
	}
	return answers[len(answers)-1], true
}

// soleAccount is for steps that name a role but no account. Refusing when more
// than one account exists is deliberate: picking one at random from a map would
// misfire silently the day a role-grant scenario stages a second account.
func (w *featureWorld) soleAccount() (string, error) {
	switch len(w.accounts) {
	case 0:
		return "", fmt.Errorf("no account has been staged")
	case 1:
		for _, id := range w.accounts {
			return id, nil
		}
	}
	names := make([]string, 0, len(w.accounts))
	for name := range w.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("this step names a role but not an account, and %d are staged (%s); "+
		"the step wording needs to say which", len(w.accounts), strings.Join(names, ", "))
}

func (w *featureWorld) lastGrantedKey() (string, error) {
	if w.grantedKey == "" {
		return "", fmt.Errorf("no grant has been staged to take away")
	}
	return w.grantedKey, nil
}
