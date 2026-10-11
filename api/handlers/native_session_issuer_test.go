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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/glebarez/sqlite"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// wm-xbd5h. A downstream binary's own sign-in — crossdeck's Mastodon and
// Bluesky — must hand back a native session the way password sign-in does: an
// hour-long access token that names its session, and a refresh token that
// renews it at POST /api/auth/refresh. NativeSessionIssuer is that one code
// path, exported. These tests drive it over a real refresh token store, so the
// renewal's rotation is the store's own. No failure message prints a token.

// issuerAuthService issues an access token whose claims carry the native
// session the context names, the way core's claims enricher does, with an exp
// an hour out — or at the context's weosoauth.AccessTokenNotAfter when that
// comes first, the way core's token service does, unless ignoresNotAfter —
// or with no exp at all when omitExp. onIssue, when set, runs after the token
// is signed and before it is handed back, so a test can let time pass mid-issue.
type issuerAuthService struct {
	authapp.AuthenticationService
	tokenErr        error
	emptyToken      bool
	ignoresNotAfter bool
	omitExp         bool
	onIssue         func()
	gotVouched      bool
	gotAccount      string
	gotSessions     []string
}

func (s *issuerAuthService) IssueIdentityToken(
	ctx context.Context, agent *authentities.Agent, accountID string, opts ...authapp.AccountTrustOption,
) (string, error) {
	s.gotVouched = len(opts) > 0
	s.gotAccount = accountID
	if s.tokenErr != nil {
		return "", s.tokenErr
	}
	if s.emptyToken {
		return "", nil
	}
	extras, _ := weosoauth.NativeSessionClaims(ctx, agent, nil, accountID)
	session, _ := extras[weosoauth.NativeSessionClaim].(string)
	s.gotSessions = append(s.gotSessions, session)
	exp := time.Now().Add(time.Hour)
	if notAfter := weosoauth.AccessTokenNotAfter(ctx); !s.ignoresNotAfter && !notAfter.IsZero() && notAfter.Before(exp) {
		exp = notAfter
	}
	body := map[string]any{"sub": agent.GetID(), "exp": exp.Unix(), weosoauth.NativeSessionClaim: session}
	if s.omitExp {
		delete(body, "exp")
	}
	claims, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	if s.onIssue != nil {
		s.onIssue()
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString(claims) + ".sig", nil
}

func issuerStore(t *testing.T) (*gorm.DB, weosoauth.RefreshTokenRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&weosoauth.OAuthRefreshToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, weosoauth.NewRefreshTokenRepository(db)
}

// newTestIssuer builds an issuer over store with everything a renewal reads.
func newTestIssuer(auth authapp.AuthenticationService, store weosoauth.RefreshTokenRepository, agent *authentities.Agent,
	account *authentities.Account,
) *handlers.NativeSessionIssuer {
	return handlers.NewNativeSessionIssuer(handlers.NativeSessionIssuerConfig{
		AuthService:   auth,
		RefreshTokens: store,
		AgentRepo:     busyRenewalAgents{agent: agent},
		AccountRepo:   busyRenewalAccounts{account: account},
		ErasureLocks:  busyRenewalLocks{},
		Logger:        nopLogger{},
	})
}

func TestNativeSessionIssuer_IssuesAnAccessTokenAndARefreshTokenForOneSession(t *testing.T) {
	_, store := issuerStore(t)
	auth := &issuerAuthService{}
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	issuer := newTestIssuer(auth, store, agent, newAccount(t, "acct-harbor", "Harbor Legal"))
	if !issuer.CanRenew() {
		t.Fatal("an issuer with every store a renewal reads says it cannot renew")
	}

	start := time.Now()
	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor")
	if err != nil {
		t.Fatalf("issue a native session: %v", err)
	}
	if tokens.Token == "" || tokens.RefreshToken == "" || tokens.Token == tokens.RefreshToken {
		t.Fatalf("the session carries token %v and refresh token %v, want both and distinct",
			tokens.Token != "", tokens.RefreshToken != "")
	}
	if tokens.TokenExpiresAt.Before(start.Add(time.Hour-time.Minute)) || tokens.TokenExpiresAt.After(time.Now().Add(time.Hour+time.Minute)) {
		t.Fatalf("the access token expires at %v, want an hour from now", tokens.TokenExpiresAt)
	}
	if tokens.RefreshTokenExpiresAt.Before(start.Add(weosoauth.NativeRefreshTokenTTL - time.Minute)) {
		t.Fatalf("the refresh token expires at %v, want %v from now", tokens.RefreshTokenExpiresAt, weosoauth.NativeRefreshTokenTTL)
	}
	if auth.gotAccount != "acct-harbor" {
		t.Fatalf("the access token was issued for account %q, want acct-harbor", auth.gotAccount)
	}
	if auth.gotVouched {
		t.Fatal("the access token was issued with the account vouched for, and the caller never asked")
	}

	stored, err := store.FindByTokenHash(context.Background(), weosoauth.HashToken(tokens.RefreshToken))
	if err != nil {
		t.Fatalf("read the refresh token back: %v", err)
	}
	if !weosoauth.IsNativeRefreshToken(stored) || stored.AgentID != "agent-ops" || stored.AccountID != "acct-harbor" {
		t.Fatalf("the stored refresh token is native %v, for agent %q account %q",
			weosoauth.IsNativeRefreshToken(stored), stored.AgentID, stored.AccountID)
	}
	if len(auth.gotSessions) != 1 || auth.gotSessions[0] == "" || auth.gotSessions[0] != stored.FamilyID {
		t.Fatalf("the access token names session %q, the refresh token's family is %q; want the same one",
			auth.gotSessions, stored.FamilyID)
	}
	if stored.FamilyExpiresAt != nil {
		t.Fatalf("an uncapped session stored family cap %v, want none", *stored.FamilyExpiresAt)
	}
}

func TestNativeSessionIssuer_WithAccountTrustPassesTheTrustToTheAccessToken(t *testing.T) {
	_, store := issuerStore(t)
	auth := &issuerAuthService{}
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	issuer := newTestIssuer(auth, store, agent, newAccount(t, "acct-harbor", "Harbor Legal"))

	if _, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor",
		handlers.WithAccountTrust(authapp.AccountAlreadyVerified())); err != nil {
		t.Fatalf("issue a native session: %v", err)
	}
	if !auth.gotVouched {
		t.Fatal("WithAccountTrust did not reach the access token's issuance")
	}
}

