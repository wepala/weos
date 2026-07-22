package config

import (
	"reflect"
	"testing"
)

func TestParseAllowedEmails(t *testing.T) {
	got := parseAllowedEmails(" Akeem@Example.com , second@EXAMPLE.com ,, ")
	want := []string{"akeem@example.com", "second@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseAllowedEmails = %v, want %v", got, want)
	}
}

func TestLoadFromEnvironment_OAuthAllowedEmails(t *testing.T) {
	t.Setenv("OAUTH_ALLOWED_EMAILS", "akeem@example.com, Second@Example.com")

	cfg := Default()
	cfg.LoadFromEnvironment()

	want := []string{"akeem@example.com", "second@example.com"}
	if !reflect.DeepEqual(cfg.OAuth.AllowedEmails, want) {
		t.Fatalf("AllowedEmails = %v, want %v", cfg.OAuth.AllowedEmails, want)
	}
}

func TestLoadFromEnvironment_OAuthAllowedEmails_NotSet(t *testing.T) {
	cfg := Default()
	cfg.LoadFromEnvironment()

	if len(cfg.OAuth.AllowedEmails) != 0 {
		t.Fatalf("expected no AllowedEmails by default, got %v", cfg.OAuth.AllowedEmails)
	}
}
