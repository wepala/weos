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

package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/labstack/echo/v4"
)

// Review findings on wm-xbd5h. A capped native session ends at its cap — the
// access token too, at sign-in and at renewal (wm-vdax2), even when a pre-cap
// binary's rotation left the cap off a row (wm-5u9q7) — and the renewal that
// lands at the cap says so in its answer (wm-2yq9p). A purge that fails is never
// silent (wm-sbevq), and neither is a configured store the issuer replaces
// (wm-9uc01).

// warnRecorder is a logger that keeps every warning, with its arguments.
type warnRecorder struct {
	nopLogger
	mu    sync.Mutex
	warns []string
}

func (l *warnRecorder) Warn(_ context.Context, msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, msg+" "+fmt.Sprint(args...))
}

func (l *warnRecorder) warnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warns...)
}

// renewalAnswer is a renewal's answer as the app reads it.
type renewalAnswer struct {
	Data struct {
		Token                 string    `json:"token"`
		TokenExpiresAt        time.Time `json:"token_expires_at"`
		RefreshToken          string    `json:"refresh_token"`
		RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
	} `json:"data"`
}

func renew(t *testing.T, h *handlers.PasswordAuthHandler, refresh string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(newJSONRequest(http.MethodPost, "/api/auth/refresh", `{"refresh_token":"`+refresh+`"}`), rec)
	if err := h.Refresh(c); err != nil {
		t.Fatalf("renew: %v", err)
	}
	return rec
}

func decodeRenewal(t *testing.T, rec *httptest.ResponseRecorder) renewalAnswer {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("the renewal answered %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var answer renewalAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("decode the renewal: %v", err)
	}
	return answer
}

// notPastCap reports whether got is at want, to the second a JWT's exp keeps,
// and never after it.
func notPastCap(got, want time.Time) bool {
	return !got.After(want) && want.Sub(got) <= time.Second
}

func cappedSessionHandler(t *testing.T, auth *issuerAuthService, logger *warnRecorder,
) (weosoauth.RefreshTokenRepository, *handlers.NativeSessionIssuer, *handlers.PasswordAuthHandler, *authentities.Agent) {
	t.Helper()
	_, store := issuerStore(t)
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	issuer := handlers.NewNativeSessionIssuer(handlers.NativeSessionIssuerConfig{
		AuthService:   auth,
		RefreshTokens: store,
		AgentRepo:     busyRenewalAgents{agent: agent},
		AccountRepo:   busyRenewalAccounts{account: newAccount(t, "acct-harbor", "Harbor Legal")},
		ErasureLocks:  busyRenewalLocks{},
		Logger:        logger,
	})
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService: auth, Logger: logger, NativeSessions: issuer,
	})
	return store, issuer, h, agent
}

// wm-vdax2. A session capped 20 minutes out hands back an access token that
// expires at the cap, not an hour on, and its answer says so.
func TestNativeSessionIssuer_ACapInsideTheHourEndsTheAccessTokenAtTheCap(t *testing.T) {
	_, issuer, _, agent := cappedSessionHandler(t, &issuerAuthService{}, &warnRecorder{})
	capAt := time.Now().Add(20 * time.Minute)

	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor", handlers.WithRefreshExpiresAt(capAt))
	if err != nil {
		t.Fatalf("issue a capped native session: %v", err)
	}
	if !notPastCap(tokens.TokenExpiresAt, capAt) {
		t.Fatalf("the access token expires at %v, want the session's cap %v", tokens.TokenExpiresAt, capAt)
	}
	if !closeTo(tokens.RefreshTokenExpiresAt, capAt) {
		t.Fatalf("the refresh token expires at %v, want the session's cap %v", tokens.RefreshTokenExpiresAt, capAt)
	}
}

// wm-vdax2. A renewal 20 minutes before the cap hands back an access token
// that expires at the cap, not an hour on.
func TestRefresh_ACapInsideTheHourEndsTheRenewedAccessTokenAtTheCap(t *testing.T) {
	_, issuer, h, agent := cappedSessionHandler(t, &issuerAuthService{}, &warnRecorder{})
	capAt := time.Now().Add(20 * time.Minute)
	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor", handlers.WithRefreshExpiresAt(capAt))
	if err != nil {
		t.Fatalf("issue a capped native session: %v", err)
	}

	answer := decodeRenewal(t, renew(t, h, tokens.RefreshToken))
	if !notPastCap(answer.Data.TokenExpiresAt, capAt) {
		t.Fatalf("the renewed access token expires at %v, want the session's cap %v", answer.Data.TokenExpiresAt, capAt)
	}
}

