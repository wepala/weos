package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// cookieSessionsFor hands back one SessionData for any request carrying the
// session cookie, which is all the identity read needs of the manager.
type cookieSessionsFor struct {
	session.SessionManager
	data *session.SessionData
}

func (s cookieSessionsFor) GetHTTPSession(r *http.Request) (*session.SessionData, error) {
	if _, err := r.Cookie("weos-session"); err != nil {
		return nil, errors.New("no session")
	}
	return s.data, nil
}

type validatingAuth struct {
	authapp.AuthenticationService
	info *authapp.SessionInfo
	err  error
}

func (v validatingAuth) ValidateSession(context.Context, string) (*authapp.SessionInfo, error) {
	return v.info, v.err
}

type lockSetFor map[string]bool

func (l lockSetFor) Lock(_ context.Context, id, _ string) error { l[id] = true; return nil }
func (l lockSetFor) IsLocked(_ context.Context, id string) (bool, error) {
	return l[id], nil
}

type stubAgents struct {
	authrepos.AgentRepository
}

func (stubAgents) FindByID(context.Context, string) (*authentities.Agent, error) {
	return nil, nil
}

type stubCreds struct {
	authrepos.CredentialRepository
}

func (stubCreds) FindByAgent(context.Context, string) ([]*authentities.Credential, error) {
	return nil, nil
}

// readMe sends GET /api/auth/me with the session cookie, through a handler
// wired to validate sessions the given way.
func readMe(t *testing.T, auth validatingAuth, locks lockSetFor, data *session.SessionData) *httptest.ResponseRecorder {
	t.Helper()
	h := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store:          sessions.NewCookieStore([]byte("test-secret")),
		AccountRepo:    stubAccounts{roles: map[string]string{"ops|acct-harbor": authentities.RoleOwner}},
		AgentRepo:      stubAgents{},
		CredRepo:       stubCreds{},
		SessionManager: cookieSessionsFor{data: data},
		AuthService:    auth,
		ErasureLocks:   locks,
		Logger:         nopLogger{},
	})
	e := echo.New()
	e.GET("/api/auth/me", h.Me(&authhttp.AuthHandlers{}))
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "weos-session", Value: "x"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func meCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return body.Code
}

// wm-ccg4f: the identity read answered 200 from the cookie for an account
// locked for deletion, and for a deleted account's second-device cookie.
func TestMe_RefusesALockedAccountsSessionWithTheCode(t *testing.T) {
	data := &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}
	deactivated := validatingAuth{err: authapp.ErrSessionAccountDeactivated}

	rec := readMe(t, deactivated, lockSetFor{"acct-harbor": true}, data)
	if rec.Code != http.StatusUnauthorized || meCode(t, rec) != apimw.CodeAccountErasurePending {
		t.Fatalf("a locked account's identity read got %d %s, want 401 %s", rec.Code, rec.Body.String(), apimw.CodeAccountErasurePending)
	}
	rec = readMe(t, deactivated, lockSetFor{}, data)
	if rec.Code != http.StatusUnauthorized || meCode(t, rec) != apimw.CodeAccountDeactivated {
		t.Fatalf("a suspended account's identity read got %d %s, want 401 %s", rec.Code, rec.Body.String(), apimw.CodeAccountDeactivated)
	}
}

func TestMe_RefusesACookieWhoseSessionIsGone(t *testing.T) {
	data := &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}
	rec := readMe(t, validatingAuth{err: authapp.ErrSessionNotFound}, lockSetFor{}, data)
	if rec.Code != http.StatusUnauthorized || meCode(t, rec) != "" {
		t.Fatalf("a gone session's identity read got %d %s, want a bare 401", rec.Code, rec.Body.String())
	}
	rec = readMe(t, validatingAuth{err: authapp.ErrSessionAccountRevoked}, lockSetFor{}, data)
	if rec.Code != http.StatusUnauthorized || meCode(t, rec) != apimw.CodeAccountAccessRevoked {
		t.Fatalf("a revoked membership's identity read got %d %s, want 401 %s", rec.Code, rec.Body.String(), apimw.CodeAccountAccessRevoked)
	}
}

// tokenFor vouches for any bearer token with fixed claims, or refuses every
// token when claims is nil, which stands for a token that is invalid or has
// expired.
type tokenFor struct {
	authapp.JWTService
	claims *authapp.PericarpClaims
}

func (t tokenFor) ValidateToken(context.Context, string) (*authapp.PericarpClaims, error) {
	if t.claims == nil {
		return nil, errors.New("token refused")
	}
	return t.claims, nil
}

// meRequest is one GET /api/auth/me: the claims the JWT service vouches for,
// the bearer token the request carries (none when empty), and whether the
// session cookie for ops in Harbor Legal rides along.
type meRequest struct {
	claims *authapp.PericarpClaims
	token  string
	cookie bool
}

