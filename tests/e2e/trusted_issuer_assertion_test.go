package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/cucumber/godog"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// TestTrustedIssuerAssertion runs the acceptance scenarios for story
// wm-63gg0.1 (epic wm-63gg0): POST /api/auth/assert verifies a login
// assertion signed by the one issuer an instance trusts, and refuses every
// assertion it cannot trust with the reason why.
//
// The scenarios drive real HTTP against the real application, mounted through
// handlers.MountTrustedIssuerAssertion — the call serve.go makes — with the
// handler built by handlers.NewTrustedIssuerAssertionHandler, the constructor
// serve.go calls. Two things are the test's own: the door, an httptest server
// that publishes a key list and counts how often it is read; and the
// instance's clock, which the verifier reads through the constructor's Now so
// "a minute later" takes no minute. Everything
// the instance writes through its logger is captured, so the scenarios can
// read the log.
func TestTrustedIssuerAssertion(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "trusted-issuer-assertion",
		ScenarioInitializer: initTrustedIssuerScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/trusted_issuer_assertion.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("trusted issuer assertion acceptance scenarios failed")
	}
}

const (
	tiAssertPath = "/api/auth/assert"
	// tiUnnamedPerson signs assertions whose scenario names nobody.
	tiUnnamedPerson = "someone@harborlegal.example"
)

// tiContractReasons is every reason the contract lets a refusal name.
var tiContractReasons = map[string]bool{
	"signature": true, "kid-miss": true, "keys-unreachable": true, "iss": true, "aud": true,
	"expired": true, "window": true, "jti-replay": true, "claims": true,
}

