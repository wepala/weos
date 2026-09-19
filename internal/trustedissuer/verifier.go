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

// Package trustedissuer verifies a login assertion: a short-lived ES256 JWT in
// which the one issuer an instance trusts — a fleet's front door — states that
// it has verified a person. The instance never talks to Google or Apple; it
// checks the issuer's signature against the issuer's published key list and
// then every claim that decides whether the statement is still good, and for
// this instance.
//
// See docs/decisions/trusted-issuer-login-assertion.md.
package trustedissuer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// Reason is the machine-readable name of the check that refused an assertion.
// It is what a refusal's body and log line carry, so the door can tell a clock
// problem it should retry from a key problem it must fix by publishing.
type Reason string

const (
	// ReasonSignature: not ES256-signed by a key the issuer publishes, or not a
	// JWT at all.
	ReasonSignature Reason = "signature"
	// ReasonKidMiss: the assertion names a key the issuer's key list does not
	// hold. The list was read for the key and lacks it, or answered with no
	// usable key, or was read for another unknown key less than
	// KeyMissRefetchInterval ago. The door fixes it by publishing the key.
	ReasonKidMiss Reason = "kid-miss"
	// ReasonKeysUnreachable: no cached key answers for the assertion, and the
	// issuer's key list could not be read — unreachable, answering other than
	// 200, undecodable, or redirecting — now or inside the backoff that
	// followed. Publishing a key does not fix it; reaching the list does. A
	// request that ended while it waited for a read is also answered with it;
	// that refusal wraps the request's own context error (see
	// Refusal.Unwrap), because nothing is wrong with the key list.
	ReasonKeysUnreachable Reason = "keys-unreachable"
	// ReasonIssuer: iss is not exactly the trusted issuer.
	ReasonIssuer Reason = "iss"
	// ReasonAudience: aud is not exactly this instance's audience.
	ReasonAudience Reason = "aud"
	// ReasonExpired: exp has passed, beyond the clock allowance.
	ReasonExpired Reason = "expired"
	// ReasonWindow: the assertion is minted to live longer than MaxLifetime,
	// is issued further ahead than the clock allowance, or does not say when
	// it was issued and when it expires.
	ReasonWindow Reason = "window"
	// ReasonReplay: the assertion's jti was already presented.
	ReasonReplay Reason = "jti-replay"
	// ReasonClaims: a required claim is missing or unacceptable — jti, sub,
	// email, provider (verbatim, one of Config.Providers), email_verified == true,
	// or a purpose other than the one the route the assertion was presented to
	// serves (see Config.Purpose). A purpose that is present but is not text,
	// or is text naming nothing, is refused here too rather than read as the
	// absent claim that means PurposeLogin.
	ReasonClaims Reason = "claims"
	// ReasonAllowlist: the instance has an identity allowlist
	// (OAUTH_ALLOWED_EMAILS) and it does not name the assertion's email. It is
	// the last check, so the assertion's jti is already spent.
	ReasonAllowlist Reason = "allowlist"
)

const (
	// KeyListTTL is how long a complete read of the issuer's key list is used
	// before it is read again.
	KeyListTTL = 10 * time.Minute
	// KeyMissRefetchInterval is the default for Config.KeyMissRefetchInterval:
	// the shortest time between two reads of the key list caused by
	// assertions naming a key the fresh cached list lacks. It is also the
	// longest a run of failed key-list reads backs off.
	KeyMissRefetchInterval = 30 * time.Second
	// KeyListFirstRetry is how long key-list reads back off after the first
	// failed read in a run. Each further failure in a row doubles it, up to
	// the miss interval, and a complete read ends the run.
	KeyListFirstRetry = 2 * time.Second
	// ReplayMemory is how long a presented jti is remembered. It outlives any
	// assertion that could still be valid: MaxLifetime plus the allowance on
	// both ends is two minutes.
	ReplayMemory = 5 * time.Minute
	// ClockLeeway is how far the instance's clock and the issuer's may
	// disagree, applied to exp against now and to iat against now.
	ClockLeeway = 30 * time.Second
	// MaxLifetime is the longest an assertion may be minted to live, exp − iat.
	// Both come from the issuer's one clock, so no allowance applies to it.
	MaxLifetime = 60 * time.Second
)

// What an assertion asks the instance to do, carried in its purpose claim.
// One issuer, one key list and one audience serve every route that takes an
// assertion, so the claim is what keeps them apart: an assertion minted to end
// a person's token access can never sign that person in, and one minted to
// sign a person in can never end their token access. Without it, an assertion
// captured on its way to one route would work at the other.
const (
	// PurposeLogin is what POST /api/auth/assert serves. An assertion carrying
	// no purpose claim asks for it: the claim was added after the route, so an
	// issuer minting the login assertion the contract has always described
	// keeps working unchanged. Only an ABSENT claim asks for it — a claim that
	// is there but is not text, or is text naming nothing, is refused.
	PurposeLogin = "login"
	// PurposeRevokeTokens is what POST /api/auth/revoke-tokens serves: end
	// every refresh token of the person the assertion names. It is never
	// implied — an assertion must carry it.
	PurposeRevokeTokens = "revoke-tokens"
)

