// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package trustedissuer_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/trustedissuer"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "https://money.weos.cloud"
	testAudience = "a1b2c3d4"
	opsEmail     = "ops@harborlegal.example"
	currentKey   = "door-2026-09"
)

// --- a clock the tests move by hand ---

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *testClock { return &testClock{now: time.Unix(1_789_000_000, 0)} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// --- a door that publishes a key list and signs assertions ---

type doorMode int

const (
	doorPublishes doorMode = iota
	doorPublishesNothing
	doorFails
	doorUnreachable
)

type testDoor struct {
	t         *testing.T
	mu        sync.Mutex
	keys      map[string]*ecdsa.PrivateKey
	published []string
	mode      doorMode
	fetches   atomic.Int32
	server    *httptest.Server
}

func newDoor(t *testing.T) *testDoor {
	t.Helper()
	d := &testDoor{t: t, keys: map[string]*ecdsa.PrivateKey{}}
	d.server = httptest.NewServer(http.HandlerFunc(d.serveKeyList))
	t.Cleanup(d.server.Close)
	return d
}

func (d *testDoor) key(kid string) *ecdsa.PrivateKey {
	d.mu.Lock()
	defer d.mu.Unlock()
	if k, ok := d.keys[kid]; ok {
		return k
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		d.t.Fatalf("generate key %s: %v", kid, err)
	}
	d.keys[kid] = k
	return k
}

func (d *testDoor) publish(kids ...string) {
	for _, kid := range kids {
		d.key(kid)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.published = kids
	d.mode = doorPublishes
}

func (d *testDoor) setMode(m doorMode) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mode = m
}

func (d *testDoor) serveKeyList(w http.ResponseWriter, _ *http.Request) {
	d.fetches.Add(1)
	d.mu.Lock()
	mode := d.mode
	var entries []map[string]string
	for _, kid := range d.published {
		entries = append(entries, publicJWK(d.t, kid, &d.keys[kid].PublicKey))
	}
	d.mu.Unlock()

	switch mode {
	case doorUnreachable:
		panic(http.ErrAbortHandler)
	case doorFails:
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	case doorPublishesNothing:
		entries = nil
	}
	if entries == nil {
		entries = []map[string]string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": entries})
}

func publicJWK(t *testing.T, kid string, pub *ecdsa.PublicKey) map[string]string {
	raw, err := pub.Bytes()
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	return map[string]string{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
		"x": base64.RawURLEncoding.EncodeToString(raw[1:33]),
		"y": base64.RawURLEncoding.EncodeToString(raw[33:65]),
	}
}

type claims map[string]any

func newJTI(t *testing.T) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("jti: %v", err)
	}
	return hex.EncodeToString(b)
}

func goodClaims(t *testing.T, now time.Time) claims {
	return claims{
		"iss":            testIssuer,
		"aud":            testAudience,
		"sub":            "108234567890",
		"email":          opsEmail,
		"email_verified": true,
		"provider":       "google",
		"name":           "Harbor Ops",
		"iat":            now.Unix(),
		"exp":            now.Add(45 * time.Second).Unix(),
		"jti":            newJTI(t),
	}
}

func (d *testDoor) sign(kid string, c claims) string {
	return signES256(d.t, d.key(kid), kid, c)
}

func signES256(t *testing.T, key *ecdsa.PrivateKey, kid string, c claims) string {
	tok := gojwt.NewWithClaims(gojwt.SigningMethodES256, gojwt.MapClaims(c))
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

// --- a logger that remembers every line ---

type logLine struct {
	level, msg string
	fields     []any
}

type captureLogger struct {
	mu    sync.Mutex
	lines []logLine
}

func (l *captureLogger) add(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, logLine{level: level, msg: msg, fields: fields})
}

func (l *captureLogger) Debug(_ context.Context, msg string, f ...any) { l.add("debug", msg, f) }
func (l *captureLogger) Info(_ context.Context, msg string, f ...any)  { l.add("info", msg, f) }
func (l *captureLogger) Warn(_ context.Context, msg string, f ...any)  { l.add("warn", msg, f) }
func (l *captureLogger) Error(_ context.Context, msg string, f ...any) { l.add("error", msg, f) }

func (l *captureLogger) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = nil
}

func (l *captureLogger) all() []logLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]logLine(nil), l.lines...)
}

func (l *captureLogger) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, line := range l.lines {
		fmt.Fprintf(&b, "%s %s %v\n", line.level, line.msg, line.fields)
	}
	return b.String()
}

// --- the fixture ---

type fixture struct {
	door     *testDoor
	clock    *testClock
	logs     *captureLogger
	verifier *trustedissuer.Verifier
}