func initTrustedIssuerScenario(sc *godog.ScenarioContext) {
	w := &tiWorld{clock: &tiClock{}, logs: &tiLogCapture{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	// The instance
	sc.Step(`^a WeOS instance that trusts login assertions from "([^"]*)" for the audience "([^"]*)"$`, w.instanceTrusting)
	sc.Step(`^a WeOS instance with no trusted issuer configured$`, w.instanceWithNoIssuer)
	sc.Step(`^a WeOS instance configured with a trusted issuer and its key list but no audience$`, w.instanceMissingAudience)
	sc.Step(`^the instance starts$`, w.boot)

	// The issuer's key list
	sc.Step(`^the issuer publishes only the signing key "([^"]*)"$`, w.publishOnly)
	sc.Step(`^the instance has the issuer's key "([^"]*)" in hand$`, w.keyInHand)
	sc.Step(`^the issuer now also publishes the signing key "([^"]*)"$`, w.alsoPublish)
	sc.Step(`^the issuer has since stopped publishing any key at all$`, func() error {
		w.door.setMode(tiPublishesNothing)
		return nil
	})
	sc.Step(`^the issuer's key list has since become unreachable$`, func() error {
		w.door.setMode(tiUnreachable)
		return nil
	})

	// Assertions presented
	sc.Step(`^the door presents an assertion for "([^"]*)" signed with "([^"]*)"$`, w.presentSignedWith)
	sc.Step(`^the door then presents an assertion for "([^"]*)" signed with "([^"]*)"$`, w.presentSignedWith)
	sc.Step(`^the door presents an assertion signed with the unpublished key "([^"]*)"$`, func(kid string) error {
		return w.presentSignedWith(tiUnnamedPerson, kid)
	})
	sc.Step(`^the door presents an assertion for "([^"]*)" that lives (\d+) seconds$`, w.presentLiving)
	sc.Step(`^the door presents an assertion for "([^"]*)" that expired (\d+) seconds ago$`, w.presentExpiredAgo)
	sc.Step(`^the door presents an assertion for "([^"]*)" issued (\d+) seconds in the future$`, w.presentIssuedAhead)
	sc.Step(`^the door presents an assertion for "([^"]*)" signed with a key the issuer has never published$`, w.presentSignedWithRogueKey)
	sc.Step(`^the door presents an assertion for "([^"]*)" naming "([^"]*)" as its issuer$`, w.presentNamingIssuer)
	sc.Step(`^the door presents an assertion for "([^"]*)" naming the audience "([^"]*)"$`, w.presentNamingAudience)
	sc.Step(`^the door presents an assertion for "([^"]*)" minted to live ten minutes$`, func(email string) error {
		return w.presentLiving(email, 600)
	})
	sc.Step(`^the door presents an assertion for "([^"]*)" whose email address is not marked verified$`, w.presentUnverifiedEmail)
	sc.Step(`^the door presents an assertion for "([^"]*)" carrying no subject claim$`, w.presentWithoutSubject)
	sc.Step(`^the door presents an assertion for "([^"]*)" naming the sign-in provider "([^"]*)"$`, w.presentNamingProvider)
	sc.Step(`^the door presents an assertion for "([^"]*)" signed with HMAC over the issuer's published public key$`, w.presentHMACOverPublicKey)
	sc.Step(`^the door presents an assertion for "([^"]*)" carrying no signature at all$`, w.presentUnsigned)
	sc.Step(`^the door presents a freshly signed assertion for "([^"]*)"$`, w.presentFresh)
	sc.Step(`^the door presents a second assertion for "([^"]*)" a minute later$`, func(email string) error {
		w.clock.Advance(time.Minute)
		return w.presentFresh(email)
	})

	// Requests without a usable assertion
	sc.Step(`^someone sends a sign-in request carrying the assertion "([^"]*)"$`, func(value string) error {
		return w.sendSignIn(tiAssertionBody(value))
	})
	sc.Step(`^someone sends a sign-in request carrying an empty assertion$`, func() error {
		return w.sendSignIn(tiAssertionBody(""))
	})
	sc.Step(`^someone sends a sign-in request with no assertion in it at all$`, func() error {
		return w.sendSignIn(`{}`)
	})

	// One assertion, presented more than once
	sc.Step(`^the door has signed one assertion for "([^"]*)"$`, w.signOneAssertion)
	sc.Step(`^the door presents that assertion$`, w.presentHeld)
	sc.Step(`^the door presents the same assertion again$`, w.presentHeld)
	sc.Step(`^two sign-ins present that assertion at the same moment$`, w.raceHeld)
	sc.Step(`^"([^"]*)" signed in with an assertion a moment ago$`, w.signedInAMomentAgo)

	// An instance with no assertion path
	sc.Step(`^someone presents an assertion to that instance$`, w.presentToUnconfiguredInstance)
	sc.Step(`^someone posts the same details to "([^"]*)", an endpoint this instance has never had$`, w.postSameDetailsTo)

	// Outcomes
	sc.Step(`^the sign-in succeeds$`, func() error { return w.signInAt(-1).succeeded() })
	sc.Step(`^the sign-in is refused$`, func() error { return w.signInAt(-1).refused() })
	sc.Step(`^the first sign-in succeeds$`, func() error { return w.signInAt(0).succeeded() })
	sc.Step(`^the first sign-in is refused$`, func() error { return w.signInAt(0).refused() })
	sc.Step(`^the second sign-in succeeds$`, func() error { return w.signInAt(1).succeeded() })
	sc.Step(`^the second sign-in is refused$`, func() error { return w.signInAt(1).refused() })
	sc.Step(`^both sign-ins succeed$`, w.lastTwoSucceed)
	sc.Step(`^exactly one of the two sign-ins succeeds$`, w.exactlyOneOfTwoSucceeds)
	sc.Step(`^the other sign-in is refused$`, w.theOtherIsRefused)
	sc.Step(`^"([^"]*)" holds an authenticated session$`, w.holdsAuthenticatedSession)
	sc.Step(`^"([^"]*)" holds no session on the instance$`, w.holdsNoSession)
	sc.Step(`^the answer names no refusal reason$`, w.answerNamesNoReason)
	sc.Step(`^the refusal names the reason "([^"]*)"$`, w.refusalNamesReason)
	sc.Step(`^the refusal names one of the contract's reasons$`, w.refusalNamesAContractReason)
	sc.Step(`^the instance does not report a failure of its own$`, w.noFailureOfItsOwn)
	sc.Step(`^the instance read the issuer's key list again before answering$`, w.keyListReadOnce)
	sc.Step(`^the instance read the issuer's key list once$`, w.keyListReadOnce)
	sc.Step(`^the instance's log records the reason "([^"]*)"$`, w.logRecordsReason)
	sc.Step(`^nothing the instance logged contains the assertion$`, w.logHoldsNoAssertion)
	sc.Step(`^both requests are answered with the same status and the same body$`, w.lastTwoAnswersSame)
	sc.Step(`^the instance warns that the trusted issuer's audience is missing$`, w.warnsAudienceMissing)
	sc.Step(`^someone presenting an assertion is answered as though the path never existed$`, w.assertAnsweredAsNeverExisted)
}

// --- the instance's clock ---

// tiClock is real time plus an offset the scenario can move. Sessions and the
// database run on real time; only the verifier reads this clock, and the door
// mints against it, so the two agree on what "now" is.
type tiClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *tiClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *tiClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

// --- the door ---

type tiDoorMode int

const (
	tiPublishes tiDoorMode = iota
	tiPublishesNothing
	tiUnreachable
)

type tiDoor struct {
	mu        sync.Mutex
	keys      map[string]*ecdsa.PrivateKey
	published []string
	mode      tiDoorMode
	reads     atomic.Int32
	server    *httptest.Server
}

func newTiDoor() *tiDoor {
	d := &tiDoor{keys: map[string]*ecdsa.PrivateKey{}}
	d.server = httptest.NewServer(http.HandlerFunc(d.serveKeyList))
	return d
}

func (d *tiDoor) key(kid string) (*ecdsa.PrivateKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if k, ok := d.keys[kid]; ok {
		return k, nil
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate the door's key %q: %w", kid, err)
	}
	d.keys[kid] = k
	return k, nil
}

func (d *tiDoor) publish(kids ...string) error {
	for _, kid := range kids {
		if _, err := d.key(kid); err != nil {
			return err
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.published = append([]string(nil), kids...)
	d.mode = tiPublishes
	return nil
}

func (d *tiDoor) setMode(m tiDoorMode) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mode = m
}

func (d *tiDoor) jwk(kid string) (map[string]string, error) {
	k, err := d.key(kid)
	if err != nil {
		return nil, err
	}
	raw, err := k.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
		"x": base64.RawURLEncoding.EncodeToString(raw[1:33]),
		"y": base64.RawURLEncoding.EncodeToString(raw[33:65]),
	}, nil
}

func (d *tiDoor) serveKeyList(w http.ResponseWriter, _ *http.Request) {
	d.reads.Add(1)
	d.mu.Lock()
	mode, kids := d.mode, append([]string(nil), d.published...)
	d.mu.Unlock()

	if mode == tiUnreachable {
		panic(http.ErrAbortHandler)
	}
	entries := []map[string]string{}
	if mode == tiPublishes {
		for _, kid := range kids {
			entry, err := d.jwk(kid)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			entries = append(entries, entry)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": entries})
}

func (d *tiDoor) close() {
	if d != nil && d.server != nil {
		d.server.Close()
	}
}

// --- the log ---

type tiLogLine struct {
	level, msg string
	fields     []any
}

type tiLogCapture struct {
	mu    sync.Mutex
	lines []tiLogLine
}

func (l *tiLogCapture) add(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, tiLogLine{level, msg, fields})
}

func (l *tiLogCapture) Debug(_ context.Context, m string, f ...any) { l.add("debug", m, f) }
func (l *tiLogCapture) Info(_ context.Context, m string, f ...any)  { l.add("info", m, f) }
func (l *tiLogCapture) Warn(_ context.Context, m string, f ...any)  { l.add("warn", m, f) }
func (l *tiLogCapture) Error(_ context.Context, m string, f ...any) { l.add("error", m, f) }

func (l *tiLogCapture) snapshot() []tiLogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]tiLogLine(nil), l.lines...)
}

