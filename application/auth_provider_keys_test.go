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

// TestOAuthProviderKeysCoverTheRegistry pins OAuthProviderKeys to the keys the
// registry actually registers providers under. A trusted issuer's provider
// claim is checked against that list, so a provider added to the registry and
// not to the list would be refused at the door, and one renamed in only one
// place would sign its people in as someone new.
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

	got := make([]string, 0, len(registry))
	for name := range registry {
		got = append(got, name)
	}
	want := append([]string(nil), application.OAuthProviderKeys()...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry keys %v, OAuthProviderKeys %v", got, want)
	}
}