// oneConnectionPerRead keeps each key-list read on its own connection. On a
// reused connection, Go's transport silently replays a GET the server aborted,
// which would count one unreachable read as two.
func oneConnectionPerRead() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWith(t, nil)
}

// newFixtureWith builds the fixture, letting a test change the verifier's
// settings before the verifier is built.
func newFixtureWith(t *testing.T, configure func(*trustedissuer.Config)) *fixture {
	t.Helper()
	door := newDoor(t)
	door.publish(currentKey)
	clock := newClock()
	logs := &captureLogger{}
	cfg := trustedissuer.Config{
		Issuer:     testIssuer,
		JWKSURL:    door.server.URL + "/door/jwks.json",
		Audience:   testAudience,
		Providers:  []string{"google", "netsuite", "apple"},
		HTTPClient: oneConnectionPerRead(),
		Now:        clock.Now,
		Logger:     logs,
	}
	if configure != nil {
		configure(&cfg)
	}
	v := trustedissuer.NewVerifier(cfg)
	return &fixture{door: door, clock: clock, logs: logs, verifier: v}
}

func (f *fixture) good(t *testing.T) claims { return goodClaims(t, f.clock.Now()) }

func (f *fixture) verify(token string) (trustedissuer.Identity, error) {
	return f.verifier.Verify(context.Background(), token)
}

// prime puts the current key in the verifier's hands and forgets the fetch.
func (f *fixture) prime(t *testing.T) {
	t.Helper()
	if _, err := f.verify(f.door.sign(currentKey, f.good(t))); err != nil {
		t.Fatalf("prime: %v", err)
	}
	f.door.fetches.Store(0)
}

func requireReason(t *testing.T, err error, want trustedissuer.Reason) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a refusal with reason %q, the assertion was accepted", want)
	}
	var refusal *trustedissuer.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("expected a *trustedissuer.Refusal, got %T: %v", err, err)
	}
	if refusal.Reason != want {
		t.Fatalf("expected reason %q, got %q (%s)", want, refusal.Reason, refusal.Detail)
	}
}

func requireAccepted(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected the assertion to be accepted, got %v", err)
	}
}

// --- acceptance ---

func TestVerifyAcceptsAnAssertionFromTheTrustedIssuer(t *testing.T) {
	f := newFixture(t)
	c := f.good(t)
	id, err := f.verify(f.door.sign(currentKey, c))
	requireAccepted(t, err)
	if id.Subject != "108234567890" || id.Email != opsEmail || id.Provider != "google" || id.Name != "Harbor Ops" {
		t.Fatalf("identity does not carry the claims: %+v", id)
	}
	if id.JTI != c["jti"] {
		t.Fatalf("identity jti = %q, want %q", id.JTI, c["jti"])
	}
	if got := f.door.fetches.Load(); got != 1 {
		t.Fatalf("expected one key-list read, got %d", got)
	}
}

func TestVerifyAcceptsEachSideOfTheClockAllowance(t *testing.T) {
	cases := map[string]func(now time.Time, c claims){
		"lives exactly sixty seconds": func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Unix(), now.Add(60*time.Second).Unix()
		},
		"expired twenty seconds ago": func(now time.Time, c claims) {
			c["exp"], c["iat"] = now.Add(-20*time.Second).Unix(), now.Add(-80*time.Second).Unix()
		},
		"expired thirty seconds ago": func(now time.Time, c claims) {
			c["exp"], c["iat"] = now.Add(-30*time.Second).Unix(), now.Add(-90*time.Second).Unix()
		},
		"issued twenty seconds in the future": func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Add(20*time.Second).Unix(), now.Add(80*time.Second).Unix()
		},
		"issued thirty seconds in the future": func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Add(30*time.Second).Unix(), now.Add(90*time.Second).Unix()
		},
		"name left out": func(_ time.Time, c claims) { delete(c, "name") },
		"audience as a one-element list": func(_ time.Time, c claims) {
			c["aud"] = []string{testAudience}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			c := f.good(t)
			mutate(f.clock.Now(), c)
			_, err := f.verify(f.door.sign(currentKey, c))
			requireAccepted(t, err)
		})
	}
}

// --- every refusal, by reason ---

