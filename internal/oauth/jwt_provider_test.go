package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
)

// testSigningKeyPEM is a JWT_SIGNING_KEY value: a PEM-encoded RSA private key.
func testSigningKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate the signing key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// tokenSurvivesRestart issues a token from the service built for cfg and
// checks it with a second service built for the same cfg, as a restarted
// instance would.
func tokenSurvivesRestart(t *testing.T, cfg config.Config) bool {
	t.Helper()
	ctx := context.Background()
	before, err := ProvideJWTService(cfg)
	if err != nil {
		t.Fatalf("build the service before the restart: %v", err)
	}
	agent, err := new(entities.Agent).With("agent-dana", "Dana Whitfield", entities.AgentTypePerson)
	if err != nil {
		t.Fatalf("build the agent: %v", err)
	}
	account, err := new(entities.Account).With("account-dana", "Dana Whitfield's Account", entities.AccountTypePersonal)
	if err != nil {
		t.Fatalf("build the account: %v", err)
	}
	token, err := before.IssueToken(ctx, agent, []*entities.Account{account}, account.GetID(), nil, nil)
	if err != nil {
		t.Fatalf("issue the token: %v", err)
	}
	if _, err := before.ValidateToken(ctx, token); err != nil {
		t.Fatalf("the token does not validate on the service that issued it: %v", err)
	}
	after, err := ProvideJWTService(cfg)
	if err != nil {
		t.Fatalf("build the service after the restart: %v", err)
	}
	_, err = after.ValidateToken(ctx, token)
	return err == nil
}

var testSignIns = map[string]func(*config.Config){
	"an OAuth provider": func(c *config.Config) {
		c.OAuth.GoogleClientID = "google-client-id"
		c.OAuth.GoogleClientSecret = "google-client-secret"
	},
	"password sign-in": func(c *config.Config) { c.PasswordAuthEnabled = true },
	"a trusted issuer": func(c *config.Config) {
		c.TrustedIssuer = config.TrustedIssuerConfig{
			Issuer:   "https://money.weos.cloud",
			JWKSURL:  "https://money.weos.cloud/door/jwks.json",
			Audience: "a1b2c3d4",
		}
	},
}

// Every instance with a sign-in mounts the authorization server and hands MCP
// connectors bearer tokens, so every one of them honors JWT_SIGNING_KEY: a
// token issued before a restart is still good after it.
func TestProvideJWTServiceHonorsTheSigningKeyOnEveryInstanceWithASignIn(t *testing.T) {
	key := testSigningKeyPEM(t)
	for name, signIn := range testSignIns {
		configure := func(signingKey string) config.Config {
			cfg := config.Default()
			signIn(&cfg)
			cfg.OAuth.JWTSigningKey = signingKey
			if !cfg.AuthEnabled() {
				t.Fatalf("%s: the instance has no sign-in", name)
			}
			return cfg
		}
		t.Run(name+", key configured", func(t *testing.T) {
			if !tokenSurvivesRestart(t, configure(key)) {
				t.Fatalf("a token issued before the restart is refused after it, with the same JWT_SIGNING_KEY")
			}
		})
		for _, ephemeral := range []string{"", "auto"} {
			t.Run(name+", key "+`"`+ephemeral+`"`, func(t *testing.T) {
				if tokenSurvivesRestart(t, configure(ephemeral)) {
					t.Fatalf("an ephemeral key survived the restart")
				}
			})
		}
		t.Run(name+", malformed key", func(t *testing.T) {
			if _, err := ProvideJWTService(configure("not a PEM key")); err == nil {
				t.Fatalf("a malformed JWT_SIGNING_KEY on an instance that issues tokens was accepted")
			}
		})
	}
}

// An instance with no sign-in issues no tokens. There the key is ignored, as
// before, so a malformed one left in a development environment does not stop
// boot.
func TestProvideJWTServiceIgnoresTheSigningKeyWithNoSignIn(t *testing.T) {
	cfg := config.Default()
	if cfg.AuthEnabled() {
		t.Fatalf("the default configuration must have no sign-in")
	}
	cfg.OAuth.JWTSigningKey = testSigningKeyPEM(t)
	if tokenSurvivesRestart(t, cfg) {
		t.Fatalf("an instance with no sign-in used JWT_SIGNING_KEY")
	}
	cfg.OAuth.JWTSigningKey = "-----BEGIN RSA PRIVATE KEY-----\nbm90IGEga2V5\n-----END RSA PRIVATE KEY-----"
	if _, err := ProvideJWTService(cfg); err != nil {
		t.Fatalf("a malformed key on an instance with no sign-in stopped boot: %v", err)
	}
}