// wm-vdax2. A token service that cannot end the token at the cap is not hidden:
// the answer carries the token's real expiry, and a warning names the gap.
func TestNativeSessionIssuer_WarnsWhenTheTokenOutlivesTheCap(t *testing.T) {
	logger := &warnRecorder{}
	_, issuer, _, agent := cappedSessionHandler(t, &issuerAuthService{ignoresNotAfter: true}, logger)
	capAt := time.Now().Add(20 * time.Minute)

	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor", handlers.WithRefreshExpiresAt(capAt))
	if err != nil {
		t.Fatalf("issue a capped native session: %v", err)
	}
	if !tokens.TokenExpiresAt.After(capAt) {
		t.Fatalf("the access token expires at %v; the test's token service should ignore the cap %v", tokens.TokenExpiresAt, capAt)
	}
	warned := false
	for _, w := range logger.warnings() {
		if strings.Contains(w, "outlives") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("no warning said the access token outlives the session's cap; got %q", logger.warnings())
	}
}

// wm-2yq9p. The renewal that lands at the cap answers all four fields, and both
// expiries are the cap: refresh_token_expires_at within the app's renewal
// margin means sign in again, not renew again.
func TestRefresh_AtTheCapTheAnswerEndsBothTokensAtTheCap(t *testing.T) {
	_, issuer, h, agent := cappedSessionHandler(t, &issuerAuthService{}, &warnRecorder{})
	capAt := time.Now().Add(30 * time.Second)
	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor", handlers.WithRefreshExpiresAt(capAt))
	if err != nil {
		t.Fatalf("issue a capped native session: %v", err)
	}

	rec := renew(t, h, tokens.RefreshToken)
	var raw struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode the renewal: %v", err)
	}
	for _, field := range []string{"token", "token_expires_at", "refresh_token", "refresh_token_expires_at"} {
		if _, ok := raw.Data[field]; !ok {
			t.Fatalf("the renewal at the cap has no %q field", field)
		}
	}
	answer := decodeRenewal(t, rec)
	if answer.Data.Token == "" || answer.Data.RefreshToken == "" || answer.Data.RefreshToken == tokens.RefreshToken {
		t.Fatal("the renewal at the cap handed back no new token and refresh token")
	}
	if !closeTo(answer.Data.RefreshTokenExpiresAt, capAt) || answer.Data.RefreshTokenExpiresAt.After(capAt) {
		t.Fatalf("refresh_token_expires_at is %v, want the cap %v", answer.Data.RefreshTokenExpiresAt, capAt)
	}
	if !notPastCap(answer.Data.TokenExpiresAt, capAt) {
		t.Fatalf("token_expires_at is %v, want the cap %v", answer.Data.TokenExpiresAt, capAt)
	}
}

// wm-5u9q7. A renewal served by a binary from before the cap wrote a successor
// with no cap and a 30-day life. Renewing that successor here still keeps to the
// family's cap, and writes it back on.
func TestRefresh_AFamilyCapAPreCapRotationLeftOffStillHolds(t *testing.T) {
	store, issuer, h, agent := cappedSessionHandler(t, &issuerAuthService{}, &warnRecorder{})
	ctx := context.Background()
	capAt := time.Now().Add(90 * time.Minute)
	tokens, err := issuer.IssueNativeSession(ctx, agent, "acct-harbor", handlers.WithRefreshExpiresAt(capAt))
	if err != nil {
		t.Fatalf("issue a capped native session: %v", err)
	}
	root, err := store.FindByTokenHash(ctx, weosoauth.HashToken(tokens.RefreshToken))
	if err != nil {
		t.Fatalf("read the capped refresh token back: %v", err)
	}
	stripped := *root
	stripped.FamilyExpiresAt = nil
	oldBinary, err := weosoauth.RotateNativeRefreshToken(ctx, store, &stripped, tokens.RefreshToken, nil)
	if err != nil {
		t.Fatalf("rotate as a pre-cap binary: %v", err)
	}

	answer := decodeRenewal(t, renew(t, h, oldBinary.Raw))
	if !closeTo(answer.Data.RefreshTokenExpiresAt, capAt) {
		t.Fatalf("renewing the pre-cap successor handed back a refresh token expiring at %v, want the family cap %v",
			answer.Data.RefreshTokenExpiresAt, capAt)
	}
	successor, err := store.FindByTokenHash(ctx, weosoauth.HashToken(answer.Data.RefreshToken))
	if err != nil {
		t.Fatalf("read the successor back: %v", err)
	}
	if successor.FamilyExpiresAt == nil || !closeTo(*successor.FamilyExpiresAt, capAt) {
		t.Fatalf("the successor carries cap %v, want %v written back on", successor.FamilyExpiresAt, capAt)
	}
}