func TestVerifyRefusesWithTheReasonWhy(t *testing.T) {
	type tc struct {
		token func(t *testing.T, f *fixture) string
		want  trustedissuer.Reason
	}
	withClaims := func(mutate func(now time.Time, c claims)) func(*testing.T, *fixture) string {
		return func(t *testing.T, f *fixture) string {
			c := f.good(t)
			mutate(f.clock.Now(), c)
			return f.door.sign(currentKey, c)
		}
	}
	cases := map[string]tc{
		// signature
		"signed with a key the issuer never published": {func(t *testing.T, f *fixture) string {
			rogue, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			return signES256(t, rogue, currentKey, f.good(t))
		}, trustedissuer.ReasonSignature},
		"HMAC over the published public key": {func(t *testing.T, f *fixture) string {
			pub, _ := f.door.key(currentKey).PublicKey.Bytes()
			tok := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.MapClaims(f.good(t)))
			tok.Header["kid"] = currentKey
			s, err := tok.SignedString(pub)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}, trustedissuer.ReasonSignature},
		"alg none": {func(t *testing.T, f *fixture) string {
			tok := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims(f.good(t)))
			tok.Header["kid"] = currentKey
			s, err := tok.SignedString(gojwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}, trustedissuer.ReasonSignature},
		"ES256 header with the signature stripped": {func(t *testing.T, f *fixture) string {
			s := f.door.sign(currentKey, f.good(t))
			return s[:strings.LastIndex(s, ".")+1]
		}, trustedissuer.ReasonSignature},
		"claims swapped under a real signature": {func(t *testing.T, f *fixture) string {
			genuine := strings.Split(f.door.sign(currentKey, f.good(t)), ".")
			forged := f.good(t)
			forged["email"] = "intruder@harborlegal.example"
			other := strings.Split(f.door.sign(currentKey, forged), ".")
			return genuine[0] + "." + other[1] + "." + genuine[2]
		}, trustedissuer.ReasonSignature},
		"no kid in the header": {func(t *testing.T, f *fixture) string {
			return signES256(t, f.door.key(currentKey), "", f.good(t))
		}, trustedissuer.ReasonSignature},
		"not a token":      {func(*testing.T, *fixture) string { return "not-a-token" }, trustedissuer.ReasonSignature},
		"empty":            {func(*testing.T, *fixture) string { return "" }, trustedissuer.ReasonSignature},
		"only whitespace":  {func(*testing.T, *fixture) string { return "   " }, trustedissuer.ReasonSignature},
		"three empty dots": {func(*testing.T, *fixture) string { return ".." }, trustedissuer.ReasonSignature},

		// kid-miss
		"a key id the issuer does not publish": {func(t *testing.T, f *fixture) string {
			return f.door.sign("door-2026-11", f.good(t))
		}, trustedissuer.ReasonKidMiss},

		// iss
		"another issuer":   {withClaims(func(_ time.Time, c claims) { c["iss"] = "https://door.cedarrealty.example" }), trustedissuer.ReasonIssuer},
		"issuer left out":  {withClaims(func(_ time.Time, c claims) { delete(c, "iss") }), trustedissuer.ReasonIssuer},
		"issuer not exact": {withClaims(func(_ time.Time, c claims) { c["iss"] = testIssuer + "/" }), trustedissuer.ReasonIssuer},

		// aud
		"another audience":    {withClaims(func(_ time.Time, c claims) { c["aud"] = "9f8e7d6c" }), trustedissuer.ReasonAudience},
		"audience left out":   {withClaims(func(_ time.Time, c claims) { delete(c, "aud") }), trustedissuer.ReasonAudience},
		"two audiences":       {withClaims(func(_ time.Time, c claims) { c["aud"] = []string{testAudience, "9f8e7d6c"} }), trustedissuer.ReasonAudience},
		"audience not a text": {withClaims(func(_ time.Time, c claims) { c["aud"] = 42 }), trustedissuer.ReasonAudience},

		// expired
		"expired forty-five seconds ago": {withClaims(func(now time.Time, c claims) {
			c["exp"], c["iat"] = now.Add(-45*time.Second).Unix(), now.Add(-105*time.Second).Unix()
		}), trustedissuer.ReasonExpired},
		"expired thirty-one seconds ago": {withClaims(func(now time.Time, c claims) {
			c["exp"], c["iat"] = now.Add(-31*time.Second).Unix(), now.Add(-91*time.Second).Unix()
		}), trustedissuer.ReasonExpired},

		// window
		"minted to live ten minutes": {withClaims(func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Unix(), now.Add(10*time.Minute).Unix()
		}), trustedissuer.ReasonWindow},
		"minted to live sixty-one seconds": {withClaims(func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Unix(), now.Add(61*time.Second).Unix()
		}), trustedissuer.ReasonWindow},
		"issued forty-five seconds in the future": {withClaims(func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Add(45*time.Second).Unix(), now.Add(100*time.Second).Unix()
		}), trustedissuer.ReasonWindow},
		"expiry before issue": {withClaims(func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Add(10*time.Second).Unix(), now.Add(5*time.Second).Unix()
		}), trustedissuer.ReasonWindow},
		"no expiry":    {withClaims(func(_ time.Time, c claims) { delete(c, "exp") }), trustedissuer.ReasonWindow},
		"no issued-at": {withClaims(func(_ time.Time, c claims) { delete(c, "iat") }), trustedissuer.ReasonWindow},

		// claims
		"no jti":                  {withClaims(func(_ time.Time, c claims) { delete(c, "jti") }), trustedissuer.ReasonClaims},
		"no subject":              {withClaims(func(_ time.Time, c claims) { delete(c, "sub") }), trustedissuer.ReasonClaims},
		"no email":                {withClaims(func(_ time.Time, c claims) { delete(c, "email") }), trustedissuer.ReasonClaims},
		"no provider":             {withClaims(func(_ time.Time, c claims) { delete(c, "provider") }), trustedissuer.ReasonClaims},
		"provider okta":           {withClaims(func(_ time.Time, c claims) { c["provider"] = "okta" }), trustedissuer.ReasonClaims},
		"provider not verbatim":   {withClaims(func(_ time.Time, c claims) { c["provider"] = "Google" }), trustedissuer.ReasonClaims},
		"email_verified false":    {withClaims(func(_ time.Time, c claims) { c["email_verified"] = false }), trustedissuer.ReasonClaims},
		"email_verified absent":   {withClaims(func(_ time.Time, c claims) { delete(c, "email_verified") }), trustedissuer.ReasonClaims},
		"email_verified as text":  {withClaims(func(_ time.Time, c claims) { c["email_verified"] = "true" }), trustedissuer.ReasonClaims},
		"subject not a text":      {withClaims(func(_ time.Time, c claims) { c["sub"] = 7 }), trustedissuer.ReasonClaims},
		"subject only whitespace": {withClaims(func(_ time.Time, c claims) { c["sub"] = "  " }), trustedissuer.ReasonClaims},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.verify(c.token(t, f))
			requireReason(t, err, c.want)
		})
	}
}