// Refusal is the error Verify returns for every assertion it does not accept.
// Detail explains the refusal to an operator. It never contains the assertion
// or any part of it.
type Refusal struct {
	Reason Reason
	Detail string
	// cause is the request's context error when the request ended while it
	// waited for a key-list read; nil for every other refusal.
	cause error
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("login assertion refused (%s): %s", r.Reason, r.Detail)
}

// Unwrap returns the request's context error when the refusal was made because
// the request ended while it waited for a key-list read, and nil otherwise. A
// caller that finds its own request's context error in a refusal knows the
// request went away and the key list did not fail.
func (r *Refusal) Unwrap() error { return r.cause }

func refuse(reason Reason, detail string) (Identity, error) {
	return Identity{}, &Refusal{Reason: reason, Detail: detail}
}

// Identity is what an accepted assertion says about the person.
type Identity struct {
	// Subject is the provider's stable id for the person (sub).
	Subject string
	// Email is the person's door-account email.
	Email string
	// Provider is the key of the provider that verified the person: a registry
	// key such as "google", or "door" for an identity the door owns itself.
	Provider string
	// Name is optional.
	Name string
	// JTI is the assertion's single-use id, already spent.
	JTI string
}

// Config configures a Verifier.
type Config struct {
	// Issuer is the exact iss value accepted (TRUSTED_ISSUER).
	Issuer string
	// JWKSURL is where the issuer publishes its key list
	// (TRUSTED_ISSUER_JWKS_URL). See CheckJWKSURL.
	JWKSURL string
	// Audience is the exact aud value accepted (TRUSTED_ISSUER_AUDIENCE).
	Audience string
	// Purpose is what an accepted assertion must ask for: PurposeLogin or
	// PurposeRevokeTokens. An assertion asking for anything else is refused as
	// claims, and so is one asking for nothing where PurposeLogin is not what
	// is wanted. Optional; empty means PurposeLogin.
	Purpose string
	// Providers are the provider claim values accepted, verbatim — the keys
	// application.OAuthProviderKeys lists: core's OAuth registry keys, plus
	// "door", which no registry entry holds and which names an email+password
	// identity the door owns. Every key, "door" included, is accepted only with
	// email_verified true.
	Providers []string
	// AllowedEmails is the instance's identity allowlist
	// (OAUTH_ALLOWED_EMAILS). When it has any entry, an assertion whose email
	// it does not name is refused as allowlist, after every other check and
	// after the jti is spent. Both the entries and the email are folded with
	// owner binding's rule (repositories.FoldCredentialEmail: spaces trimmed,
	// ASCII capitals lower-cased, nothing else), then matched exactly, so an
	// address the allowlist admits is one binding can match. Empty admits
	// every email, as the callback does.
	AllowedEmails []string

	// KeyMissRefetchInterval is the shortest time between two key-list reads
	// caused by an unknown kid, instance-wide. Inside it, an assertion naming
	// a key neither the fresh cached list nor the last miss read holds is
	// refused as kid-miss with no read. It is also the longest reads back off
	// after a run of reads that failed or held no usable key; the first
	// failure in a run backs off KeyListFirstRetry. Optional; zero or less
	// means KeyMissRefetchInterval.
	KeyMissRefetchInterval time.Duration

	// HTTPClient reads the key list. Optional. The verifier reads through a
	// copy that follows no redirect, whatever this client's CheckRedirect.
	HTTPClient *http.Client
	// Now is the instance's clock. Optional; time.Now.
	Now func() time.Time
	// Logger receives key-list problems. Optional. The verifier never logs an
	// assertion.
	Logger entities.Logger
}

// Verifier checks login assertions. It is safe for concurrent use.
//
// The jti memory is held in process: it is reset when the instance restarts.
// That is acceptable because no assertion outlives MaxLifetime plus the clock
// allowance, so a restart cannot bring back one that is still valid for long.
type Verifier struct {
	issuer    string
	audience  string
	purpose   string
	providers map[string]struct{}
	// allowed is the normalized allowlist; nil when the instance has none.
	allowed map[string]struct{}
	now     func() time.Time
	parser  *gojwt.Parser
	keys    *keyList
	spent   *replayMemory
}

