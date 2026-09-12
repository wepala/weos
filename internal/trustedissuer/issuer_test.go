package trustedissuer_test

import (
	"testing"

	"github.com/wepala/weos/v3/internal/trustedissuer"
)

// An operator may write the issuer with a trailing slash and the door may mint
// iss without one, or the other way round. Neither changes the address, so
// neither refuses the door's assertions.
func TestVerifyAcceptsAnIssuerThatDiffersOnlyByTrailingSlashes(t *testing.T) {
	cases := map[string]struct{ configured, claimed string }{
		"a slash on the claim":       {testIssuer, testIssuer + "/"},
		"a slash on the setting":     {testIssuer + "/", testIssuer},
		"a slash on both":            {testIssuer + "/", testIssuer + "/"},
		"two slashes on the claim":   {testIssuer, testIssuer + "//"},
		"spaces around the setting":  {" " + testIssuer + "/ ", testIssuer},
		"the same address, no slash": {testIssuer, testIssuer},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixtureWith(t, func(cfg *trustedissuer.Config) { cfg.Issuer = c.configured })
			claims := f.good(t)
			claims["iss"] = c.claimed
			_, err := f.verify(f.door.sign(currentKey, claims))
			requireAccepted(t, err)
		})
	}
}

func TestVerifyRefusesAnIssuerThatDiffersByMoreThanTrailingSlashes(t *testing.T) {
	for name, claimed := range map[string]string{
		"a path added":                  testIssuer + "/fleet",
		"a longer host":                 testIssuer + ".example",
		"a trailing space":              testIssuer + " ",
		"only a slash":                  "/",
		"empty":                         "",
		"another scheme":                "http://money.weos.cloud",
		"a slash-trimmed prefix of it":  "https://money.weos",
		"the address with a query mark": testIssuer + "?",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			claims := f.good(t)
			claims["iss"] = claimed
			_, err := f.verify(f.door.sign(currentKey, claims))
			requireReason(t, err, trustedissuer.ReasonIssuer)
		})
	}
}

// A verifier configured with no issuer trusts no iss at all, even an empty or
// slash-only one.
func TestVerifyWithNoIssuerRefusesEveryIssuer(t *testing.T) {
	for _, configured := range []string{"", "/", " // "} {
		for _, claimed := range []string{"", "/", testIssuer} {
			f := newFixtureWith(t, func(cfg *trustedissuer.Config) { cfg.Issuer = configured })
			claims := f.good(t)
			claims["iss"] = claimed
			_, err := f.verify(f.door.sign(currentKey, claims))
			requireReason(t, err, trustedissuer.ReasonIssuer)
		}
	}
}

func TestCheckIssuerURL(t *testing.T) {
	cases := map[string]bool{
		"https://money.weos.cloud":           true,
		"https://door.example/fleet":         true,
		"https://money.weos.cloud:8443":      true,
		"HTTPS://money.weos.cloud":           true,
		"http://127.0.0.1:9443":              true,
		"http://localhost:9443":              true,
		"http://[::1]:9443":                  true,
		"http://money.weos.cloud":            false,
		"money.weos.cloud":                   false,
		"https:":                             false,
		"https://":                           false,
		"https:///door":                      false,
		"https://door.example?tenant=x":      false,
		"https://door.example?":              false,
		"https://door.example#start":         false,
		"https://door.example#":              false,
		"https://ops:secret@door.example":    false,
		"https://ops@door.example":           false,
		"ftp://door.example":                 false,
		"mailto:ops@harborlegal.example":     false,
		"":                                   false,
		"/door":                              false,
		"https://door.example/a b":           false,
		"https://door.example/%zz":           false,
		"javascript://door.example/%0Aalert": false,
	}
	for raw, ok := range cases {
		t.Run(raw, func(t *testing.T) {
			err := trustedissuer.CheckIssuerURL(raw)
			if (err == nil) != ok {
				t.Fatalf("CheckIssuerURL(%q) = %v, want accepted %v", raw, err, ok)
			}
			if err != nil && raw != "" && len(raw) > 8 && containsValue(err.Error(), raw) {
				t.Fatalf("the error repeats the value: %v", err)
			}
		})
	}
}

func containsValue(message, value string) bool {
	for i := 0; i+len(value) <= len(message); i++ {
		if message[i:i+len(value)] == value {
			return true
		}
	}
	return false
}