func TestRefusalNeverCarriesTheAssertion(t *testing.T) {
	f := newFixture(t)
	c := f.good(t)
	c["iss"] = "https://door.cedarrealty.example"
	token := f.door.sign(currentKey, c)
	_, err := f.verify(token)
	requireReason(t, err, trustedissuer.ReasonIssuer)
	for i, part := range strings.Split(token, ".") {
		if strings.Contains(err.Error(), part) {
			t.Fatalf("refusal text carries segment %d of the assertion: %v", i, err)
		}
	}
}

// --- single use ---

func TestVerifyRefusesAnAssertionPresentedTwice(t *testing.T) {
	f := newFixture(t)
	token := f.door.sign(currentKey, f.good(t))
	_, err := f.verify(token)
	requireAccepted(t, err)
	_, err = f.verify(token)
	requireReason(t, err, trustedissuer.ReasonReplay)
}

func TestARefusedAssertionDoesNotSpendItsJTI(t *testing.T) {
	f := newFixture(t)
	c := f.good(t)
	c["email_verified"] = false
	_, err := f.verify(f.door.sign(currentKey, c))
	requireReason(t, err, trustedissuer.ReasonClaims)
	c["email_verified"] = true
	_, err = f.verify(f.door.sign(currentKey, c))
	requireAccepted(t, err)
}

func TestRacingPresentationsOfOneAssertionLetExactlyOneThrough(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	token := f.door.sign(currentKey, f.good(t))
	const racers = 16
	var accepted, replayed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.verify(token)
			var refusal *trustedissuer.Refusal
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.As(err, &refusal) && refusal.Reason == trustedissuer.ReasonReplay:
				replayed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 1 || replayed.Load() != racers-1 {
		t.Fatalf("expected 1 accepted and %d replays, got %d and %d", racers-1, accepted.Load(), replayed.Load())
	}
}

func TestAJTIIsRememberedForFiveMinutes(t *testing.T) {
	f := newFixture(t)
	c := f.good(t)
	_, err := f.verify(f.door.sign(currentKey, c))
	requireAccepted(t, err)

	f.clock.Advance(4*time.Minute + 59*time.Second)
	now := f.clock.Now()
	c["iat"], c["exp"] = now.Unix(), now.Add(45*time.Second).Unix()
	_, err = f.verify(f.door.sign(currentKey, c))
	requireReason(t, err, trustedissuer.ReasonReplay)

	f.clock.Advance(2 * time.Second)
	now = f.clock.Now()
	c["iat"], c["exp"] = now.Unix(), now.Add(45*time.Second).Unix()
	_, err = f.verify(f.door.sign(currentKey, c))
	requireAccepted(t, err)
}