func (l *tiLogCapture) text() string {
	var b strings.Builder
	for _, line := range l.snapshot() {
		fmt.Fprintf(&b, "%s %s %v\n", line.level, line.msg, line.fields)
	}
	return b.String()
}

func (line tiLogLine) field(key string) (any, bool) {
	for i := 0; i+1 < len(line.fields); i += 2 {
		if k, ok := line.fields[i].(string); ok && k == key {
			return line.fields[i+1], true
		}
	}
	return nil, false
}

// --- answers ---

type tiAnswer struct {
	status     int
	body       string
	code       string
	errorText  string
	agentID    string
	agentEmail string
	cookies    []*http.Cookie
}

func (a *tiAnswer) succeeded() error {
	if a == nil {
		return fmt.Errorf("no sign-in has been attempted")
	}
	if a.status != http.StatusOK {
		return fmt.Errorf("expected the sign-in to succeed, got %d: %s", a.status, a.body)
	}
	return nil
}

func (a *tiAnswer) refused() error {
	if a == nil {
		return fmt.Errorf("no sign-in has been attempted")
	}
	if a.status != http.StatusUnauthorized {
		return fmt.Errorf("expected the sign-in to be refused (401), got %d: %s", a.status, a.body)
	}
	return nil
}

// --- the world ---

