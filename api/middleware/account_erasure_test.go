package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/labstack/echo/v4"
)

type nopLogger struct{}

func (nopLogger) Debug(context.Context, string, ...any) {}
func (nopLogger) Info(context.Context, string, ...any)  {}
func (nopLogger) Warn(context.Context, string, ...any)  {}
func (nopLogger) Error(context.Context, string, ...any) {}

// cookieSessions hands back one SessionData for any request carrying a
// cookie at all, which is all the middleware under test reads.
type cookieSessions struct {
	session.SessionManager
	data *session.SessionData
}

func (s cookieSessions) GetHTTPSession(r *http.Request) (*session.SessionData, error) {
	if _, err := r.Cookie("weos-session"); err != nil || s.data == nil {
		return nil, errors.New("no session")
	}
	return s.data, nil
}

type validating struct {
	authapp.AuthenticationService
	info *authapp.SessionInfo
	err  error
}

func (v validating) ValidateSession(context.Context, string) (*authapp.SessionInfo, error) {
	return v.info, v.err
}

type lockSet map[string]bool

func (l lockSet) Lock(_ context.Context, id, _ string) error { l[id] = true; return nil }
func (l lockSet) IsLocked(_ context.Context, id string) (bool, error) {
	return l[id], nil
}

type accountBook struct {
	authrepos.AccountRepository
	accounts map[string]*authentities.Account
	roles    map[string]string // agent|account -> role
	members  map[string][]*authentities.Account
}

func (b accountBook) FindByID(_ context.Context, id string) (*authentities.Account, error) {
	return b.accounts[id], nil
}

func (b accountBook) FindMemberRole(_ context.Context, accountID, agentID string) (string, error) {
	return b.roles[agentID+"|"+accountID], nil
}

func (b accountBook) FindByMember(_ context.Context, agentID string) ([]*authentities.Account, error) {
	return b.members[agentID], nil
}