// --- the issuer's key list ---

func TestTheKeyListIsReadOnceInTenMinutes(t *testing.T) {
	f := newFixture(t)
	for i := range 10 {
		_, err := f.verify(f.door.sign(currentKey, f.good(t)))
		requireAccepted(t, err)
		if i < 9 {
			f.clock.Advance(time.Minute - time.Second)
		}
	}
	if got := f.door.fetches.Load(); got != 1 {
		t.Fatalf("expected one key-list read inside ten minutes, got %d", got)
	}
	f.clock.Advance(2 * time.Minute)
	_, err := f.verify(f.door.sign(currentKey, f.good(t)))
	requireAccepted(t, err)
	if got := f.door.fetches.Load(); got != 2 {
		t.Fatalf("expected the key list to be read again after ten minutes, got %d reads", got)
	}
}

func TestANewlyPublishedKeyIsAcceptedAfterOneRefetch(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	f.door.publish(currentKey, "door-2026-10")
	_, err := f.verify(f.door.sign("door-2026-10", f.good(t)))
	requireAccepted(t, err)
	if got := f.door.fetches.Load(); got != 1 {
		t.Fatalf("expected exactly one refetch, got %d", got)
	}
	// And the refreshed list is now the cached one: both keys verify without
	// another read.
	for _, kid := range []string{currentKey, "door-2026-10"} {
		_, err := f.verify(f.door.sign(kid, f.good(t)))
		requireAccepted(t, err)
	}
	if got := f.door.fetches.Load(); got != 1 {
		t.Fatalf("expected the refreshed list to be cached, got %d reads", got)
	}
}

func TestAShortRefetchNeverReplacesTheCachedKeys(t *testing.T) {
	shortFetches := map[string]func(d *testDoor){
		"empty key list":            func(d *testDoor) { d.setMode(doorPublishesNothing) },
		"failing key list":          func(d *testDoor) { d.setMode(doorFails) },
		"unreachable key list":      func(d *testDoor) { d.setMode(doorUnreachable) },
		"key list lacking that kid": func(d *testDoor) { d.publish("door-2026-12") },
	}
	for name, shorten := range shortFetches {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.prime(t)
			shorten(f.door)

			_, err := f.verify(f.door.sign("door-2026-11", f.good(t)))
			requireReason(t, err, trustedissuer.ReasonKidMiss)
			if got := f.door.fetches.Load(); got != 1 {
				t.Fatalf("expected one refetch for the unknown kid, got %d", got)
			}

			_, err = f.verify(f.door.sign(currentKey, f.good(t)))
			requireAccepted(t, err)
			if got := f.door.fetches.Load(); got != 1 {
				t.Fatalf("expected the cached key to answer without another read, got %d reads", got)
			}
		})
	}
}

func TestAShortRefetchNeverExtendsTheCachedKeys(t *testing.T) {
	f := newFixture(t)
	f.prime(t) // the key list is read at t=0

	f.clock.Advance(9 * time.Minute)
	f.door.publish("door-2026-12")
	_, err := f.verify(f.door.sign("door-2026-11", f.good(t)))
	requireReason(t, err, trustedissuer.ReasonKidMiss)

	// Eleven minutes after the only complete read: had the short refetch
	// reset the clock, this would be served from the cache with no read.
	f.clock.Advance(2 * time.Minute)
	f.door.publish(currentKey)
	_, err = f.verify(f.door.sign(currentKey, f.good(t)))
	requireAccepted(t, err)
	if got := f.door.fetches.Load(); got != 2 {
		t.Fatalf("expected the aged cache to be read again (2 reads), got %d", got)
	}
}

func TestAnAgedKeyListStillServesWhenTheIssuerCannotBeReached(t *testing.T) {
	for name, mode := range map[string]doorMode{
		"unreachable": doorUnreachable,
		"failing":     doorFails,
		"empty":       doorPublishesNothing,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.prime(t)
			f.clock.Advance(11 * time.Minute)
			f.door.setMode(mode)
			_, err := f.verify(f.door.sign(currentKey, f.good(t)))
			requireAccepted(t, err)
			if got := f.door.fetches.Load(); got != 1 {
				t.Fatalf("expected an attempt to refresh the aged list, got %d reads", got)
			}
		})
	}
}

func TestARefreshedKeyListRetiresAKeyTheIssuerDropped(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	f.clock.Advance(11 * time.Minute)
	f.door.publish("door-2026-10")

	_, err := f.verify(f.door.sign(currentKey, f.good(t)))
	requireReason(t, err, trustedissuer.ReasonKidMiss)
	_, err = f.verify(f.door.sign("door-2026-10", f.good(t)))
	requireAccepted(t, err)
	if got := f.door.fetches.Load(); got != 1 {
		t.Fatalf("expected the complete refresh to be cached, got %d reads", got)
	}
}