// readMeThrough sends the request through the identity read with the bearer
// middleware in front of it, as serve.go mounts the route (wm-hg3xf).
func readMeThrough(t *testing.T, r meRequest) *httptest.ResponseRecorder {
	t.Helper()
	cedar, err := (&authentities.Account{}).With("acct-cedar", "Cedar Realty", authentities.AccountTypePersonal)
	if err != nil {
		t.Fatal(err)
	}
	accounts := stubAccounts{
		accounts: map[string]*authentities.Account{"acct-harbor": harborAccount(t), "acct-cedar": cedar},
		roles: map[string]string{
			"ops|acct-harbor":   authentities.RoleOwner,
			"broker|acct-cedar": authentities.RoleMember,
		},
	}
	sessionsForOps := cookieSessionsFor{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	opsSession := validatingAuth{info: &authapp.SessionInfo{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	h := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store:          sessions.NewCookieStore([]byte("test-secret")),
		AccountRepo:    accounts,
		AgentRepo:      stubAgents{},
		CredRepo:       stubCreds{},
		SessionManager: sessionsForOps,
		AuthService:    opsSession,
		ErasureLocks:   lockSetFor{},
		Logger:         nopLogger{},
	})
	pericarpMe := authhttp.NewAuthHandlers(authhttp.HandlerConfig{
		AuthService: opsSession, SessionManager: sessionsForOps, Credentials: stubCreds{}, Logger: nopLogger{},
	})
	noSession := func(next http.Handler) http.Handler { return next }
	e := echo.New()
	e.GET("/api/auth/me", h.Me(pericarpMe),
		apimw.BearerOrSession(tokenFor{claims: r.claims}, noSession, "http://acceptance.invalid", accounts, lockSetFor{}))
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.cookie {
		req.AddCookie(&http.Cookie{Name: "weos-session", Value: "x"})
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

type meAnswer struct {
	ID        string `json:"id"`
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
}

func meBody(t *testing.T, rec *httptest.ResponseRecorder) meAnswer {
	t.Helper()
	var body struct {
		Data meAnswer `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return body.Data
}

// wm-hg3xf: an app in a native shell holds no cookie for the instance, only
// the token its sign-in handed back, and the identity read refused it.
func TestMe_AnswersABearerTokenWithTheBodyASessionGets(t *testing.T) {
	ops := &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor"}
	byToken := readMeThrough(t, meRequest{claims: ops, token: "token-for-ops"})
	if byToken.Code != http.StatusOK {
		t.Fatalf("a bearer token with no cookie got %d %s, want 200", byToken.Code, byToken.Body.String())
	}
	byCookie := readMeThrough(t, meRequest{cookie: true})
	if byCookie.Code != http.StatusOK {
		t.Fatalf("the session cookie with the bearer middleware in front got %d %s, want 200", byCookie.Code, byCookie.Body.String())
	}
	if got := meBody(t, byCookie); got != (meAnswer{ID: "ops", AccountID: "acct-harbor", Role: authentities.RoleOwner}) {
		t.Fatalf("the session's answer = %+v, want ops in acct-harbor as owner", got)
	}
	if byToken.Body.String() != byCookie.Body.String() {
		t.Fatalf("the token's answer %s differs from the session's %s", byToken.Body.String(), byCookie.Body.String())
	}
}

func TestMe_RefusesABearerTokenItCannotTrust(t *testing.T) {
	cases := map[string]*authapp.PericarpClaims{
		"invalid or expired":          nil,
		"for an account that is gone": {AgentID: "ops", AccountIDs: []string{"acct-gone"}, ActiveAccountID: "acct-gone"},
	}
	for name, claims := range cases {
		t.Run(name, func(t *testing.T) {
			rec := readMeThrough(t, meRequest{claims: claims, token: "token-for-ops"})
			if rec.Code != http.StatusUnauthorized || meCode(t, rec) != "" {
				t.Fatalf("got %d %s, want a 401 with no code", rec.Code, rec.Body.String())
			}
		})
	}
}

// The token wins over a cookie beside it, the precedence BearerOrSession
// already applies on the routes that take both: the answer is for the token's
// person, and a token the instance cannot trust is refused even when a good
// session cookie rides along.
func TestMe_ABearerTokenWinsOverASessionCookie(t *testing.T) {
	broker := &authapp.PericarpClaims{AgentID: "broker", AccountIDs: []string{"acct-cedar"}, ActiveAccountID: "acct-cedar"}
	rec := readMeThrough(t, meRequest{claims: broker, token: "token-for-broker", cookie: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := meBody(t, rec); got != (meAnswer{ID: "broker", AccountID: "acct-cedar", Role: authentities.RoleMember}) {
		t.Fatalf("answer = %+v, want broker in acct-cedar as member: the token's person, not the cookie's", got)
	}
	rec = readMeThrough(t, meRequest{token: "token-for-broker", cookie: true})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an untrusted token beside a good cookie got %d %s, want 401", rec.Code, rec.Body.String())
	}
}

func TestMe_AnswersFromTheValidatedSession(t *testing.T) {
	data := &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}
	valid := validatingAuth{info: &authapp.SessionInfo{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	rec := readMe(t, valid, lockSetFor{}, data)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			ID        string `json:"id"`
			AccountID string `json:"account_id"`
			Role      string `json:"role"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Data.ID != "ops" || body.Data.AccountID != "acct-harbor" || body.Data.Role != authentities.RoleOwner {
		t.Errorf("answer = %+v, want ops in acct-harbor as owner", body.Data)
	}
}
