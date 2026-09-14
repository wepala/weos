package application_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"reflect"
	"sort"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"

	"go.uber.org/fx"
)

// doorProviderKey is the provider a trusted issuer names for an identity it
// owns itself, an email and a password it verified. Written out rather than
// read from the application package, so a rename there fails this test.
const doorProviderKey = "door"

// TestOAuthProviderKeysCoverTheRegistry pins OAuthProviderKeys to the keys the
// registry actually registers providers under, plus the door's own key, which
// no registry entry holds. A trusted issuer's provider claim is checked against
// that list, so a provider added to the registry and not to the list would be
// refused at the door, and one renamed in only one place would sign its people
// in as someone new.
func TestOAuthProviderKeysCoverTheRegistry(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.OAuth.GoogleClientID, cfg.OAuth.GoogleClientSecret = "id.apps.googleusercontent.com", "secret"
	cfg.OAuth.NetSuiteClientID, cfg.OAuth.NetSuiteClientSecret, cfg.OAuth.NetSuiteAccountID = "id", "secret", "1234567"
	cfg.OAuth.AppleClientID, cfg.OAuth.AppleTeamID, cfg.OAuth.AppleKeyID = "app.example.web", "TEAM123456", "KEY1234567"
	cfg.OAuth.ApplePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	registry := application.ProvideOAuthProviderRegistry(struct {
		fx.In
		Config config.Config
	}{Config: cfg})

	got := make([]string, 0, len(registry)+1)
	for name := range registry {
		got = append(got, name)
	}
	// The door's key is accepted from a trusted issuer and never registered:
	// the door, not this instance, signs those identities in. A registry that
	// ever registered it would list it twice here.
	got = append(got, doorProviderKey)
	want := append([]string(nil), application.OAuthProviderKeys()...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry keys %v, OAuthProviderKeys %v", got, want)
	}
}