// --- reads caused by unknown key ids are limited ---

func TestInventedKeyIDsInsideTheMissIntervalCauseOneRead(t *testing.T) {
	t.Run("one after another", func(t *testing.T) {
		f := newFixture(t)
		f.prime(t)
		for i := range 20 {
			_, err := f.verify(f.door.sign(fmt.Sprintf("invented-%02d", i), f.good(t)))
			requireReason(t, err, trustedissuer.ReasonKidMiss)
			f.clock.Advance(time.Second) // 20 assertions across 20 seconds
		}
		if got := f.door.fetches.Load(); got != 1 {
			t.Fatalf("expected one key-list read for 20 invented kids inside %s, got %d",
				trustedissuer.KeyMissRefetchInterval, got)
		}
		// The cached key still answers, with no read.
		_, err := f.verify(f.door.sign(currentKey, f.good(t)))
		requireAccepted(t, err)
		if got := f.door.fetches.Load(); got != 1 {
			t.Fatalf("expected the cached key to answer without a read, got %d reads", got)
		}
	})

	t.Run("all at once", func(t *testing.T) {
		f := newFixture(t)
		f.prime(t)
		tokens := make([]string, 16)
		for i := range tokens {
			tokens[i] = f.door.sign(fmt.Sprintf("invented-%02d", i), f.good(t))
		}
		var wg sync.WaitGroup
		for _, token := range tokens {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := f.verify(token)
				var refusal *trustedissuer.Refusal
				if !errors.As(err, &refusal) || refusal.Reason != trustedissuer.ReasonKidMiss {
					t.Errorf("expected a kid-miss refusal, got %v", err)
				}
			}()
		}
		wg.Wait()
		if got := f.door.fetches.Load(); got != 1 {
			t.Fatalf("expected one key-list read for 16 concurrent invented kids, got %d", got)
		}
	})
}

func TestAKeyPublishedAfterTheMissIntervalIsAcceptedOnItsFirstAssertion(t *testing.T) {
	for name, c := range map[string]struct {
		configured time.Duration // 0 leaves the default
		interval   time.Duration
	}{
		"default interval":   {interval: trustedissuer.KeyMissRefetchInterval},
		"shortened interval": {configured: 5 * time.Second, interval: 5 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixtureWith(t, func(cfg *trustedissuer.Config) { cfg.KeyMissRefetchInterval = c.configured })
			f.prime(t)

			_, err := f.verify(f.door.sign("invented-01", f.good(t)))
			requireReason(t, err, trustedissuer.ReasonKidMiss)
			if got := f.door.fetches.Load(); got != 1 {
				t.Fatalf("expected the first unknown kid to cause one read, got %d", got)
			}

			// Just inside the interval, an unknown kid causes no read.
			f.clock.Advance(c.interval - time.Second)
			_, err = f.verify(f.door.sign("invented-02", f.good(t)))
			requireReason(t, err, trustedissuer.ReasonKidMiss)
			if got := f.door.fetches.Load(); got != 1 {
				t.Fatalf("expected no read inside the interval, got %d reads", got)
			}

			// Once the interval has passed, the issuer rotates: it publishes
			// the new key, then signs with it.
			f.clock.Advance(time.Second)
			f.door.publish(currentKey, "door-2026-10")
			_, err = f.verify(f.door.sign("door-2026-10", f.good(t)))
			requireAccepted(t, err)
			if got := f.door.fetches.Load(); got != 2 {
				t.Fatalf("expected the rotated key to cause one read, got %d reads", got)
			}
		})
	}
}