// A session capped at 24 hours hands back a refresh token that stops renewing
// with the session, and every renewal of it at POST /auth/refresh keeps to the
// cap: the successor expires at the cap, not 30 days on.
func TestNativeSessionIssuer_ACappedSessionRenewsOnlyUntilTheCap(t *testing.T) {
	_, store := issuerStore(t)
	auth := &issuerAuthService{}
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	account := newAccount(t, "acct-harbor", "Harbor Legal")
	issuer := newTestIssuer(auth, store, agent, account)
	capAt := time.Now().Add(24 * time.Hour)

	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor", handlers.WithRefreshExpiresAt(capAt))
	if err != nil {
		t.Fatalf("issue a capped native session: %v", err)
	}
	if !closeTo(tokens.RefreshTokenExpiresAt, capAt) {
		t.Fatalf("the capped refresh token expires at %v, want the cap %v", tokens.RefreshTokenExpiresAt, capAt)
	}

	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    auth,
		Logger:         nopLogger{},
		NativeSessions: issuer,
	})
	refresh := tokens.RefreshToken
	for i := range 2 {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(newJSONRequest(http.MethodPost, "/api/auth/refresh", `{"refresh_token":"`+refresh+`"}`), rec)
		if err := h.Refresh(c); err != nil {
			t.Fatalf("renewal %d: %v", i+1, err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("renewal %d answered %d, want 200", i+1, rec.Code)
		}
		var answer struct {
			Data struct {
				RefreshToken          string    `json:"refresh_token"`
				RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatalf("decode renewal %d: %v", i+1, err)
		}
		if answer.Data.RefreshToken == "" || answer.Data.RefreshToken == refresh {
			t.Fatalf("renewal %d handed back no new refresh token", i+1)
		}
		if !closeTo(answer.Data.RefreshTokenExpiresAt, capAt) {
			t.Fatalf("renewal %d handed back a refresh token expiring at %v, want the session's cap %v",
				i+1, answer.Data.RefreshTokenExpiresAt, capAt)
		}
		refresh = answer.Data.RefreshToken
	}
}

func TestNativeSessionIssuer_ACapThatHasPassedIssuesNothing(t *testing.T) {
	db, store := issuerStore(t)
	auth := &issuerAuthService{}
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	issuer := newTestIssuer(auth, store, agent, newAccount(t, "acct-harbor", "Harbor Legal"))

	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor",
		handlers.WithRefreshExpiresAt(time.Now().Add(-time.Minute)))
	if !errors.Is(err, handlers.ErrNativeRefreshCapPassed) {
		t.Fatalf("a cap that has passed answered %v, want ErrNativeRefreshCapPassed", err)
	}
	if tokens.Token != "" || tokens.RefreshToken != "" || len(auth.gotSessions) != 0 {
		t.Fatal("a cap that has passed still issued a token")
	}
	var rows int64
	if err := db.Model(&weosoauth.OAuthRefreshToken{}).Count(&rows).Error; err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}
	if rows != 0 {
		t.Fatalf("the store holds %d refresh token(s), want 0", rows)
	}
}

