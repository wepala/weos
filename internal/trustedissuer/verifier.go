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
	// 200, undecodable, or redirecting — now or less than
	// KeyMissRefetchInterval ago, or the request ended while it waited for a
	// read. Publishing a key does not fix it; reaching the list does.
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
	// email, provider (a registry key, verbatim), email_verified == true.
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
	// assertions naming a key the fresh cached list lacks.
	KeyMissRefetchInterval = 30 * time.Second
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

// Refusal is the error Verify returns for every assertion it does not accept.
// Detail explains the refusal to an operator. It never contains the assertion
// or any part of it.
type Refusal struct {
	Reason Reason
	Detail string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("login assertion refused (%s): %s", r.Reason, r.Detail)
}

func refuse(reason Reason, detail string) (Identity, error) {
	return Identity{}, &Refusal{Reason: reason, Detail: detail}
}

// Identity is what an accepted assertion says about the person.
type Identity struct {
	// Subject is the provider's stable id for the person (sub).
	Subject string
	// Email is the person's door-account email.
	Email string
	// Provider is the registry key of the provider that verified the person.
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
	// Providers are the provider claim values accepted, verbatim — core's
	// OAuth registry keys.
	Providers []string
	// AllowedEmails is the instance's identity allowlist
	// (OAUTH_ALLOWED_EMAILS). When it has any entry, an assertion whose email
	// it does not name is refused as allowlist, after every other check and
	// after the jti is spent. The comparison is the OAuth callback's: both
	// sides trimmed and lowercased, then an exact match. Empty admits every
	// email, as the callback does.
	AllowedEmails []string

	// KeyMissRefetchInterval is the shortest time between two key-list reads
	// caused by an unknown kid, instance-wide. Inside it, an assertion naming
	// a key neither the fresh cached list nor the last miss read holds is
	// refused as kid-miss with no read. It is also how long reads back off
	// after one fails or holds no usable key. Optional; zero or less means
	// KeyMissRefetchInterval.
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
	var allowed map[string]struct{}
	if len(cfg.AllowedEmails) > 0 {
		allowed = make(map[string]struct{}, len(cfg.AllowedEmails))
		for _, e := range cfg.AllowedEmails {
			allowed[normalizeEmail(e)] = struct{}{}
		}
	}
	return &Verifier{
		issuer:    cfg.Issuer,
		audience:  cfg.Audience,
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
			return refuse(keys.reason, keys.why)
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

	if iss, err := c.GetIssuer(); err != nil || iss != v.issuer {
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

// normalizeEmail is the OAuth callback's normalization: trimmed and
// lowercased, nothing else.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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