func TestAMissReadLackingTheKeyLeavesTheCacheUntouched(t *testing.T) {
	f := newFixture(t)
	f.prime(t) // the only complete read the cache holds, at t=0
	f.door.publish("door-2026-12")

	missUntouched := func(step string, wantReads int32) {
		t.Helper()
		_, err := f.verify(f.door.sign("door-2026-11", f.good(t)))
		requireReason(t, err, trustedissuer.ReasonKidMiss)
		if got := f.door.fetches.Load(); got != wantReads {
			t.Fatalf("%s: expected %d reads after the unknown kid, got %d", step, wantReads, got)
		}
		_, err = f.verify(f.door.sign(currentKey, f.good(t)))
		requireAccepted(t, err)
		if got := f.door.fetches.Load(); got != wantReads {
			t.Fatalf("%s: expected the cached key to answer without a read, got %d reads", step, got)
		}
	}

	f.clock.Advance(time.Minute)
	missUntouched("first miss", 1)
	f.clock.Advance(10 * time.Second)
	missUntouched("throttled miss", 1)
	f.clock.Advance(trustedissuer.KeyMissRefetchInterval)
	missUntouched("miss after the interval", 2)

	// Ten minutes after the prime, not after either miss read: had a short
	// read reset the clock, the cache would answer with no read.
	f.clock.Advance(trustedissuer.KeyListTTL - time.Minute - 10*time.Second - trustedissuer.KeyMissRefetchInterval)
	f.door.publish(currentKey)
	_, err := f.verify(f.door.sign(currentKey, f.good(t)))
	requireAccepted(t, err)
	if got := f.door.fetches.Load(); got != 3 {
		t.Fatalf("expected the aged cache to be read again (3 reads), got %d", got)
	}
}

func TestTheKeyListTTLRefreshIgnoresTheMissThrottle(t *testing.T) {
	for name, kid := range map[string]string{
		"an assertion signed with a cached key":          currentKey,
		"an assertion signed with a newly published key": "door-2026-10",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.prime(t) // t=0

			f.clock.Advance(trustedissuer.KeyListTTL - 15*time.Second) // 9m45s
			_, err := f.verify(f.door.sign("invented-01", f.good(t)))
			requireReason(t, err, trustedissuer.ReasonKidMiss)
			f.clock.Advance(5 * time.Second)
			_, err = f.verify(f.door.sign("invented-02", f.good(t)))
			requireReason(t, err, trustedissuer.ReasonKidMiss)
			if got := f.door.fetches.Load(); got != 1 {
				t.Fatalf("expected the throttle to hold before the TTL, got %d reads", got)
			}

			// 10m05s: the cache has aged out while the throttle is still
			// holding (20 s since the miss read).
			f.clock.Advance(15 * time.Second)
			f.door.publish(currentKey, "door-2026-10")
			_, err = f.verify(f.door.sign(kid, f.good(t)))
			requireAccepted(t, err)
			if got := f.door.fetches.Load(); got != 2 {
				t.Fatalf("expected the aged list to be refreshed, got %d reads", got)
			}
		})
	}
}

func TestKeysThatAreNotP256SigningKeysAreIgnored(t *testing.T) {
	f := newFixture(t)
	good := publicJWK(t, currentKey, &f.door.key(currentKey).PublicKey)
	mutated := func(k, v string) map[string]string {
		m := map[string]string{}
		for key, val := range good {
			m[key] = val
		}
		m["kid"] = "bad-" + k
		m[k] = v
		return m
	}
	list := map[string]any{"keys": []any{
		mutated("crv", "P-384"),
		mutated("kty", "RSA"),
		mutated("alg", "HS256"),
		mutated("use", "enc"),
		mutated("x", "AAAA"),
		mutated("y", base64.RawURLEncoding.EncodeToString(make([]byte, 32))),
		good,
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(list)
	}))
	t.Cleanup(server.Close)
	v := trustedissuer.NewVerifier(trustedissuer.Config{
		Issuer: testIssuer, JWKSURL: server.URL, Audience: testAudience,
		Providers: []string{"google"}, Now: f.clock.Now, HTTPClient: server.Client(),
	})
	for _, bad := range []string{"crv", "kty", "alg", "use", "x", "y"} {
		_, err := v.Verify(context.Background(), signES256(t, f.door.key(currentKey), "bad-"+bad, f.good(t)))
		requireReason(t, err, trustedissuer.ReasonKidMiss)
	}
	_, err := v.Verify(context.Background(), signES256(t, f.door.key(currentKey), currentKey, f.good(t)))
	requireAccepted(t, err)
}

