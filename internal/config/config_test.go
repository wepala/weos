package config

import (
	"reflect"
	"testing"
)

func TestLoadFromEnvironment_SMTP(t *testing.T) {
	t.Setenv("SMTP_HOST", "mail.example.com")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("SMTP_USERNAME", "user")
	t.Setenv("SMTP_PASSWORD", "pass")
	t.Setenv("SMTP_FROM", "noreply@example.com")

	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.SMTP.Host != "mail.example.com" {
		t.Fatalf("expected Host mail.example.com, got %s", cfg.SMTP.Host)
	}
	if cfg.SMTP.Port != "2525" {
		t.Fatalf("expected Port 2525, got %s", cfg.SMTP.Port)
	}
	if cfg.SMTP.Username != "user" {
		t.Fatalf("expected Username user, got %s", cfg.SMTP.Username)
	}
	if cfg.SMTP.Password != "pass" {
		t.Fatalf("expected Password pass, got %s", cfg.SMTP.Password)
	}
	if cfg.SMTP.From != "noreply@example.com" {
		t.Fatalf("expected From noreply@example.com, got %s", cfg.SMTP.From)
	}
}

func TestLoadFromEnvironment_SMTP_NotSet(t *testing.T) {
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.SMTP.Host != "" {
		t.Fatalf("expected empty Host, got %s", cfg.SMTP.Host)
	}
	if cfg.SMTP.Port != "" {
		t.Fatalf("expected empty Port, got %s", cfg.SMTP.Port)
	}
}

func TestLoadFromEnvironment_NetSuiteScopes(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{"comma-separated", "rest_webservices,openid", []string{"rest_webservices", "openid"}},
		{"space-separated", "rest_webservices openid", []string{"rest_webservices", "openid"}},
		{"mixed with whitespace", " rest_webservices ,  openid\trestlets ", []string{"rest_webservices", "openid", "restlets"}},
		{"single scope", "openid", []string{"openid"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NETSUITE_SCOPES", tc.env)
			cfg := Default()
			cfg.LoadFromEnvironment()
			if !reflect.DeepEqual(cfg.OAuth.NetSuiteScopes, tc.want) {
				t.Fatalf("NetSuiteScopes = %#v, want %#v", cfg.OAuth.NetSuiteScopes, tc.want)
			}
		})
	}
}

func TestLoadFromEnvironment_NetSuiteScopes_NotSet(t *testing.T) {
	cfg := Default()
	cfg.LoadFromEnvironment()
	if cfg.OAuth.NetSuiteScopes != nil {
		t.Fatalf("expected nil NetSuiteScopes when env not set, got %#v", cfg.OAuth.NetSuiteScopes)
	}
}