// PR 576 review. The cap passes while the access token is signed: the cap was
// in the future when the sign-in began, and is not by the time the refresh
// token is written. Nothing is handed back — not the access token, which ends
// at the cap and so has already expired — and the answer says the cap passed,
// not that the refresh token failed, so no caller hands the dead token back
// alone.
func TestNativeSessionIssuer_ACapThatPassesWhileTheAccessTokenIsSignedIssuesNothing(t *testing.T) {
	db, store := issuerStore(t)
	capAt := time.Now().Add(300 * time.Millisecond)
	auth := &issuerAuthService{onIssue: func() { time.Sleep(time.Until(capAt) + 20*time.Millisecond) }}
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	issuer := newTestIssuer(auth, store, agent, newAccount(t, "acct-harbor", "Harbor Legal"))

	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor", handlers.WithRefreshExpiresAt(capAt))
	if !errors.Is(err, handlers.ErrNativeRefreshCapPassed) {
		t.Fatalf("a cap that passed mid-issue answered %v, want ErrNativeRefreshCapPassed", err)
	}
	if errors.Is(err, handlers.ErrNativeRefreshTokenNotIssued) {
		t.Fatal("a cap that passed mid-issue answered ErrNativeRefreshTokenNotIssued, which says the access token is good alone")
	}
	if tokens != (handlers.NativeSessionTokens{}) {
		t.Fatalf("a cap that passed mid-issue handed back token %v, refresh token %v; want neither",
			tokens.Token != "", tokens.RefreshToken != "")
	}
	var rows int64
	if err := db.Model(&weosoauth.OAuthRefreshToken{}).Count(&rows).Error; err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}
	if rows != 0 {
		t.Fatalf("the store holds %d refresh token(s), want 0", rows)
	}
}

func TestNativeSessionIssuer_WithoutTheStoresItCannotRenewAndIssuesNothing(t *testing.T) {
	auth := &issuerAuthService{}
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	var nilIssuer *handlers.NativeSessionIssuer
	if nilIssuer.CanRenew() {
		t.Fatal("a nil issuer says it can renew")
	}
	issuer := handlers.NewNativeSessionIssuer(handlers.NativeSessionIssuerConfig{AuthService: auth, Logger: nopLogger{}})
	if issuer.CanRenew() {
		t.Fatal("an issuer with no refresh token store says it can renew")
	}
	_, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor")
	if !errors.Is(err, handlers.ErrNativeSessionUnavailable) {
		t.Fatalf("an issuer that cannot renew answered %v, want ErrNativeSessionUnavailable", err)
	}
	if len(auth.gotSessions) != 0 {
		t.Fatal("an issuer that cannot renew still issued an access token")
	}
}

func TestNativeSessionIssuer_AnAccessTokenThatFailsIssuesNoRefreshToken(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth *issuerAuthService
	}{
		{"an error", &issuerAuthService{tokenErr: errors.New("signing key unavailable")}},
		{"no token", &issuerAuthService{emptyToken: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, store := issuerStore(t)
			agent := newAgent(t, "agent-ops", "Dana Whitfield")
			issuer := newTestIssuer(tc.auth, store, agent, newAccount(t, "acct-harbor", "Harbor Legal"))
			tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor")
			if err == nil || tokens.Token != "" || tokens.RefreshToken != "" {
				t.Fatalf("a failed access token answered err %v, token %v, refresh token %v; want an error and neither",
					err, tokens.Token != "", tokens.RefreshToken != "")
			}
			var rows int64
			if err := db.Model(&weosoauth.OAuthRefreshToken{}).Count(&rows).Error; err != nil {
				t.Fatalf("count refresh tokens: %v", err)
			}
			if rows != 0 {
				t.Fatalf("the store holds %d refresh token(s), want 0", rows)
			}
		})
	}
}

// A refresh token store that fails leaves the access token good: the caller
// may hand it back alone, as password sign-in does, and is told which part
// failed.
func TestNativeSessionIssuer_ARefreshTokenThatFailsStillAnswersTheAccessToken(t *testing.T) {
	auth := &issuerAuthService{}
	agent := newAgent(t, "agent-ops", "Dana Whitfield")
	issuer := newTestIssuer(auth, failingCreateStore{}, agent, newAccount(t, "acct-harbor", "Harbor Legal"))

	tokens, err := issuer.IssueNativeSession(context.Background(), agent, "acct-harbor")
	if !errors.Is(err, handlers.ErrNativeRefreshTokenNotIssued) {
		t.Fatalf("a failed refresh token answered %v, want ErrNativeRefreshTokenNotIssued", err)
	}
	if tokens.Token == "" || tokens.RefreshToken != "" {
		t.Fatalf("a failed refresh token answered token %v, refresh token %v; want the token alone",
			tokens.Token != "", tokens.RefreshToken != "")
	}
}

type failingCreateStore struct {
	weosoauth.RefreshTokenRepository
}

func (failingCreateStore) Create(context.Context, *weosoauth.OAuthRefreshToken, string) error {
	return errors.New("database is locked")
}

func closeTo(got, want time.Time) bool {
	d := got.Sub(want)
	return d > -2*time.Second && d < 2*time.Second
}