type tiWorld struct {
	clock *tiClock
	logs  *tiLogCapture
	door  *tiDoor

	tmpDir   string
	app      *fx.App
	server   *httptest.Server
	verifier handlers.AssertionVerifier

	authService    authapp.AuthenticationService
	credRepo       authrepos.CredentialRepository
	agentRepo      authrepos.AgentRepository
	accountRepo    authrepos.AccountRepository
	sessionManager session.SessionManager
	erasureLocks   repositories.AccountErasureLocks
	appLogger      entities.Logger
	signIn         *application.AssertedSignIn

	issuer, audience string
	signingKey       string

	mu        sync.Mutex
	answers   []*tiAnswer // every answer, in order
	signIns   []*tiAnswer // answers to sign-in requests, in order
	presented []string    // every assertion sent to the instance
	held      string      // the assertion "the door has signed"
	lastBody  string      // the body last sent, for "the same details"

	envBefore map[string]*string
}

func (w *tiWorld) instanceTrusting(issuer, audience string) error {
	w.door = newTiDoor()
	w.issuer, w.audience = issuer, audience
	w.setEnv(config.EnvTrustedIssuer, ptr(issuer))
	w.setEnv(config.EnvTrustedIssuerJWKSURL, ptr(w.door.server.URL+"/door/jwks.json"))
	w.setEnv(config.EnvTrustedIssuerAudience, ptr(audience))
	return w.boot()
}

func (w *tiWorld) instanceWithNoIssuer() error {
	w.setEnv(config.EnvTrustedIssuer, nil)
	w.setEnv(config.EnvTrustedIssuerJWKSURL, nil)
	w.setEnv(config.EnvTrustedIssuerAudience, nil)
	return w.boot()
}

// instanceMissingAudience configures, and does not start, an instance: the
// scenario starts it in its When, where the boot warning is the behavior.
func (w *tiWorld) instanceMissingAudience() error {
	w.door = newTiDoor()
	w.issuer = "https://money.weos.cloud"
	w.setEnv(config.EnvTrustedIssuer, ptr(w.issuer))
	w.setEnv(config.EnvTrustedIssuerJWKSURL, ptr(w.door.server.URL+"/door/jwks.json"))
	w.setEnv(config.EnvTrustedIssuerAudience, nil)
	return nil
}

// boot starts the application against a fresh SQLite database, reading the
// trusted-issuer settings from the environment through config, and mounts the
// assertion route the way serve.go does.
func (w *tiWorld) boot() error {
	dir, err := os.MkdirTemp("", "weos-trusted-issuer-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	// Pinned after reading the environment so an ambient DATABASE_DSN cannot
	// pull the scenario onto a real database.
	cfg.DatabaseDSN = filepath.Join(dir, "test.db")
	cfg.LogLevel = "error"

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		// serve.go provides this beside the module; sign-in issues its token.
		fx.Provide(weosoauth.ProvideJWTService),
		fx.Populate(&w.authService, &w.credRepo, &w.agentRepo, &w.accountRepo),
		fx.Populate(&w.sessionManager, &w.erasureLocks, &w.appLogger, &w.signIn),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app

	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")
	api.Use(apimw.Messages())

	// The dependencies serve.go gives the handler, all of them, so an asserted
	// sign-in completes here exactly as it completes in a running instance.
	sessions := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    w.authService,
		SessionManager: w.sessionManager,
		SecureCookies:  cfg.SessionSecret != "change-me-in-production",
		Logger:         w.logs,
		AccountRepo:    w.accountRepo,
		ErasureLocks:   w.erasureLocks,
	})
	// The same call and the same constructor as serve.go. The clock is the one
	// thing added: the scenarios move time without waiting for it.
	handlers.MountTrustedIssuerAssertion(context.Background(), api, cfg.TrustedIssuer, w.logs,
		func() *handlers.TrustedIssuerHandler {
			h := handlers.NewTrustedIssuerAssertionHandler(cfg.TrustedIssuer, cfg.OAuth.AllowedEmails, handlers.TrustedIssuerAssertionDeps{
				SignIn:   w.signIn,
				Sessions: sessions,
				Logger:   w.logs,
				Now:      w.clock.Now,
			})
			w.verifier = h.Verifier()
			return h
		})

	// What an unmatched /api path meets on an instance with no OAuth
	// configured: the not-found catch-all serve.go's last empty-prefix group
	// registers, behind SoftAuth. password_registration_flag_test.go explains
	// why that, and not a bare echo 404, is the answer to compare against.
	catchAll := api.Group("")
	catchAll.Use(apimw.SoftAuth(w.credRepo, w.agentRepo, w.accountRepo, w.appLogger))

	w.server = httptest.NewServer(e)
	return nil
}

