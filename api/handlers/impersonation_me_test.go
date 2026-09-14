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

// memberBook is stubAccounts that can also list the accounts a person belongs
// to, which the impersonation branch reads.
type memberBook struct {
	stubAccounts
	members map[string][]*authentities.Account
}

func (m memberBook) FindByMember(_ context.Context, agentID string) ([]*authentities.Account, error) {
	return m.members[agentID], nil
}

// meRequest is one GET /api/auth/me: the claims the JWT service vouches for,
// the bearer token the request carries (none when empty), whether the
// session cookie for ops in Harbor Legal rides along, and the person an
// impersonation cookie started by ops names (none when empty).
type meRequest struct {
	claims        *authapp.PericarpClaims
	token         string
	cookie        bool
	impersonating string
}

// readMeThrough sends the request through the identity read with the bearer
// middleware in front of it, as serve.go mounts the route (wm-hg3xf).
func readMeThrough(t *testing.T, r meRequest) *httptest.ResponseRecorder {
	t.Helper()
	cedar, err := (&authentities.Account{}).With("acct-cedar", "Cedar Realty", authentities.AccountTypePersonal)
	if err != nil {
		t.Fatal(err)
	}
	harbor := harborAccount(t)
	accounts := memberBook{
		stubAccounts: stubAccounts{
			accounts: map[string]*authentities.Account{"acct-harbor": harbor, "acct-cedar": cedar},
			roles: map[string]string{
				"ops|acct-harbor":   authentities.RoleOwner,
				"broker|acct-cedar": authentities.RoleMember,
			},
		},
		members: map[string][]*authentities.Account{"ops": {harbor}, "broker": {cedar}},
	}
	store := sessions.NewCookieStore([]byte("test-secret"))
	sessionsForOps := cookieSessionsFor{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	opsSession := validatingAuth{info: &authapp.SessionInfo{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	h := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store:          store,
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
	e := echo.New()
	e.GET("/api/auth/me", h.Me(pericarpMe),
		apimw.BearerWhenPresent(tokenFor{claims: r.claims}, "http://acceptance.invalid", accounts, lockSetFor{}))
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if r.cookie {
		req.AddCookie(&http.Cookie{Name: "weos-session", Value: "x"})
	}
	if r.impersonating != "" {
		// Encoded by the store the handler reads, the way Start saves it.
		saved := httptest.NewRecorder()
		imp, err := store.New(req, apimw.ImpersonationSessionName)
		if err != nil {
			t.Fatalf("start the impersonation session: %v", err)
		}
		imp.Values[apimw.KeyImpersonatedAgentID] = r.impersonating
		imp.Values[apimw.KeyRealAgentID] = "ops"
		imp.Values[apimw.KeyRealAccountID] = "acct-harbor"
		if err := imp.Save(req, saved); err != nil {
			t.Fatalf("save the impersonation session: %v", err)
		}
		for _, ck := range saved.Result().Cookies() {
			req.AddCookie(ck)
		}
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

// A token is refused where the session it stands beside would be. A person
// removed from the account the token names gets the code the cookie path
// answers a revoked membership with, and a token that names no account gets
// the code the cookie path answers an unscoped session with (wm-qqoq2).
func TestMe_RefusesABearerTokenItCannotTrust(t *testing.T) {
	cases := map[string]struct {
		claims *authapp.PericarpClaims
		code   string
	}{
		"invalid or expired":          {claims: nil},
		"for an account that is gone": {claims: &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-gone"}, ActiveAccountID: "acct-gone"}},
		"for an account the person was removed from": {
			claims: &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-cedar"}, ActiveAccountID: "acct-cedar"},
			code:   apimw.CodeAccountAccessRevoked,
		},
		"naming no account": {
			claims: &authapp.PericarpClaims{AgentID: "ops"},
			code:   apimw.CodeUnscopedSession,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := readMeThrough(t, meRequest{claims: c.claims, token: "token-for-ops"})
			if rec.Code != http.StatusUnauthorized || meCode(t, rec) != c.code {
				t.Fatalf("got %d %s, want a 401 with code %q", rec.Code, rec.Body.String(), c.code)
			}
			if c.code == "" {
				return
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
			}
			if body["error"] != "not authenticated" || len(body) != 2 {
				t.Fatalf("body = %s, want the cookie path's shape {\"error\":\"not authenticated\",\"code\":%q}", rec.Body.String(), c.code)
			}
		})
	}
}

// The token wins over a session cookie beside it, the precedence
// BearerOrSession already applies on the routes that take both: the answer is for the token's
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

// wm-fqjc2: on the bearer path the identity read ignores the impersonation
// cookie on purpose. The MCP group does not — there Impersonation runs after
// BearerOrSession — so this pins the difference: a token for the admin who
// started an impersonation is answered for the admin, never for the person
// the cookie names.
func TestMe_ABearerTokenIsAnsweredForItsOwnPersonBesideAnImpersonationCookie(t *testing.T) {
	// The cookie is good: beside the admin's session cookie, the read answers
	// for the person it names.
	bySession := readMeThrough(t, meRequest{cookie: true, impersonating: "broker"})
	var impersonated struct {
		Data struct {
			ID            string `json:"id"`
			Impersonating bool   `json:"impersonating"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bySession.Body.Bytes(), &impersonated); err != nil {
		t.Fatalf("body %q is not JSON: %v", bySession.Body.String(), err)
	}
	if bySession.Code != http.StatusOK || impersonated.Data.ID != "broker" || !impersonated.Data.Impersonating {
		t.Fatalf("the admin's session beside the impersonation cookie got %d %s, want 200 for broker, impersonating", bySession.Code, bySession.Body.String())
	}

	ops := &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor"}
	byToken := readMeThrough(t, meRequest{claims: ops, token: "token-for-ops", impersonating: "broker"})
	if byToken.Code != http.StatusOK {
		t.Fatalf("the admin's token beside the impersonation cookie got %d %s, want 200", byToken.Code, byToken.Body.String())
	}
	if got := meBody(t, byToken); got != (meAnswer{ID: "ops", AccountID: "acct-harbor", Role: authentities.RoleOwner}) {
		t.Fatalf("answer = %+v, want ops in acct-harbor as owner: the token's own person, not the one the cookie names", got)
	}
	tokenAlone := readMeThrough(t, meRequest{claims: ops, token: "token-for-ops"})
	if byToken.Body.String() != tokenAlone.Body.String() {
		t.Fatalf("the impersonation cookie changed the token's answer: %s, alone %s", byToken.Body.String(), tokenAlone.Body.String())
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
