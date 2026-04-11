package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"weos/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/auth/application"
	gojwt "github.com/golang-jwt/jwt/v5"
)

func TestNewHMACInviteTokenService_EmptySecret(t *testing.T) {
	svc, err := NewHMACInviteTokenService("")
	if svc != nil {
		t.Fatal("expected nil service for empty secret")
	}
	if !errors.Is(err, application.ErrNoSigningKey) {
		t.Fatalf("expected ErrNoSigningKey, got %v", err)
	}
}

func TestNewHMACInviteTokenService_WithSecret(t *testing.T) {
	svc, err := NewHMACInviteTokenService("shhh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestIssueAndValidate_RoundTrip(t *testing.T) {
	svc, err := NewHMACInviteTokenService("shhh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	token, err := svc.IssueInviteToken(context.Background(), "invite-123", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error issuing token: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := svc.ValidateInviteToken(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error validating token: %v", err)
	}
	if claims.InviteID != "invite-123" {
		t.Fatalf("expected invite_id=invite-123, got %s", claims.InviteID)
	}
	if claims.Subject != "invite-123" {
		t.Fatalf("expected sub=invite-123, got %s", claims.Subject)
	}
	if claims.Issuer != "weos" {
		t.Fatalf("expected iss=weos, got %s", claims.Issuer)
	}
}

func TestIssueInviteToken_EmptyInviteID(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	_, err := svc.IssueInviteToken(context.Background(), "", time.Hour)
	if err == nil {
		t.Fatal("expected error for empty invite ID")
	}
}

func TestIssueInviteToken_NonPositiveExpiry(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	_, err := svc.IssueInviteToken(context.Background(), "invite-1", 0)
	if err == nil {
		t.Fatal("expected error for zero expiry")
	}
	_, err = svc.IssueInviteToken(context.Background(), "invite-1", -time.Second)
	if err == nil {
		t.Fatal("expected error for negative expiry")
	}
}

func TestIssueInviteToken_ContextCancelled(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.IssueInviteToken(ctx, "invite-1", time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestValidateInviteToken_Expired(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")

	// Build a token that expired 1 second ago by signing claims manually.
	now := time.Now()
	claims := application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    "weos",
			Subject:   "invite-1",
			IssuedAt:  gojwt.NewNumericDate(now.Add(-time.Hour)),
			ExpiresAt: gojwt.NewNumericDate(now.Add(-time.Second)),
		},
		InviteID: "invite-1",
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(svc.secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = svc.ValidateInviteToken(context.Background(), signed)
	if !errors.Is(err, application.ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestValidateInviteToken_WrongSecret(t *testing.T) {
	issuer, _ := NewHMACInviteTokenService("secret-A")
	validator, _ := NewHMACInviteTokenService("secret-B")

	token, err := issuer.IssueInviteToken(context.Background(), "invite-1", time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, err = validator.ValidateInviteToken(context.Background(), token)
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateInviteToken_WrongAlgorithm(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")

	// A token with "alg: none" must be rejected — the service only
	// accepts HMAC signatures.
	claims := application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   "invite-1",
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		InviteID: "invite-1",
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims)
	signed, err := token.SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = svc.ValidateInviteToken(context.Background(), signed)
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

// signedClaims is a test helper that HS256-signs the given claims with the
// given secret, bypassing the service's own IssueInviteToken to produce
// intentionally malformed tokens for validation tests.
func signedClaims(t *testing.T, secret string, claims application.InviteClaims) string {
	t.Helper()
	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func TestValidateInviteToken_HS384_Rejected(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")

	// HS384 is a valid HMAC variant but should be rejected — we only
	// accept HS256 to match the documented algorithm.
	claims := application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    "weos",
			Subject:   "invite-1",
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		InviteID: "invite-1",
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodHS384, claims)
	signed, err := token.SignedString(svc.secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = svc.ValidateInviteToken(context.Background(), signed)
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for HS384, got %v", err)
	}
}

func TestValidateInviteToken_WrongIssuer(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	signed := signedClaims(t, "shhh", application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    "not-weos",
			Subject:   "invite-1",
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		InviteID: "invite-1",
	})
	_, err := svc.ValidateInviteToken(context.Background(), signed)
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateInviteToken_MissingExpiry(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	signed := signedClaims(t, "shhh", application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:  "weos",
			Subject: "invite-1",
		},
		InviteID: "invite-1",
	})
	_, err := svc.ValidateInviteToken(context.Background(), signed)
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateInviteToken_MissingInviteID(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	signed := signedClaims(t, "shhh", application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    "weos",
			Subject:   "invite-1",
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		// InviteID intentionally empty
	})
	_, err := svc.ValidateInviteToken(context.Background(), signed)
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateInviteToken_SubjectMismatch(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	signed := signedClaims(t, "shhh", application.InviteClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    "weos",
			Subject:   "different-id",
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		InviteID: "invite-1",
	})
	_, err := svc.ValidateInviteToken(context.Background(), signed)
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateInviteToken_Malformed(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	_, err := svc.ValidateInviteToken(context.Background(), "not.a.jwt")
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateInviteToken_EmptyString(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	_, err := svc.ValidateInviteToken(context.Background(), "")
	if !errors.Is(err, application.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateInviteToken_ContextCancelled(t *testing.T) {
	svc, _ := NewHMACInviteTokenService("shhh")
	token, _ := svc.IssueInviteToken(context.Background(), "invite-1", time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.ValidateInviteToken(ctx, token)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestProvideInviteTokenService_WithSecret(t *testing.T) {
	cfg := config.Config{SessionSecret: "shhh"}
	svc, err := ProvideInviteTokenService(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestProvideInviteTokenService_NoSecret(t *testing.T) {
	cfg := config.Config{}
	_, err := ProvideInviteTokenService(cfg)
	if !errors.Is(err, application.ErrNoSigningKey) {
		t.Fatalf("expected ErrNoSigningKey, got %v", err)
	}
}