func TestConcurrentVerificationIsRaceFree(t *testing.T) {
	f := newFixture(t)
	f.door.publish(currentKey, "door-2026-10")
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kid := currentKey
			if i%2 == 1 {
				kid = "door-2026-10"
			}
			if _, err := f.verify(f.door.sign(kid, goodClaims(t, f.clock.Now()))); err != nil {
				t.Errorf("verify %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
}

// --- what the verifier writes down ---

func TestTheVerifierNeverLogsAnAssertion(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	f.door.setMode(doorUnreachable)
	var tokens []string
	for _, kid := range []string{"door-2026-11", currentKey} {
		token := f.door.sign(kid, f.good(t))
		tokens = append(tokens, token)
		_, _ = f.verify(token)
	}
	logged := f.logs.text()
	if !strings.Contains(logged, "key list") {
		t.Fatalf("expected the short key-list read to be logged, got:\n%s", logged)
	}
	for _, token := range tokens {
		for _, part := range strings.Split(token, ".") {
			if part != "" && strings.Contains(logged, part) {
				t.Fatalf("the log carries part of an assertion:\n%s", logged)
			}
		}
	}
}

// hugeKeyID is a kid of 1 MiB whose every 17-character run is unique to it, so
// a log that carries any part of it past a 16-character prefix is caught.
func hugeKeyID() string {
	var b strings.Builder
	for b.Len() < 1<<20 {
		fmt.Fprintf(&b, "kid-%08d.", b.Len())
	}
	return b.String()
}

func TestACallerChosenKeyIDNeverReachesARefusalAndReachesTheLogOnlyAsAShortPrefix(t *testing.T) {
	huge := hugeKeyID()
	paths := map[string]func(t *testing.T, f *fixture){
		"read for and not published": func(*testing.T, *fixture) {},
		"inside the miss interval": func(t *testing.T, f *fixture) {
			_, _ = f.verify(f.door.sign("invented-01", f.good(t)))
		},
		"key list unreachable": func(_ *testing.T, f *fixture) { f.door.setMode(doorUnreachable) },
		"key list empty":       func(_ *testing.T, f *fixture) { f.door.setMode(doorPublishesNothing) },
		"cache aged and key list unreachable": func(_ *testing.T, f *fixture) {
			f.clock.Advance(trustedissuer.KeyListTTL + time.Second)
			f.door.setMode(doorUnreachable)
		},
	}
	for name, stage := range paths {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.prime(t)
			stage(t, f)
			f.logs.reset()

			token := f.door.sign(huge, f.good(t))
			_, err := f.verify(token)
			var refusal *trustedissuer.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("expected a refusal, got %v", err)
			}
			if strings.Contains(refusal.Detail, huge[:4]) || len(refusal.Detail) > 256 {
				t.Fatalf("the refusal's detail carries the caller's kid (%d bytes): %.200q", len(refusal.Detail), refusal.Detail)
			}

			logged := f.logs.text()
			if len(logged) > 2048 {
				t.Fatalf("a 1 MiB kid produced %d bytes of log", len(logged))
			}
			if strings.Contains(logged, huge[:17]) {
				t.Fatalf("the log carries the kid past a 16-character prefix:\n%.400s", logged)
			}
			header := strings.Split(token, ".")[0]
			if strings.Contains(logged, header[:64]) {
				t.Fatalf("the log carries the assertion's base64 header:\n%.400s", logged)
			}
		})
	}
}

func TestALoggedKeyIDIsPrintableASCIIOfAtMostSixteenCharacters(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	f.logs.reset()
	kid := "door\n2026\x1b[31m-10é-rotated-and-long"
	_, err := f.verify(f.door.sign(kid, f.good(t)))
	requireReason(t, err, trustedissuer.ReasonKidMiss)

	logged := f.logs.all()
	if len(logged) == 0 {
		t.Fatal("expected the miss read to be logged")
	}
	for _, line := range logged {
		for i := 0; i+1 < len(line.fields); i += 2 {
			if line.fields[i] != "kid" {
				continue
			}
			value, _ := line.fields[i+1].(string)
			if len(value) > 16 {
				t.Fatalf("logged kid is %d characters, want at most 16: %q", len(value), value)
			}
			for _, r := range value {
				if r < 0x20 || r > 0x7e {
					t.Fatalf("logged kid carries a character that is not printable ASCII: %q", value)
				}
			}
			if !strings.HasPrefix(value, "door?2026?[31m-1") {
				t.Fatalf("logged kid = %q, want the sanitized prefix of the kid", value)
			}
		}
	}
}

// --- the key-list address ---

func TestCheckJWKSURL(t *testing.T) {
	cases := map[string]bool{
		"https://money.weos.cloud/door/jwks.json": true,
		"http://127.0.0.1:8123/door/jwks.json":    true,
		"http://localhost:8123/door/jwks.json":    true,
		"http://[::1]:8123/door/jwks.json":        true,
		"http://money.weos.cloud/door/jwks.json":  false,
		"http://10.0.0.8/door/jwks.json":          false,
		"ftp://money.weos.cloud/jwks.json":        false,
		"money.weos.cloud/door/jwks.json":         false,
		"https:///door/jwks.json":                 false,
		"":                                        false,
	}
	for raw, ok := range cases {
		err := trustedissuer.CheckJWKSURL(raw)
		if ok && err != nil {
			t.Errorf("%q: expected usable, got %v", raw, err)
		}
		if !ok && err == nil {
			t.Errorf("%q: expected refused, was accepted", raw)
		}
	}
}