// wm-5u9q7. A pre-cap successor whose family's cap has passed does not renew,
// though its own row says it has 30 days left.
func TestRefresh_APreCapSuccessorOfAFamilyWhoseCapHasPassedDoesNotRenew(t *testing.T) {
	store, _, h, _ := cappedSessionHandler(t, &issuerAuthService{}, &warnRecorder{})
	ctx := context.Background()
	passed := time.Now().Add(-time.Minute)
	if err := store.Create(ctx, &weosoauth.OAuthRefreshToken{
		ID: "family-ended", FamilyID: "family-ended", AgentID: "agent-ops", AccountID: "acct-harbor",
		ClientID: weosoauth.NativeClientID, ExpiresAt: passed, FamilyExpiresAt: &passed, Revoked: true,
	}, "raw-root"); err != nil {
		t.Fatalf("store the capped root: %v", err)
	}
	if err := store.Create(ctx, &weosoauth.OAuthRefreshToken{
		FamilyID: "family-ended", AgentID: "agent-ops", AccountID: "acct-harbor",
		ClientID: weosoauth.NativeClientID, ExpiresAt: time.Now().Add(weosoauth.NativeRefreshTokenTTL),
	}, "raw-pre-cap-successor"); err != nil {
		t.Fatalf("store the pre-cap successor: %v", err)
	}

	rec := renew(t, h, "raw-pre-cap-successor")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), handlers.CodeInvalidRefreshToken) {
		t.Fatalf("a successor past its family's cap answered %d %s, want 401 %s",
			rec.Code, rec.Body.String(), handlers.CodeInvalidRefreshToken)
	}
}

// failingPurgeStore is a working refresh token store whose purge fails.
type failingPurgeStore struct {
	weosoauth.RefreshTokenRepository
}

func (failingPurgeStore) PurgeExpired(context.Context, string, time.Time) (int64, error) {
	return 0, errors.New("database is locked")
}

// wm-sbevq. An issuer built with no logger still reports a purge that failed,
// through the process's default logger.
func TestNativeSessionIssuer_APurgeThatFailsIsLoggedWithoutALogger(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, store := issuerStore(t)
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	issuer := handlers.NewNativeSessionIssuer(handlers.NativeSessionIssuerConfig{
		AuthService:   &issuerAuthService{},
		RefreshTokens: failingPurgeStore{store},
		AgentRepo:     busyRenewalAgents{agent: agent},
		AccountRepo:   busyRenewalAccounts{account: newAccount(t, "acct-harbor", "Harbor Legal")},
		ErasureLocks:  busyRenewalLocks{},
	})
	if _, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor"); err != nil {
		t.Fatalf("issue a native session: %v", err)
	}
	if !strings.Contains(out.String(), "purge failed") || !strings.Contains(out.String(), "database is locked") {
		t.Fatalf("a failed purge by an issuer with no logger left %q in the default log, want the failure", out.String())
	}
}

// wm-9uc01. A handler given both an issuer and its own stores takes the
// issuer's, as before, and warns for each configured store it replaced with a
// different one.
func TestNewPasswordAuthHandler_WarnsWhenTheIssuerReplacesAConfiguredStore(t *testing.T) {
	_, issuerStoreRepo := issuerStore(t)
	_, otherStore := issuerStore(t)
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	agents := busyRenewalAgents{agent: agent}
	issuer := handlers.NewNativeSessionIssuer(handlers.NativeSessionIssuerConfig{
		AuthService:   &issuerAuthService{},
		RefreshTokens: issuerStoreRepo,
		AgentRepo:     agents,
		AccountRepo:   busyRenewalAccounts{account: newAccount(t, "acct-harbor", "Harbor Legal")},
		ErasureLocks:  busyRenewalLocks{},
		Logger:        nopLogger{},
	})

	for _, tc := range []struct {
		name     string
		cfg      handlers.PasswordAuthHandlerConfig
		wantWarn string
	}{
		{"a different refresh token store", handlers.PasswordAuthHandlerConfig{RefreshTokens: otherStore}, "RefreshTokens"},
		{"the issuer's own stores", handlers.PasswordAuthHandlerConfig{RefreshTokens: issuerStoreRepo, AgentRepo: agents}, ""},
		{"no stores", handlers.PasswordAuthHandlerConfig{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := &warnRecorder{}
			cfg := tc.cfg
			cfg.AuthService, cfg.Logger, cfg.NativeSessions = &issuerAuthService{}, logger, issuer
			handlers.NewPasswordAuthHandler(cfg)
			warns := logger.warnings()
			if tc.wantWarn == "" {
				if len(warns) != 0 {
					t.Fatalf("the handler warned %q, want no warning", warns)
				}
				return
			}
			if len(warns) != 1 || !strings.Contains(warns[0], tc.wantWarn) {
				t.Fatalf("the handler warned %q, want one warning naming %s", warns, tc.wantWarn)
			}
		})
	}
}