// NewVerifier builds a Verifier. It reads nothing until the first assertion.
func NewVerifier(cfg Config) *Verifier {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	client := &http.Client{Timeout: keyListFetchTimeout}
	if cfg.HTTPClient != nil {
		supplied := *cfg.HTTPClient
		client = &supplied
	}
	// A copy, so the caller's client keeps its own redirect policy.
	client.CheckRedirect = refuseRedirect
	logger := cfg.Logger
	if logger == nil {
		logger = nopLogger{}
	}
	missInterval := cfg.KeyMissRefetchInterval
	if missInterval <= 0 {
		missInterval = KeyMissRefetchInterval
	}
	providers := make(map[string]struct{}, len(cfg.Providers))
	for _, p := range cfg.Providers {
		providers[p] = struct{}{}
	}
	purpose := cfg.Purpose
	if purpose == "" {
		purpose = PurposeLogin
	}
	var allowed map[string]struct{}
	if len(cfg.AllowedEmails) > 0 {
		allowed = make(map[string]struct{}, len(cfg.AllowedEmails))
		for _, e := range cfg.AllowedEmails {
			allowed[normalizeEmail(e)] = struct{}{}
		}
	}
	return &Verifier{
		// Without trailing slashes, as checkClaims compares iss.
		issuer:    strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/"),
		audience:  cfg.Audience,
		purpose:   purpose,
		providers: providers,
		allowed:   allowed,
		now:       now,
		// Claims are checked below, one by one, so each refusal names the
		// check that failed; the parser only establishes the signature.
		parser: gojwt.NewParser(
			gojwt.WithValidMethods([]string{gojwt.SigningMethodES256.Alg()}),
			gojwt.WithoutClaimsValidation(),
		),
		keys: &keyList{
			url: cfg.JWKSURL, client: client, now: now, logger: logger,
			missInterval: missInterval,
			firstRetry:   KeyListFirstRetry,
			fetchSlot:    make(chan struct{}, 1),
		},
		spent: newReplayMemory(),
	}
}

// Verify accepts an assertion and returns who it names, or returns a *Refusal
// naming the one check that refused it. An accepted assertion's jti is spent:
// presenting it again is refused.
func (v *Verifier) Verify(ctx context.Context, assertion string) (Identity, error) {
	if strings.TrimSpace(assertion) == "" {
		return refuse(ReasonSignature, "no assertion was presented")
	}
	claims := gojwt.MapClaims{}
	_, err := v.parser.ParseWithClaims(assertion, claims, func(tok *gojwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)
		if kid == "" {
			return nil, errNoKeyID
		}
		return v.keys.key(ctx, kid)
	})
	if err != nil {
		var keys *keyRefusal
		if errors.As(err, &keys) {
			return Identity{}, &Refusal{Reason: keys.reason, Detail: keys.why, cause: keys.cause}
		}
		return refuse(ReasonSignature, signatureDetail(err))
	}
	return v.checkClaims(claims)
}

// signatureDetail names the signature failure from a fixed set of phrases, so
// no text a parser derived from the assertion can reach a log line.
func signatureDetail(err error) string {
	switch {
	case errors.Is(err, errNoKeyID):
		return "the assertion names no signing key (kid)"
	case errors.Is(err, gojwt.ErrTokenMalformed):
		return "the assertion is not a well-formed JWT"
	case errors.Is(err, gojwt.ErrTokenSignatureInvalid):
		return "the assertion is not ES256-signed by the issuer's key"
	default:
		return "the assertion's signature cannot be verified"
	}
}