func (w *tiWorld) teardown() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(stopCtx)
		w.app = nil
	}
	w.door.close()
	w.restoreEnv()
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

// setEnv sets or clears a variable, remembering what was there the first time
// this scenario touched it; a nil value means absent.
func (w *tiWorld) setEnv(key string, value *string) {
	if w.envBefore == nil {
		w.envBefore = map[string]*string{}
	}
	if _, remembered := w.envBefore[key]; !remembered {
		if prior, ok := os.LookupEnv(key); ok {
			w.envBefore[key] = &prior
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

func (w *tiWorld) restoreEnv() {
	for key, prior := range w.envBefore {
		if prior == nil {
			os.Unsetenv(key)
			continue
		}
		os.Setenv(key, *prior)
	}
	w.envBefore = nil
}

// --- the key list ---

func (w *tiWorld) publishOnly(kid string) error {
	w.signingKey = kid
	return w.door.publish(kid)
}

// keyInHand has the instance read the key list holding kid, then forgets that
// read, so the scenario counts only the reads that follow.
func (w *tiWorld) keyInHand(kid string) error {
	if err := w.publishOnly(kid); err != nil {
		return err
	}
	if w.verifier == nil {
		return fmt.Errorf("the instance has no assertion verifier; is the route mounted?")
	}
	token, err := w.sign(kid, w.claimsFor("warm-up@harborlegal.example"))
	if err != nil {
		return err
	}
	if _, err := w.verifier.Verify(context.Background(), token); err != nil {
		return fmt.Errorf("the instance could not take the issuer's key %q in hand: %w", kid, err)
	}
	w.door.reads.Store(0)
	return nil
}

func (w *tiWorld) alsoPublish(kid string) error {
	w.door.mu.Lock()
	kids := append(append([]string(nil), w.door.published...), kid)
	w.door.mu.Unlock()
	return w.door.publish(kids...)
}

// --- minting ---

func tiJTI() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+).
	return hex.EncodeToString(b)
}

// claimsFor is a good assertion for email, minted now. The subject is stable
// per person, as a provider's is.
func (w *tiWorld) claimsFor(email string) gojwt.MapClaims {
	now := w.clock.Now()
	return gojwt.MapClaims{
		"iss":            w.issuer,
		"aud":            w.audience,
		"sub":            "google-" + email,
		"email":          email,
		"email_verified": true,
		"provider":       "google",
		"iat":            now.Unix(),
		"exp":            now.Add(45 * time.Second).Unix(),
		"jti":            tiJTI(),
	}
}

func (w *tiWorld) sign(kid string, claims gojwt.MapClaims) (string, error) {
	key, err := w.door.key(kid)
	if err != nil {
		return "", err
	}
	return tiSignES256(key, kid, claims)
}

func tiSignES256(key *ecdsa.PrivateKey, kid string, claims gojwt.MapClaims) (string, error) {
	tok := gojwt.NewWithClaims(gojwt.SigningMethodES256, claims)
	tok.Header["kid"] = kid
	return tok.SignedString(key)
}

func (w *tiWorld) currentKey() (string, error) {
	if w.signingKey == "" {
		return "", fmt.Errorf("the scenario has not said which key the issuer signs with")
	}
	return w.signingKey, nil
}

// presentClaims signs claims with the issuer's current key and presents them.
func (w *tiWorld) presentClaims(claims gojwt.MapClaims) error {
	kid, err := w.currentKey()
	if err != nil {
		return err
	}
	token, err := w.sign(kid, claims)
	if err != nil {
		return err
	}
	return w.present(token)
}

func (w *tiWorld) presentSignedWith(email, kid string) error {
	token, err := w.sign(kid, w.claimsFor(email))
	if err != nil {
		return err
	}
	return w.present(token)
}

func (w *tiWorld) presentFresh(email string) error {
	return w.presentClaims(w.claimsFor(email))
}

func (w *tiWorld) presentLiving(email string, seconds int) error {
	c := w.claimsFor(email)
	now := w.clock.Now()
	c["iat"], c["exp"] = now.Unix(), now.Add(time.Duration(seconds)*time.Second).Unix()
	return w.presentClaims(c)
}

func (w *tiWorld) presentExpiredAgo(email string, seconds int) error {
	c := w.claimsFor(email)
	exp := w.clock.Now().Add(-time.Duration(seconds) * time.Second)
	c["exp"], c["iat"] = exp.Unix(), exp.Add(-45*time.Second).Unix()
	return w.presentClaims(c)
}

func (w *tiWorld) presentIssuedAhead(email string, seconds int) error {
	c := w.claimsFor(email)
	iat := w.clock.Now().Add(time.Duration(seconds) * time.Second)
	c["iat"], c["exp"] = iat.Unix(), iat.Add(45*time.Second).Unix()
	return w.presentClaims(c)
}

func (w *tiWorld) presentSignedWithRogueKey(email string) error {
	kid, err := w.currentKey()
	if err != nil {
		return err
	}
	rogue, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	token, err := tiSignES256(rogue, kid, w.claimsFor(email))
	if err != nil {
		return err
	}
	return w.present(token)
}

func (w *tiWorld) presentNamingIssuer(email, issuer string) error {
	c := w.claimsFor(email)
	c["iss"] = issuer
	return w.presentClaims(c)
}

func (w *tiWorld) presentNamingAudience(email, audience string) error {
	c := w.claimsFor(email)
	c["aud"] = audience
	return w.presentClaims(c)
}

func (w *tiWorld) presentUnverifiedEmail(email string) error {
	c := w.claimsFor(email)
	c["email_verified"] = false
	return w.presentClaims(c)
}

func (w *tiWorld) presentWithoutSubject(email string) error {
	c := w.claimsFor(email)
	delete(c, "sub")
	return w.presentClaims(c)
}

func (w *tiWorld) presentNamingProvider(email, provider string) error {
	c := w.claimsFor(email)
	c["provider"] = provider
	return w.presentClaims(c)
}

// presentHMACOverPublicKey is the algorithm-confusion forgery: anyone can read
// the issuer's published key, so an instance that let the assertion pick HS256
// would take that public text as the shared secret.
func (w *tiWorld) presentHMACOverPublicKey(email string) error {
	kid, err := w.currentKey()
	if err != nil {
		return err
	}
	jwk, err := w.door.jwk(kid)
	if err != nil {
		return err
	}
	secret, err := json.Marshal(jwk)
	if err != nil {
		return err
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodHS256, w.claimsFor(email))
	tok.Header["kid"] = kid
	token, err := tok.SignedString(secret)
	if err != nil {
		return err
	}
	return w.present(token)
}

func (w *tiWorld) presentUnsigned(email string) error {
	kid, err := w.currentKey()
	if err != nil {
		return err
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodNone, w.claimsFor(email))
	tok.Header["kid"] = kid
	token, err := tok.SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		return err
	}
	return w.present(token)
}

