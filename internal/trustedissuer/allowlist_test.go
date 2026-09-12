package trustedissuer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/trustedissuer"
)

const listedEmail = "dana.whitfield@harborlegal.example"

// allowlisted builds a fixture whose verifier enforces the given allowlist.
func allowlisted(t *testing.T, allowed ...string) *fixture {
	t.Helper()
	return newFixtureWith(t, func(c *trustedissuer.Config) { c.AllowedEmails = allowed })
}

func TestTheAllowlistRefusesAnEmailItDoesNotName(t *testing.T) {
	f := allowlisted(t, listedEmail)
	_, err := f.verify(f.door.sign(currentKey, f.good(t))) // good claims carry opsEmail
	requireReason(t, err, trustedissuer.ReasonAllowlist)
}

func TestTheAllowlistAdmitsTheEmailsItNames(t *testing.T) {
	f := allowlisted(t, "marcus.okafor@harborlegal.example", listedEmail)
	c := f.good(t)
	c["email"] = listedEmail
	id, err := f.verify(f.door.sign(currentKey, c))
	requireAccepted(t, err)
	if id.Email != listedEmail {
		t.Fatalf("identity email = %q, want %q", id.Email, listedEmail)
	}
}

// The OAuth callback's rule: both sides trimmed and lowercased, then an exact
// match. Nothing else about an address is forgiven.
func TestTheAllowlistComparesAsTheOAuthCallbackDoes(t *testing.T) {
	cases := map[string]struct {
		listed, presented string
		admitted          bool
	}{
		"same capitals":                {listedEmail, listedEmail, true},
		"presented in other capitals":  {listedEmail, "Dana.Whitfield@HarborLegal.example", true},
		"listed in other capitals":     {"Dana.Whitfield@HarborLegal.example", listedEmail, true},
		"listed with spaces around it": {"  " + listedEmail + " ", listedEmail, true},
		"presented with spaces":        {listedEmail, " " + listedEmail + " ", true},
		"a plus-address":               {listedEmail, "dana.whitfield+door@harborlegal.example", false},
		"the same name at another tld": {listedEmail, "dana.whitfield@harborlegal.example.net", false},
		"a shorter local part":         {listedEmail, "whitfield@harborlegal.example", false},
		"a listed prefix":              {"dana", listedEmail, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := allowlisted(t, c.listed)
			claims := f.good(t)
			claims["email"] = c.presented
			_, err := f.verify(f.door.sign(currentKey, claims))
			if c.admitted {
				requireAccepted(t, err)
				return
			}
			requireReason(t, err, trustedissuer.ReasonAllowlist)
		})
	}
}

func TestAnEmptyAllowlistAdmitsEveryone(t *testing.T) {
	for name, allowed := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			f := newFixtureWith(t, func(c *trustedissuer.Config) { c.AllowedEmails = allowed })
			_, err := f.verify(f.door.sign(currentKey, f.good(t)))
			requireAccepted(t, err)
		})
	}
}

// The allowlist is the last check, after the jti is spent: presenting a
// refused assertion again is a replay, not a second allowlist refusal.
func TestAnAllowlistRefusalHasAlreadySpentTheJTI(t *testing.T) {
	f := allowlisted(t, listedEmail)
	token := f.door.sign(currentKey, f.good(t))
	_, err := f.verify(token)
	requireReason(t, err, trustedissuer.ReasonAllowlist)
	_, err = f.verify(token)
	requireReason(t, err, trustedissuer.ReasonReplay)
}

// Every check before the allowlist names its own reason for an assertion the
// allowlist would also refuse: the first failing check is the one named.
func TestEveryEarlierCheckIsNamedBeforeTheAllowlist(t *testing.T) {
	cases := map[string]struct {
		mutate func(now time.Time, c claims)
		want   trustedissuer.Reason
	}{
		"wrong issuer":   {func(_ time.Time, c claims) { c["iss"] = "https://door.cedarrealty.example" }, trustedissuer.ReasonIssuer},
		"wrong audience": {func(_ time.Time, c claims) { c["aud"] = "9f8e7d6c" }, trustedissuer.ReasonAudience},
		"expired": {func(now time.Time, c claims) {
			c["iat"], c["exp"] = now.Add(-2*time.Minute).Unix(), now.Add(-time.Minute).Unix()
		}, trustedissuer.ReasonExpired},
		"minted too long":   {func(now time.Time, c claims) { c["exp"] = now.Add(10 * time.Minute).Unix() }, trustedissuer.ReasonWindow},
		"unverified email":  {func(_ time.Time, c claims) { c["email_verified"] = false }, trustedissuer.ReasonClaims},
		"no subject":        {func(_ time.Time, c claims) { delete(c, "sub") }, trustedissuer.ReasonClaims},
		"unknown provider":  {func(_ time.Time, c claims) { c["provider"] = "okta" }, trustedissuer.ReasonClaims},
		"no jti":            {func(_ time.Time, c claims) { delete(c, "jti") }, trustedissuer.ReasonClaims},
		"email not on list": {func(time.Time, claims) {}, trustedissuer.ReasonAllowlist},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := allowlisted(t, listedEmail)
			claims := f.good(t)
			c.mutate(f.clock.Now(), claims)
			_, err := f.verify(f.door.sign(currentKey, claims))
			requireReason(t, err, c.want)
		})
	}

	t.Run("bad signature", func(t *testing.T) {
		f := allowlisted(t, listedEmail)
		_, err := f.verify("not-a-jwt")
		requireReason(t, err, trustedissuer.ReasonSignature)
	})
}

// A refusal for any check before the allowlist does not spend the jti, so the
// corrected assertion's only remaining problem is the allowlist.
func TestARefusalBeforeTheAllowlistLeavesTheJTIUnspent(t *testing.T) {
	f := allowlisted(t, listedEmail)
	c := f.good(t)
	c["email_verified"] = false
	_, err := f.verify(f.door.sign(currentKey, c))
	requireReason(t, err, trustedissuer.ReasonClaims)
	c["email_verified"] = true
	_, err = f.verify(f.door.sign(currentKey, c))
	requireReason(t, err, trustedissuer.ReasonAllowlist)
}

func TestAnAllowlistRefusalNeverCarriesTheEmail(t *testing.T) {
	f := allowlisted(t, listedEmail)
	_, err := f.verify(f.door.sign(currentKey, f.good(t)))
	requireReason(t, err, trustedissuer.ReasonAllowlist)
	if got := err.Error(); strings.Contains(strings.ToLower(got), strings.ToLower(opsEmail)) {
		t.Fatalf("the refusal carries the presented email: %s", got)
	}
}