func (v *Verifier) checkClaims(c gojwt.MapClaims) (Identity, error) {
	now := v.now()

	// A trailing slash does not change the address an issuer names, so an iss
	// that differs from the trusted issuer only by trailing slashes is the
	// trusted issuer. v.issuer is stored without them.
	if iss, err := c.GetIssuer(); err != nil || v.issuer == "" || strings.TrimRight(iss, "/") != v.issuer {
		return refuse(ReasonIssuer, "the assertion was not issued by the trusted issuer")
	}
	// Exactly one audience: an assertion addressed to several instances could
	// sign a person into each of them, and jti memory is per instance.
	if aud, err := c.GetAudience(); err != nil || len(aud) != 1 || aud[0] != v.audience {
		return refuse(ReasonAudience, "the assertion is not addressed to this instance alone")
	}

	exp, expErr := c.GetExpirationTime()
	iat, iatErr := c.GetIssuedAt()
	if expErr != nil || exp == nil || iatErr != nil || iat == nil {
		return refuse(ReasonWindow, "the assertion does not state when it was issued and when it expires")
	}
	if now.After(exp.Add(ClockLeeway)) {
		return refuse(ReasonExpired, "the assertion has expired")
	}
	if exp.Before(iat.Time) || exp.Sub(iat.Time) > MaxLifetime {
		return refuse(ReasonWindow, "the assertion is minted to live longer than 60 seconds")
	}
	if iat.After(now.Add(ClockLeeway)) {
		return refuse(ReasonWindow, "the assertion is issued further ahead than the clock allowance")
	}

	jti := textClaim(c, "jti")
	subject := textClaim(c, "sub")
	email := textClaim(c, "email")
	provider := textClaim(c, "provider")
	switch {
	case jti == "":
		return refuse(ReasonClaims, "the assertion carries no jti")
	case subject == "":
		return refuse(ReasonClaims, "the assertion carries no subject")
	case email == "":
		return refuse(ReasonClaims, "the assertion carries no email")
	case provider == "":
		return refuse(ReasonClaims, "the assertion carries no provider")
	}
	if _, known := v.providers[provider]; !known {
		return refuse(ReasonClaims, "the assertion names a sign-in provider this instance does not know")
	}
	if verified, ok := c["email_verified"].(bool); !ok || !verified {
		return refuse(ReasonClaims, "the assertion does not mark the email address verified")
	}
	// An assertion that says nothing asks to sign its person in, which is what
	// every assertion asked for before the claim existed. Checked before the
	// jti is spent, like every other claim: an issuer that minted the wrong
	// kind corrects it and presents the same jti.
	purpose, asks := purposeClaim(c)
	if !asks {
		return refuse(ReasonClaims, "the assertion carries a purpose that names nothing this instance serves")
	}
	if purpose != v.purpose {
		return refuse(ReasonClaims, "the assertion does not ask for what this route does")
	}

	// Spent after every check except the allowlist, so an assertion refused
	// for a flaw the door can correct does not burn the jti a corrected retry
	// would carry.
	if !v.spent.remember(jti, now) {
		return refuse(ReasonReplay, "the assertion was already presented")
	}
	// After the jti is spent, so an assertion the allowlist refuses cannot be
	// presented again once the allowlist names its person. The detail stays a
	// fixed phrase: the email does not reach the log.
	if !v.admits(email) {
		return refuse(ReasonAllowlist, "the assertion's email is not on this instance's allowlist")
	}
	return Identity{
		Subject:  subject,
		Email:    email,
		Provider: provider,
		Name:     textClaim(c, "name"),
		JTI:      jti,
	}, nil
}

// admits reports whether the allowlist lets email in. An instance with no
// allowlist admits everyone.
func (v *Verifier) admits(email string) bool {
	if v.allowed == nil {
		return true
	}
	_, ok := v.allowed[normalizeEmail(email)]
	return ok
}

// normalizeEmail is owner binding's fold, repositories.FoldCredentialEmail:
// spaces trimmed from both ends and ASCII capitals lower-cased, nothing else.
// The allowlist folds both its entries and the assertion's email with it, so
// an address the allowlist admits is exactly one owner binding can match. An
// address that differs from an entry only in a non-ASCII capital, such as
// U+212A KELVIN SIGN for a k, is refused rather than admitted and then kept
// apart from its owner.
func normalizeEmail(email string) string {
	return repositories.FoldCredentialEmail(email)
}

// purposeClaim reads what an assertion asks for. An assertion with no purpose
// claim at all asks to sign its person in: the claim was added after the login
// route, so an issuer minting the login assertion the contract has always
// described keeps working unchanged.
//
// A claim that IS there and is not text — null, a number, an array, an object —
// or is text that names nothing, asks for NOTHING, and ok is false. It is not
// read as an absent claim: the claim's whole job is to keep the two routes
// apart, so a malformed one must not fall back to the route with the wider
// reach. Otherwise an assertion the door minted to end a person's access, with
// a purpose its JSON mangled, would sign that person in instead (Copilot,
// PR #573).
//
// Kept apart from textClaim because textClaim reads iss, sub, email and the
// rest too: it folds "absent" and "the wrong type" together on purpose, and
// widening it there would change refusals that have nothing to do with this
// claim.
func purposeClaim(c gojwt.MapClaims) (string, bool) {
	raw, present := c["purpose"]
	if !present {
		return PurposeLogin, true
	}
	text, isText := raw.(string)
	if !isText || strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// textClaim reads a string claim. A missing claim, one of another type, and
// one that is only whitespace all read as absent.
func textClaim(c gojwt.MapClaims, key string) string {
	s, _ := c[key].(string)
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

type nopLogger struct{}

func (nopLogger) Debug(context.Context, string, ...interface{}) {}
func (nopLogger) Info(context.Context, string, ...interface{})  {}
func (nopLogger) Warn(context.Context, string, ...interface{})  {}
func (nopLogger) Error(context.Context, string, ...interface{}) {}
