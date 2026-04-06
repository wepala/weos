package oauth

import (
	"testing"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
)

func TestValidateRedirectURI_HTTPS(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https://example.com/callback"); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestValidateRedirectURI_LocalhostHTTP(t *testing.T) {
	t.Parallel()
	for _, uri := range []string{
		"http://localhost:3000/callback",
		"http://127.0.0.1:8080/cb",
		"http://[::1]:9999/cb",
	} {
		if err := validateRedirectURI(uri); err != nil {
			t.Errorf("expected valid for %q, got %v", uri, err)
		}
	}
}

func TestValidateRedirectURI_RejectsPlainHTTP(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("http://evil.com/steal"); err == nil {
		t.Error("expected error for non-localhost HTTP")
	}
}

func TestValidateRedirectURI_RejectsJavascript(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("javascript:alert(1)"); err == nil {
		t.Error("expected error for javascript: scheme")
	}
}

func TestValidateRedirectURI_RejectsFragment(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https://example.com/cb#fragment"); err == nil {
		t.Error("expected error for fragment")
	}
}

func TestValidateRedirectURI_RejectsNoHost(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https:/path"); err == nil {
		t.Error("expected error for missing host")
	}
}

func TestValidateRedirectURI_RejectsUserinfo(t *testing.T) {
	t.Parallel()
	if err := validateRedirectURI("https://user:pass@example.com/cb"); err == nil {
		t.Error("expected error for userinfo")
	}
}

func TestIsRedirectURIAllowed(t *testing.T) {
	t.Parallel()
	ok, err := isRedirectURIAllowed(`["https://a.com/cb","https://b.com/cb"]`, "https://a.com/cb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected allowed")
	}
}

func TestIsRedirectURIAllowed_NotInList(t *testing.T) {
	t.Parallel()
	ok, err := isRedirectURIAllowed(`["https://a.com/cb"]`, "https://evil.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected not allowed")
	}
}

func TestIsRedirectURIAllowed_CorruptJSON(t *testing.T) {
	t.Parallel()
	_, err := isRedirectURIAllowed(`not json`, "https://a.com")
	if err == nil {
		t.Error("expected error for corrupt JSON")
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	t.Parallel()
	h1 := HashToken("test-token")
	h2 := HashToken("test-token")
	if h1 != h2 {
		t.Errorf("hash not deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("hash should not be empty")
	}
}

func TestHashToken_DifferentInput(t *testing.T) {
	t.Parallel()
	if HashToken("a") == HashToken("b") {
		t.Error("different inputs should produce different hashes")
	}
}

func TestGenerateRefreshToken_Unique(t *testing.T) {
	t.Parallel()
	t1, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if t1 == t2 {
		t.Error("tokens should be unique")
	}
}

func TestPKCEVerification(t *testing.T) {
	t.Parallel()
	verifier, err := authapp.GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	challenge := authapp.GenerateCodeChallenge(verifier)

	// Correct verifier should match.
	if authapp.GenerateCodeChallenge(verifier) != challenge {
		t.Error("same verifier should produce same challenge")
	}

	// Wrong verifier should not match.
	if authapp.GenerateCodeChallenge("wrong-verifier") == challenge {
		t.Error("wrong verifier should not match challenge")
	}
}

func TestStatusConstants(t *testing.T) {
	t.Parallel()
	if StatusPending == StatusIssued || StatusIssued == StatusExchanged {
		t.Error("status constants must be distinct")
	}
}