func (w *tiWorld) signOneAssertion(email string) error {
	kid, err := w.currentKey()
	if err != nil {
		return err
	}
	w.held, err = w.sign(kid, w.claimsFor(email))
	return err
}

func (w *tiWorld) presentHeld() error {
	if w.held == "" {
		return fmt.Errorf("the door has not signed an assertion to present")
	}
	return w.present(w.held)
}

func (w *tiWorld) raceHeld() error {
	if w.held == "" {
		return fmt.Errorf("the door has not signed an assertion to present")
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- w.present(w.held)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *tiWorld) signedInAMomentAgo(email string) error {
	if err := w.presentFresh(email); err != nil {
		return err
	}
	return w.signInAt(-1).succeeded()
}

func (w *tiWorld) presentToUnconfiguredInstance() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	w.issuer, w.audience = "https://money.weos.cloud", "a1b2c3d4"
	token, err := tiSignES256(key, "door-2026-09", w.claimsFor("ops@harborlegal.example"))
	if err != nil {
		return err
	}
	return w.present(token)
}

func (w *tiWorld) postSameDetailsTo(path string) error {
	if w.lastBody == "" {
		return fmt.Errorf("nothing has been sent yet, so there are no same details to send")
	}
	_, err := w.post(path, w.lastBody)
	return err
}

// --- requests ---

func tiAssertionBody(assertion string) string {
	body, _ := json.Marshal(map[string]string{"assertion": assertion})
	return string(body)
}

func (w *tiWorld) present(token string) error {
	w.mu.Lock()
	w.presented = append(w.presented, token)
	w.mu.Unlock()
	return w.sendSignIn(tiAssertionBody(token))
}

func (w *tiWorld) sendSignIn(body string) error {
	answer, err := w.post(tiAssertPath, body)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.signIns = append(w.signIns, answer)
	w.mu.Unlock()
	return nil
}

