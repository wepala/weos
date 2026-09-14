package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// countingLocks records how often the lock was read, so a test can say the
// guard asked only when it had to.
type countingLocks struct {
	lockSet
	reads int
	err   error
}

func (l *countingLocks) IsLocked(ctx context.Context, id string) (bool, error) {
	l.reads++
	if l.err != nil {
		return false, l.err
	}
	return l.lockSet.IsLocked(ctx, id)
}

// refusing stands in for RequireAuth: it writes the 401 pericarp writes, with
// the code given, or serves when the code is empty.
func refusing(code string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if code == "" {
				return next(c)
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "not authenticated", "code": code})
		}
	}
}

// serveGuarded runs one request through the guard with a stand-in for
// RequireAuth behind it.
func serveGuarded(guard echo.MiddlewareFunc, auth echo.MiddlewareFunc, withCookie bool, headers map[string]string) *httptest.ResponseRecorder {
	e := echo.New()
	e.GET("/api/thing", func(c echo.Context) error {
		return c.String(http.StatusOK, "served")
	}, guard, auth)
	req := httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	if withCookie {
		req.AddCookie(&http.Cookie{Name: "weos-session", Value: "x"})
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestErasureGuard_RewritesADeactivatedRefusalForALockedAccount(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	locks := &countingLocks{lockSet: lockSet{"acct-harbor": true}}
	rec := serveGuarded(ErasureGuard(sm, locks, nopLogger{}), refusing(CodeAccountDeactivated), true, nil)
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountErasurePending {
		t.Fatalf("got %d %s, want 401 with code %s", rec.Code, rec.Body.String(), CodeAccountErasurePending)
	}
	if rec.Header().Get(echo.HeaderContentType) == "" {
		t.Error("the rewritten refusal lost its content type")
	}
	// wm-6umqn: a bearer header beside the cookie changes nothing on a group
	// that authenticates by session.
	rec = serveGuarded(ErasureGuard(sm, locks, nopLogger{}), refusing(CodeAccountDeactivated), true,
		map[string]string{"Authorization": "Bearer stale"})
	if codeOf(t, rec) != CodeAccountErasurePending {
		t.Fatalf("a bearer header beside the cookie got %s, want %s", rec.Body.String(), CodeAccountErasurePending)
	}
}

func TestErasureGuard_LeavesEveryOtherAnswerAloneAndAsksNothing(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	locks := &countingLocks{lockSet: lockSet{"acct-harbor": true}}

	rec := serveGuarded(ErasureGuard(sm, locks, nopLogger{}), refusing(""), true, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "served" {
		t.Fatalf("a served request was changed: %d %s", rec.Code, rec.Body.String())
	}
	rec = serveGuarded(ErasureGuard(sm, locks, nopLogger{}), refusing(CodeAccountAccessRevoked), true, nil)
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountAccessRevoked {
		t.Fatalf("a revoked refusal was changed: %d %s", rec.Code, rec.Body.String())
	}
	if locks.reads != 0 {
		t.Errorf("the lock was read %d time(s) for requests that were not refused as deactivated; want 0 (wm-tsugz)", locks.reads)
	}

	// A merely suspended account keeps its code.
	unlocked := &countingLocks{lockSet: lockSet{}}
	rec = serveGuarded(ErasureGuard(sm, unlocked, nopLogger{}), refusing(CodeAccountDeactivated), true, nil)
	if codeOf(t, rec) != CodeAccountDeactivated {
		t.Fatalf("a suspended account's refusal was rewritten: %s", rec.Body.String())
	}
	// No cookie: nothing to look up, the refusal stands.
	rec = serveGuarded(ErasureGuard(sm, locks, nopLogger{}), refusing(CodeAccountDeactivated), false, nil)
	if codeOf(t, rec) != CodeAccountDeactivated {
		t.Fatalf("a refusal with no cookie was rewritten: %s", rec.Body.String())
	}
}

func TestErasureGuard_DefersToTheBearerPathWhenTold(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	locks := &countingLocks{lockSet: lockSet{"acct-harbor": true}}
	rec := serveGuarded(ErasureGuard(sm, locks, nopLogger{}, DeferToBearer()), refusing(CodeAccountDeactivated), true,
		map[string]string{"Authorization": "Bearer token"})
	if codeOf(t, rec) != CodeAccountDeactivated || locks.reads != 0 {
		t.Fatalf("a bearer request on a bearer group was judged by the cookie: %s (lock reads %d)", rec.Body.String(), locks.reads)
	}
}

func TestErasureGuard_FailsClosedWhenTheLockCannotBeRead(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "ops", AccountID: "acct-harbor"}}
	locks := &countingLocks{lockSet: lockSet{}, err: errors.New("database away")}
	rec := serveGuarded(ErasureGuard(sm, locks, nopLogger{}), refusing(CodeAccountDeactivated), true, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %s, want 503 when the lock cannot be read", rec.Code, rec.Body.String())
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

// wm-or9a5: a provider sign-in resolves no account for a locked one, so the
// session names none. The route admits an owner of a locked account from
// that session, and nobody else.
func TestSessionAuthForErasure_AdmitsAnOwnerOfALockedAccountFromAnUnscopedSession(t *testing.T) {
	sm := cookieSessions{data: &session.SessionData{SessionID: "s1", AgentID: "ops"}}
	unscoped := validating{info: &authapp.SessionInfo{SessionID: "s1", AgentID: "ops"}}
	lockedAccount := account(t, "acct-harbor", false)
	book := accountBook{
		members: map[string][]*authentities.Account{"ops": {lockedAccount}},
		roles:   map[string]string{"ops|acct-harbor": authentities.RoleOwner},
	}

	rec, identity, locked := serve(SessionAuthForErasure(sm, unscoped, book, lockSet{"acct-harbor": true}, nopLogger{}), true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("the owner of a locked account was not admitted from an unscoped session: %d %s", rec.Code, rec.Body.String())
	}
	if identity == nil || identity.AgentID != "ops" || identity.ActiveAccountID != "acct-harbor" || !locked {
		t.Fatalf("identity = %+v locked=%v, want ops in acct-harbor, marked locked", identity, locked)
	}

	member := accountBook{members: book.members, roles: map[string]string{"ops|acct-harbor": "member"}}
	rec, _, _ = serve(SessionAuthForErasure(sm, unscoped, member, lockSet{"acct-harbor": true}, nopLogger{}), true, nil)
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountErasurePending {
		t.Fatalf("a plain member got %d %s, want 401 %s", rec.Code, rec.Body.String(), CodeAccountErasurePending)
	}
	rec, _, _ = serve(SessionAuthForErasure(sm, unscoped, book, lockSet{}, nopLogger{}), true, nil)
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountDeactivated {
		t.Fatalf("the owner of a merely suspended account got %d %s, want 401 %s", rec.Code, rec.Body.String(), CodeAccountDeactivated)
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
	owner := map[string]string{"ops|acct-harbor": authentities.RoleOwner}
	cases := []struct {
		name     string
		accounts accountBook
		locks    lockSet
		status   int
		code     string
	}{
		{"active", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)}, roles: owner}, lockSet{}, http.StatusOK, ""},
		{"gone", accountBook{accounts: map[string]*authentities.Account{}, roles: owner}, lockSet{}, http.StatusUnauthorized, ""},
		{"suspended", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", false)}, roles: owner}, lockSet{}, http.StatusUnauthorized, CodeAccountDeactivated},
		{"erasure locked", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", false)}, roles: owner}, lockSet{"acct-harbor": true}, http.StatusUnauthorized, CodeAccountErasurePending},
		// wm-jwojd: the session path refuses a person removed from the account
		// with account_access_revoked, and before it says anything about the
		// account's own state.
		{"member removed", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)}}, lockSet{}, http.StatusUnauthorized, CodeAccountAccessRevoked},
		{"member removed from a suspended account", accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", false)}}, lockSet{}, http.StatusUnauthorized, CodeAccountAccessRevoked},
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

// unreadableRoles is an account book whose membership read fails.
type unreadableRoles struct{ accountBook }

func (unreadableRoles) FindMemberRole(context.Context, string, string) (string, error) {
	return "", errors.New("database away")
}

// wm-jwojd: a membership that cannot be read is not known to be good, so the
// token is refused as an unreadable account state is.
func TestBearerOrSession_FailsClosedWhenTheMembershipCannotBeRead(t *testing.T) {
	claims := &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor"}
	noSession := func(next http.Handler) http.Handler { return next }
	book := unreadableRoles{accountBook{accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)}}}
	rec, identity, _ := serve(BearerOrSession(claimsFor{claims: claims}, noSession, "http://x", book, lockSet{}), false,
		map[string]string{"Authorization": "Bearer t"})
	if rec.Code != http.StatusServiceUnavailable || identity != nil {
		t.Fatalf("got %d %s (identity %+v), want 503 and no identity", rec.Code, rec.Body.String(), identity)
	}
}

// wm-92vba: a token that names no account is refused as the session path
// refuses a session that names none — 401 {"error":"not authenticated",
// "code":"unscoped_session"} — with the challenge a refused token carries, on
// the protected routes and on the deletion alike. Before, it was admitted with
// an identity that named no account.
func TestBearerOrSession_RefusesATokenThatNamesNoAccount(t *testing.T) {
	unscoped := &authapp.PericarpClaims{AgentID: "ops"}
	book := accountBook{
		accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)},
		roles:    map[string]string{"ops|acct-harbor": authentities.RoleOwner},
		members:  map[string][]*authentities.Account{"ops": {account(t, "acct-harbor", true)}},
	}
	noSession := func(next http.Handler) http.Handler { return next }
	sessionAsked := false
	bySession := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error { sessionAsked = true; return next(c) }
	}
	for name, mw := range map[string]echo.MiddlewareFunc{
		"BearerOrSession":           BearerOrSession(claimsFor{claims: unscoped}, noSession, "http://x", book, lockSet{}),
		"BearerOrSessionForErasure": BearerOrSessionForErasure(claimsFor{claims: unscoped}, "http://x", book, lockSet{}, bySession),
	} {
		t.Run(name, func(t *testing.T) {
			rec, identity, _ := serve(mw, true, map[string]string{"Authorization": "Bearer t"})
			if rec.Code != http.StatusUnauthorized || identity != nil || sessionAsked {
				t.Fatalf("got %d %s (identity %+v, session asked %v), want 401 with no identity and no session asked",
					rec.Code, rec.Body.String(), identity, sessionAsked)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
			}
			if body["error"] != "not authenticated" || body["code"] != CodeUnscopedSession || len(body) != 2 {
				t.Fatalf("body = %s, want the session path's {\"error\":\"not authenticated\",\"code\":%q}", rec.Body.String(), CodeUnscopedSession)
			}
			if !strings.Contains(rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
				t.Errorf("challenge = %q, want invalid_token", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

// wm-8i8ln: a token the OAuth token endpoint issued to a connector carries
// token_use=oauth. A group that passed RefuseConnectorTokens refuses it with
// 403 token_not_allowed and an insufficient_scope challenge; a group that did
// not takes it; and a token without the mark is taken everywhere.
func TestBearerOrSession_RefusesAConnectorsTokenOnlyWhereTold(t *testing.T) {
	connector := &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor",
		Extras: map[string]any{"token_use": "oauth"}}
	native := &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor"}
	book := accountBook{
		accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)},
		roles:    map[string]string{"ops|acct-harbor": authentities.RoleOwner},
	}
	noSession := func(next http.Handler) http.Handler { return next }
	bySession := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	cases := []struct {
		name    string
		mw      echo.MiddlewareFunc
		refused bool
	}{
		{"a connector's token where connectors are taken", BearerOrSession(claimsFor{claims: connector}, noSession, "http://x", book, lockSet{}), false},
		{"a connector's token where they are refused", BearerOrSession(claimsFor{claims: connector}, noSession, "http://x", book, lockSet{}, RefuseConnectorTokens()), true},
		{"a native token where connectors are refused", BearerOrSession(claimsFor{claims: native}, noSession, "http://x", book, lockSet{}, RefuseConnectorTokens()), false},
		{"a connector's token on the deletion", BearerOrSessionForErasure(claimsFor{claims: connector}, "http://x", book, lockSet{}, bySession, RefuseConnectorTokens()), true},
		{"a native token on the deletion", BearerOrSessionForErasure(claimsFor{claims: native}, "http://x", book, lockSet{}, bySession, RefuseConnectorTokens()), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, identity, _ := serve(tc.mw, false, map[string]string{"Authorization": "Bearer t"})
			if !tc.refused {
				if rec.Code != http.StatusOK || identity == nil || identity.AgentID != "ops" {
					t.Fatalf("got %d %s (identity %+v), want 200 for ops", rec.Code, rec.Body.String(), identity)
				}
				return
			}
			if rec.Code != http.StatusForbidden || identity != nil || codeOf(t, rec) != CodeTokenNotAllowed {
				t.Fatalf("got %d %s (identity %+v), want 403 %s and no identity", rec.Code, rec.Body.String(), identity, CodeTokenNotAllowed)
			}
			if challenge := rec.Header().Get("WWW-Authenticate"); !strings.Contains(challenge, `error="insufficient_scope"`) {
				t.Fatalf("challenge = %q, want insufficient_scope", challenge)
			}
		})
	}
}

// wm-aj2eb: the deletion route takes a token as SessionAuthForErasure takes a
// session. A token for an account whose erasure is unfinished is admitted and
// marked, so the deletion can run again; everything else the bearer path
// refuses stays refused, and a request with no token goes to the session auth.
func TestBearerOrSessionForErasure_TakesATokenAsTheDeletionTakesASession(t *testing.T) {
	claims := &authapp.PericarpClaims{AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor"}
	owner := map[string]string{"ops|acct-harbor": authentities.RoleOwner}
	active := map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)}
	inactive := map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", false)}
	cases := []struct {
		name   string
		book   accountBook
		locks  lockSet
		status int
		code   string
		locked bool
	}{
		{"active", accountBook{accounts: active, roles: owner}, lockSet{}, http.StatusOK, "", false},
		{"erasure locked", accountBook{accounts: inactive, roles: owner}, lockSet{"acct-harbor": true}, http.StatusOK, "", true},
		{"suspended", accountBook{accounts: inactive, roles: owner}, lockSet{}, http.StatusUnauthorized, CodeAccountDeactivated, false},
		{"member removed from a locked account", accountBook{accounts: inactive}, lockSet{"acct-harbor": true}, http.StatusUnauthorized, CodeAccountAccessRevoked, false},
		{"gone", accountBook{accounts: map[string]*authentities.Account{}, roles: owner}, lockSet{}, http.StatusUnauthorized, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionAsked := false
			sessionAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					sessionAsked = true
					return next(c)
				}
			}
			mw := BearerOrSessionForErasure(claimsFor{claims: claims}, "http://x", tc.book, tc.locks, sessionAuth)
			rec, identity, locked := serve(mw, true, map[string]string{"Authorization": "Bearer t"})
			if sessionAsked {
				t.Fatalf("a request with a token was handed to the session auth")
			}
			if rec.Code != tc.status {
				t.Fatalf("got %d %s, want %d", rec.Code, rec.Body.String(), tc.status)
			}
			if tc.status == http.StatusOK {
				if identity == nil || identity.AgentID != "ops" || identity.ActiveAccountID != "acct-harbor" || locked != tc.locked {
					t.Fatalf("identity = %+v locked=%v, want ops in acct-harbor, locked=%v", identity, locked, tc.locked)
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

	t.Run("an invalid token is refused, not handed to the session", func(t *testing.T) {
		sessionAsked := false
		sessionAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { sessionAsked = true; return next(c) }
		}
		mw := BearerOrSessionForErasure(claimsFor{}, "http://x", accountBook{}, lockSet{}, sessionAuth)
		rec, _, _ := serve(mw, true, map[string]string{"Authorization": "Bearer t"})
		if rec.Code != http.StatusUnauthorized || sessionAsked {
			t.Fatalf("got %d %s (session asked %v), want 401 without asking the session", rec.Code, rec.Body.String(), sessionAsked)
		}
	})

	t.Run("no token goes to the session auth", func(t *testing.T) {
		sessionAsked := false
		sessionAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { sessionAsked = true; return next(c) }
		}
		mw := BearerOrSessionForErasure(claimsFor{claims: claims}, "http://x", accountBook{}, lockSet{}, sessionAuth)
		rec, _, _ := serve(mw, true, nil)
		if rec.Code != http.StatusOK || !sessionAsked {
			t.Fatalf("got %d %s (session asked %v), want the session auth to decide", rec.Code, rec.Body.String(), sessionAsked)
		}
	})
}