func account(t *testing.T, id string, active bool) *authentities.Account {
	t.Helper()
	a, err := (&authentities.Account{}).With(id, id, authentities.AccountTypePersonal)
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		if err := a.Deactivate(); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

// serve runs one request with a cookie through mw and a handler that reports
// the identity it was given.
func serve(mw echo.MiddlewareFunc, withCookie bool, headers map[string]string) (*httptest.ResponseRecorder, *auth.Identity, bool) {
	e := echo.New()
	var seen *auth.Identity
	locked := false
	e.GET("/api/thing", func(c echo.Context) error {
		seen = auth.AgentFromCtx(c.Request().Context())
		locked = ErasureLocked(c.Request().Context())
		return c.String(http.StatusOK, "served")
	}, mw)
	req := httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	if withCookie {
		req.AddCookie(&http.Cookie{Name: "weos-session", Value: "x"})
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec, seen, locked
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return body.Code
}

func TestErasureGuard_RefusesALockedAccountWithItsCode(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	locks := lockSet{"acct-harbor": true}
	rec, _, _ := serve(ErasureGuard(sm, locks, nopLogger{}), true, nil)
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountErasurePending {
		t.Fatalf("got %d %s, want 401 with code %s", rec.Code, rec.Body.String(), CodeAccountErasurePending)
	}
}

func TestErasureGuard_PassesAnUnlockedAccountAndBearerRequests(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	rec, _, _ := serve(ErasureGuard(sm, lockSet{}, nopLogger{}), true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("an unlocked account was refused: %d %s", rec.Code, rec.Body.String())
	}
	rec, _, _ = serve(ErasureGuard(sm, lockSet{"acct-harbor": true}, nopLogger{}), true,
		map[string]string{"Authorization": "Bearer token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("a bearer request was judged by the cookie: %d %s", rec.Code, rec.Body.String())
	}
	rec, _, _ = serve(ErasureGuard(sm, lockSet{"acct-harbor": true}, nopLogger{}), false, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a request with no cookie was refused by the guard: %d", rec.Code)
	}
}

func TestSessionAuthForErasure_AdmitsALockedAccountAndOnlyThat(t *testing.T) {
	data := &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}
	sm := cookieSessions{data: data}
	deactivated := validating{err: authapp.ErrSessionAccountDeactivated}
	book := accountBook{}

	rec, identity, locked := serve(SessionAuthForErasure(sm, deactivated, book, lockSet{"acct-harbor": true}, nopLogger{}), true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a locked account was not admitted to the deletion route: %d %s", rec.Code, rec.Body.String())
	}
	if identity == nil || identity.AgentID != "ops" || identity.ActiveAccountID != "acct-harbor" || !locked {
		t.Fatalf("identity = %+v locked=%v, want ops in acct-harbor, marked locked", identity, locked)
	}

	rec, _, _ = serve(SessionAuthForErasure(sm, deactivated, book, lockSet{}, nopLogger{}), true, nil)
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountDeactivated {
		t.Fatalf("a merely suspended account got %d %s, want 401 %s", rec.Code, rec.Body.String(), CodeAccountDeactivated)
	}
	rec, _, _ = serve(SessionAuthForErasure(sm, validating{err: authapp.ErrSessionAccountRevoked}, book, lockSet{}, nopLogger{}), true, nil)
	if codeOf(t, rec) != CodeAccountAccessRevoked {
		t.Fatalf("a revoked membership got %s, want %s", rec.Body.String(), CodeAccountAccessRevoked)
	}
	rec, _, _ = serve(SessionAuthForErasure(sm, validating{err: authapp.ErrSessionExpired}, book, lockSet{}, nopLogger{}), true, nil)
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "" {
		t.Fatalf("an expired session got %d %s, want a bare 401", rec.Code, rec.Body.String())
	}
	rec, _, _ = serve(SessionAuthForErasure(sm, deactivated, book, lockSet{}, nopLogger{}), false, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie got %d, want 401", rec.Code)
	}
}

func TestSessionAuthForErasure_UnscopedSessionSaysWhy(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "broker"}}
	unscoped := validating{info: &authapp.SessionInfo{SessionID: "s1", AgentID: "broker"}}
	suspended := account(t, "acct-cedar", false)
	lockedAccount := account(t, "acct-harbor", false)

	cases := []struct {
		name  string
		book  accountBook
		locks lockSet
		want  string
	}{
		{"only a suspended account", accountBook{members: map[string][]*authentities.Account{"broker": {suspended}}}, lockSet{}, CodeAccountDeactivated},
		{"a member of a locked account", accountBook{members: map[string][]*authentities.Account{"broker": {lockedAccount}}}, lockSet{"acct-harbor": true}, CodeAccountErasurePending},
		{"no account at all", accountBook{members: map[string][]*authentities.Account{}}, lockSet{}, CodeUnscopedSession},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, _, _ := serve(SessionAuthForErasure(sm, unscoped, tc.book, tc.locks, nopLogger{}), true, nil)
			if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != tc.want {
				t.Fatalf("got %d %s, want 401 with code %s", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

type claimsFor struct {
	authapp.JWTService
	claims *authapp.PericarpClaims
}

func (c claimsFor) ValidateToken(context.Context, string) (*authapp.PericarpClaims, error) {
	if c.claims == nil {
		return nil, errors.New("bad token")
	}
	return c.claims, nil
}

func TestBearerOrSession_ChecksTheTokensAccount(t *testing.T) {
	claims := &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor"}
	noSession := func(next http.Handler) http.Handler { return next }
	cases := []struct {
		name     string
		accounts accountBook
		locks    lockSet
		status   int
		code     string
	}{
		{"active", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)}}, lockSet{}, http.StatusOK, ""},
		{"gone", accountBook{accounts: map[string]*authentities.Account{}}, lockSet{}, http.StatusUnauthorized, ""},
		{"suspended", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", false)}}, lockSet{}, http.StatusUnauthorized, CodeAccountDeactivated},
		{"erasure locked", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", false)}}, lockSet{"acct-harbor": true}, http.StatusUnauthorized, CodeAccountErasurePending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mw := BearerOrSession(claimsFor{claims: claims}, noSession, "http://x", tc.accounts, tc.locks)
			rec, identity, _ := serve(mw, false, map[string]string{"Authorization": "Bearer t"})
			if rec.Code != tc.status {
				t.Fatalf("got %d %s, want %d", rec.Code, rec.Body.String(), tc.status)
			}
			if tc.status == http.StatusOK {
				if identity == nil || identity.ActiveAccountID != "acct-harbor" {
					t.Fatalf("identity = %+v, want the token's", identity)
				}
				return
			}
			if got := codeOf(t, rec); got != tc.code {
				t.Fatalf("code = %q, want %q (%s)", got, tc.code, rec.Body.String())
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("the refusal carries no WWW-Authenticate challenge")
			}
		})
	}
}