func (w *tiWorld) post(path, body string) (*tiAnswer, error) {
	if w.server == nil {
		return nil, fmt.Errorf("the instance has not started")
	}
	req, err := http.NewRequest(http.MethodPost, w.server.URL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	answer := &tiAnswer{status: resp.StatusCode, body: strings.TrimSpace(string(raw)), cookies: resp.Cookies()}
	var parsed struct {
		Error string `json:"error"`
		Code  string `json:"code"`
		Data  struct {
			Agent struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"agent"`
		} `json:"data"`
	}
	// Not every answer is this envelope (a not-found body is echo's own); the
	// fields simply stay empty, and the steps that need them say so.
	_ = json.Unmarshal(raw, &parsed)
	answer.code, answer.errorText = parsed.Code, parsed.Error
	answer.agentID, answer.agentEmail = parsed.Data.Agent.ID, parsed.Data.Agent.Email

	w.mu.Lock()
	w.answers = append(w.answers, answer)
	w.lastBody = body
	w.mu.Unlock()
	return answer, nil
}

// --- outcomes ---

// signInAt returns the i-th sign-in answer, counting from the end when i < 0.
func (w *tiWorld) signInAt(i int) *tiAnswer {
	w.mu.Lock()
	defer w.mu.Unlock()
	if i < 0 {
		i += len(w.signIns)
	}
	if i < 0 || i >= len(w.signIns) {
		return nil
	}
	return w.signIns[i]
}

func (w *tiWorld) lastTwoSignIns() ([]*tiAnswer, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.signIns) < 2 {
		return nil, fmt.Errorf("expected two sign-ins, saw %d", len(w.signIns))
	}
	return w.signIns[len(w.signIns)-2:], nil
}

func (w *tiWorld) lastTwoSucceed() error {
	pair, err := w.lastTwoSignIns()
	if err != nil {
		return err
	}
	for _, a := range pair {
		if err := a.succeeded(); err != nil {
			return err
		}
	}
	return nil
}

func (w *tiWorld) exactlyOneOfTwoSucceeds() error {
	pair, err := w.lastTwoSignIns()
	if err != nil {
		return err
	}
	ok := 0
	for _, a := range pair {
		if a.status == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		return fmt.Errorf("expected exactly one of the two sign-ins to succeed, %d did: %d %s / %d %s",
			ok, pair[0].status, pair[0].body, pair[1].status, pair[1].body)
	}
	return nil
}

func (w *tiWorld) theOtherIsRefused() error {
	pair, err := w.lastTwoSignIns()
	if err != nil {
		return err
	}
	for _, a := range pair {
		if a.status != http.StatusOK {
			return a.refused()
		}
	}
	return fmt.Errorf("both sign-ins succeeded; neither was refused")
}

// latestRefusal is the most recent refused sign-in, which is the refusal a
// scenario means when it goes on to name a reason.
func (w *tiWorld) latestRefusal() (*tiAnswer, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := len(w.signIns) - 1; i >= 0; i-- {
		if w.signIns[i].status == http.StatusUnauthorized {
			return w.signIns[i], nil
		}
	}
	return nil, fmt.Errorf("no sign-in has been refused")
}

func (w *tiWorld) refusalNamesReason(reason string) error {
	refusal, err := w.latestRefusal()
	if err != nil {
		return err
	}
	if refusal.code != reason {
		return fmt.Errorf("expected the refusal to name %q, it named %q: %s", reason, refusal.code, refusal.body)
	}
	return nil
}

func (w *tiWorld) refusalNamesAContractReason() error {
	refusal, err := w.latestRefusal()
	if err != nil {
		return err
	}
	if !tiContractReasons[refusal.code] {
		return fmt.Errorf("the refusal names %q, which is not one of the contract's reasons: %s", refusal.code, refusal.body)
	}
	return nil
}

func (w *tiWorld) noFailureOfItsOwn() error {
	last := w.signInAt(-1)
	if last == nil {
		return fmt.Errorf("no sign-in has been attempted")
	}
	if last.status >= http.StatusInternalServerError {
		return fmt.Errorf("the instance reported a failure of its own: %d %s", last.status, last.body)
	}
	return last.refused()
}

func (w *tiWorld) answerNamesNoReason() error {
	last := w.signInAt(-1)
	if last == nil {
		return fmt.Errorf("no sign-in has been attempted")
	}
	if last.code != "" || last.errorText != "" {
		return fmt.Errorf("the answer names a refusal: %s", last.body)
	}
	return nil
}

// holdsAuthenticatedSession requires the last answer to name the person and to
// carry a session cookie that the instance's session manager reads back as a
// session for the agent the answer names.
func (w *tiWorld) holdsAuthenticatedSession(email string) error {
	last := w.signInAt(-1)
	if err := last.succeeded(); err != nil {
		return err
	}
	if !strings.EqualFold(last.agentEmail, email) {
		return fmt.Errorf("expected the sign-in to be for %q, the answer names %q", email, last.agentEmail)
	}
	if len(last.cookies) == 0 {
		return fmt.Errorf("expected a session cookie for %q, none was set", email)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	for _, c := range last.cookies {
		req.AddCookie(c)
	}
	data, err := w.sessionManager.GetHTTPSession(req)
	if err != nil || data == nil {
		return fmt.Errorf("the cookies set for %q do not carry a session: %v", email, err)
	}
	if data.AgentID == "" || data.AgentID != last.agentID {
		return fmt.Errorf("the session is for agent %q, the answer names agent %q", data.AgentID, last.agentID)
	}
	creds, err := w.credRepo.FindByEmail(context.Background(), email)
	if err != nil {
		return fmt.Errorf("could not look up %q: %w", email, err)
	}
	if len(creds) == 0 {
		return fmt.Errorf("the instance holds no credential for %q", email)
	}
	return nil
}

func (w *tiWorld) holdsNoSession(email string) error {
	last := w.signInAt(-1)
	if err := last.refused(); err != nil {
		return err
	}
	if len(last.cookies) != 0 {
		return fmt.Errorf("a refused sign-in set cookies: %v", last.cookies)
	}
	// An unknown email is an empty result, not an error; a real error means
	// the lookup never happened and must not read as absence.
	creds, err := w.credRepo.FindByEmail(context.Background(), email)
	if err != nil {
		return fmt.Errorf("could not check whether %q holds anything: %w", email, err)
	}
	if len(creds) != 0 {
		return fmt.Errorf("a refused sign-in left %d credential(s) for %q", len(creds), email)
	}
	return nil
}

func (w *tiWorld) keyListReadOnce() error {
	if got := w.door.reads.Load(); got != 1 {
		return fmt.Errorf("expected the instance to read the issuer's key list once, it read it %d times", got)
	}
	return nil
}

func (w *tiWorld) logRecordsReason(reason string) error {
	for _, line := range w.logs.snapshot() {
		if got, ok := line.field("reason"); ok && got == reason {
			return nil
		}
	}
	return fmt.Errorf("no log line records the reason %q; the log:\n%s", reason, w.logs.text())
}

func (w *tiWorld) logHoldsNoAssertion() error {
	logged := w.logs.text()
	w.mu.Lock()
	presented := append([]string(nil), w.presented...)
	w.mu.Unlock()
	if len(presented) == 0 {
		return fmt.Errorf("no assertion has been presented")
	}
	for _, token := range presented {
		for i, part := range strings.Split(token, ".") {
			if part != "" && strings.Contains(logged, part) {
				return fmt.Errorf("the log carries segment %d of a presented assertion:\n%s", i, logged)
			}
		}
	}
	return nil
}

func (w *tiWorld) lastTwoAnswersSame() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.answers) < 2 {
		return fmt.Errorf("expected two requests, saw %d", len(w.answers))
	}
	a, b := w.answers[len(w.answers)-2], w.answers[len(w.answers)-1]
	if a.status != b.status {
		return fmt.Errorf("answers differ in status: %d vs %d", a.status, b.status)
	}
	if a.body != b.body {
		return fmt.Errorf("answers differ in body: %q vs %q", a.body, b.body)
	}
	return nil
}

func (w *tiWorld) warnsAudienceMissing() error {
	var warnings []tiLogLine
	for _, line := range w.logs.snapshot() {
		if line.level == "warn" {
			warnings = append(warnings, line)
		}
	}
	if len(warnings) != 1 {
		return fmt.Errorf("expected one boot warning, got %d:\n%s", len(warnings), w.logs.text())
	}
	if missing, _ := warnings[0].field("missing"); missing != config.EnvTrustedIssuerAudience {
		return fmt.Errorf("expected the warning to name %s as missing, it names %v", config.EnvTrustedIssuerAudience, missing)
	}
	return nil
}

func (w *tiWorld) assertAnsweredAsNeverExisted() error {
	w.issuer, w.audience = "https://money.weos.cloud", "a1b2c3d4"
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	token, err := tiSignES256(key, "door-2026-09", w.claimsFor("ops@harborlegal.example"))
	if err != nil {
		return err
	}
	if err := w.present(token); err != nil {
		return err
	}
	if err := w.postSameDetailsTo("/api/auth/enroll"); err != nil {
		return err
	}
	return w.lastTwoAnswersSame()
}
