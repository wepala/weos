package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// impersonated is what one request through Impersonation reached: the answer,
// the identity the handler was given, and the identity the middleware kept as
// the impersonator. Both identities are nil when the handler was not reached.
type impersonated struct {
	rec          *httptest.ResponseRecorder
	seen         *auth.Identity
	impersonator *auth.Identity
}

// impersonate runs one request as "ops", signed in to acct-harbor, carrying an
// impersonation cookie that names "counsel", through Impersonation.
func impersonate(t *testing.T, book accountBook, locks lockSet) impersonated {
	t.Helper()
	return impersonateAs(t, "ops", book, locks)
}

// impersonateAs is impersonate with the person really signed in as caller; the
// cookie was still started by "ops".
func impersonateAs(t *testing.T, caller string, book accountBook, locks lockSet) impersonated {
	t.Helper()
	return impersonateIn(t, caller, "acct-harbor", "acct-harbor", book, locks)
}

// wm-dpzo5: an impersonation is judged in the account it started in. The
// caller acting in another account — where the same person is also a member
// and the caller is also an admin — does not carry it there, and a cookie
// that records no account is not applied anywhere.
func TestImpersonation_StaysInTheAccountItStartedIn(t *testing.T) {
	book := harborBook(t, map[string]string{
		"counsel|acct-harbor": authentities.RoleMember,
		"ops|acct-cedar":      authentities.RoleAdmin,
		"counsel|acct-cedar":  authentities.RoleMember,
	})
	book.accounts["acct-cedar"] = account(t, "acct-cedar", true)

	got := impersonateIn(t, "ops", "acct-cedar", "acct-harbor", book, lockSet{})
	if got.rec.Code != http.StatusForbidden || codeOf(t, got.rec) != CodeImpersonationTargetNotMember {
		t.Fatalf("acting in another account got %d %s identity %+v, want 403 %s",
			got.rec.Code, got.rec.Body.String(), got.seen, CodeImpersonationTargetNotMember)
	}
	if got.seen != nil || !impersonationCookieCleared(got.rec) {
		t.Fatalf("the handler was reached as %+v, cookie cleared=%v; want neither reached and the cookie cleared",
			got.seen, impersonationCookieCleared(got.rec))
	}

	got = impersonateIn(t, "ops", "acct-harbor", "", book, lockSet{})
	if got.rec.Code != http.StatusForbidden || got.seen != nil || !impersonationCookieCleared(got.rec) {
		t.Fatalf("a cookie that records no account got %d %s identity %+v, want 403, not reached, and cleared",
			got.rec.Code, got.rec.Body.String(), got.seen)
	}
}

// impersonateIn runs one request as caller acting in acting, carrying a cookie
// started by "ops" for "counsel" that records startedIn as its account.
func impersonateIn(t *testing.T, caller, acting, startedIn string, book accountBook, locks lockSet) impersonated {
	t.Helper()
	return impersonateFrom(t, caller, acting, startedIn, book, locks, nopLogger{}, nil)
}

// impersonateFrom is impersonateIn logged to logger, with prepare, when set,
// applied to the request before it is sent.
func impersonateFrom(t *testing.T, caller, acting, startedIn string, book accountBook, locks lockSet, logger entities.Logger, prepare func(*http.Request)) impersonated {
	t.Helper()
	store := sessions.NewCookieStore([]byte("test-secret"))
	e := echo.New()
	var got impersonated
	signedIn := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{AgentID: caller, ActiveAccountID: acting})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
	e.GET("/api/thing", func(c echo.Context) error {
		got.seen = auth.AgentFromCtx(c.Request().Context())
		got.impersonator = ImpersonatorFromCtx(c.Request().Context())
		return c.String(http.StatusOK, "served")
	}, signedIn, Impersonation(store, book, locks, logger))

	// Mint the impersonation cookie the way the start route does.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, _ := store.Get(req, ImpersonationSessionName)
	sess.Values[KeyImpersonatedAgentID] = "counsel"
	sess.Values[KeyRealAgentID] = "ops"
	sess.Values[KeyRealAccountID] = startedIn
	if err := sess.Save(req, rec); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	if prepare != nil {
		prepare(req)
	}
	got.rec = httptest.NewRecorder()
	e.ServeHTTP(got.rec, req)
	return got
}

// harborBook is an active Harbor Legal whose owner is ops, with the roles
// given added.
func harborBook(t *testing.T, roles map[string]string) accountBook {
	t.Helper()
	all := map[string]string{"ops|acct-harbor": authentities.RoleOwner}
	for k, v := range roles {
		all[k] = v
	}
	return accountBook{
		accounts: map[string]*authentities.Account{"acct-harbor": account(t, "acct-harbor", true)},
		roles:    all,
		members:  map[string][]*authentities.Account{},
	}
}

func impersonationCookieCleared(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == ImpersonationSessionName && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

// wm-ptcuk: the impersonation acts in the account the caller's authority
// covers, never in some other account the person also belongs to.
func TestImpersonation_ActsInTheCallersAccount(t *testing.T) {
	book := harborBook(t, map[string]string{"counsel|acct-harbor": authentities.RoleMember})
	book.members["counsel"] = []*authentities.Account{account(t, "acct-cedar", true), account(t, "acct-harbor", true)}
	got := impersonate(t, book, lockSet{})
	if got.rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want the request served", got.rec.Code, got.rec.Body.String())
	}
	if got.seen == nil || got.seen.AgentID != "counsel" || got.seen.ActiveAccountID != "acct-harbor" {
		t.Fatalf("identity = %+v, want counsel in acct-harbor, the caller's account, not counsel's own first account", got.seen)
	}
	if got.impersonator == nil || got.impersonator.AgentID != "ops" {
		t.Fatalf("impersonator = %+v, want ops, the person signed in", got.impersonator)
	}
}

// wm-ptcuk: a cookie whose person is not a member of the caller's account —
// one started before the start route checked, or whose person has since left —
// is refused and cleared, and the request is served as neither person.
func TestImpersonation_RefusesAndClearsACookieForAPersonOutsideTheAccount(t *testing.T) {
	book := harborBook(t, nil)
	book.members["counsel"] = []*authentities.Account{account(t, "acct-cedar", true)}
	got := impersonate(t, book, lockSet{})
	if got.rec.Code != http.StatusForbidden || codeOf(t, got.rec) != CodeImpersonationTargetNotMember {
		t.Fatalf("got %d %s, want 403 %s", got.rec.Code, got.rec.Body.String(), CodeImpersonationTargetNotMember)
	}
	if got.seen != nil {
		t.Fatalf("the handler was reached as %+v", got.seen)
	}
	if !impersonationCookieCleared(got.rec) {
		t.Fatal("the refused impersonation cookie was not cleared")
	}
}

// wm-ptcuk: the caller's role is judged on every request, so an admin demoted
// to member stops impersonating.
func TestImpersonation_RefusesACallerWhoseRoleNoLongerAllowsIt(t *testing.T) {
	book := harborBook(t, map[string]string{
		"ops|acct-harbor":     authentities.RoleMember,
		"counsel|acct-harbor": authentities.RoleMember,
	})
	got := impersonate(t, book, lockSet{})
	if got.rec.Code != http.StatusForbidden || codeOf(t, got.rec) != CodeImpersonationTargetNotMember {
		t.Fatalf("got %d %s, want 403 %s", got.rec.Code, got.rec.Body.String(), CodeImpersonationTargetNotMember)
	}
}

// wm-iiasy: while an account is locked for deletion it serves nothing but
// the deletion, an impersonating admin included.
func TestImpersonation_RefusesInAnInactiveAccountWithTheCode(t *testing.T) {
	book := harborBook(t, map[string]string{"counsel|acct-harbor": authentities.RoleMember})
	book.accounts["acct-harbor"] = account(t, "acct-harbor", false)
	got := impersonate(t, book, lockSet{"acct-harbor": true})
	if got.rec.Code != http.StatusUnauthorized || codeOf(t, got.rec) != CodeAccountErasurePending {
		t.Fatalf("got %d %s, want 401 %s", got.rec.Code, got.rec.Body.String(), CodeAccountErasurePending)
	}

	got = impersonate(t, book, lockSet{})
	if got.rec.Code != http.StatusUnauthorized || codeOf(t, got.rec) != CodeAccountDeactivated {
		t.Fatalf("a suspended account got %d %s, want 401 %s", got.rec.Code, got.rec.Body.String(), CodeAccountDeactivated)
	}
}

// A cookie started by somebody else is not applied: the request is served as
// the person signed in. It is expired as well (wm-ptcuk, Copilot review
// 5203947880), so it is not kept in the browser until the person who started it
// signs in there again and finds it applied.
func TestImpersonation_ACookieStartedByAnotherPersonIsIgnored(t *testing.T) {
	book := harborBook(t, map[string]string{"counsel|acct-harbor": authentities.RoleMember})
	got := impersonateAs(t, "broker", book, lockSet{})
	if got.rec.Code != http.StatusOK || got.seen == nil || got.seen.AgentID != "broker" || got.impersonator != nil {
		t.Fatalf("got %d identity %+v impersonator %+v, want broker served as broker", got.rec.Code, got.seen, got.impersonator)
	}
	if !impersonationCookieCleared(got.rec) {
		t.Fatal("a cookie started by another person was served past but not expired")
	}
}

// Expiring the cookie twice on one answer — the middleware, then a handler that
// ends the impersonation too — writes one Set-Cookie, not two.
func TestExpireImpersonationCookie_WritesOneCookieWhenCalledTwice(t *testing.T) {
	rec := httptest.NewRecorder()
	ExpireImpersonationCookie(rec)
	ExpireImpersonationCookie(rec)
	if got := rec.Header().Values("Set-Cookie"); len(got) != 1 {
		t.Fatalf("Set-Cookie written %d times, want once: %q", len(got), got)
	}
}

// Fail closed: roles that cannot be read are not known to allow anything.
func TestImpersonation_FailsClosedWhenTheRolesCannotBeRead(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret"))
	e := echo.New()
	reached := false
	signedIn := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{AgentID: "ops", ActiveAccountID: "acct-harbor"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
	e.GET("/api/thing", func(c echo.Context) error {
		reached = true
		return c.String(http.StatusOK, "served")
	}, signedIn, Impersonation(store, unreadableRoles{harborBook(t, nil)}, lockSet{}, nopLogger{}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, _ := store.Get(req, ImpersonationSessionName)
	sess.Values[KeyImpersonatedAgentID] = "counsel"
	sess.Values[KeyRealAgentID] = "ops"
	sess.Values[KeyRealAccountID] = "acct-harbor"
	if err := sess.Save(req, rec); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || reached {
		t.Fatalf("got %d %s reached=%v, want 503 and the handler not reached", rec.Code, rec.Body.String(), reached)
	}
	// The same answer BearerOrSession gives in front of it on the same group.
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("the 503 carries no Retry-After, unlike the bearer path's answer for an unreadable account state")
	}
}

func TestMayImpersonate(t *testing.T) {
	book := harborBook(t, map[string]string{
		"admin|acct-harbor":   authentities.RoleAdmin,
		"member|acct-harbor":  authentities.RoleMember,
		"counsel|acct-harbor": authentities.RoleMember,
		"broker|acct-cedar":   authentities.RoleOwner,
	})
	cases := []struct {
		name                    string
		account, caller, target string
		want                    bool
	}{
		{"an owner, a member of the account", "acct-harbor", "ops", "counsel", true},
		{"an admin, a member of the account", "acct-harbor", "admin", "counsel", true},
		{"a member may not", "acct-harbor", "member", "counsel", false},
		{"a person outside the account", "acct-harbor", "ops", "broker", false},
		{"a person who does not exist", "acct-harbor", "ops", "nobody", false},
		{"no account", "", "ops", "counsel", false},
		{"themselves", "acct-harbor", "ops", "ops", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := MayImpersonate(context.Background(), book, c.account, c.caller, c.target)
			if err != nil || got != c.want {
				t.Fatalf("MayImpersonate = %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

// recordedLog records every line the middleware logs.
type recordedLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *recordedLog) add(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf("%s: %s %v", level, msg, fields))
}

func (l *recordedLog) Debug(_ context.Context, m string, f ...any) { l.add("debug", m, f) }
func (l *recordedLog) Info(_ context.Context, m string, f ...any)  { l.add("info", m, f) }
func (l *recordedLog) Warn(_ context.Context, m string, f ...any)  { l.add("warn", m, f) }
func (l *recordedLog) Error(_ context.Context, m string, f ...any) { l.add("error", m, f) }

func (l *recordedLog) mentioning(s string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			out = append(out, line)
		}
	}
	return out
}

// sentThroughAProxyHeader is a request from 192.0.2.10 whose forwarding headers
// claim other addresses. serve configures no trusted proxy, so any caller can
// write those headers.
func sentThroughAProxyHeader(r *http.Request) {
	r.RemoteAddr = "192.0.2.10:52814"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.Header.Set("X-Real-IP", "198.51.100.7")
}

// wm-ptcuk, Copilot review 5204289188. The middleware's impersonation lines
// record the address of the connection's peer, not an address a forwarding
// header claims, so a caller cannot choose the address recorded against them.
func TestImpersonation_RecordsTheConnectionPeerNotAForwardedAddress(t *testing.T) {
	cases := []struct {
		name, caller, acting, line string
		book                       accountBook
	}{
		{"another person is signed in", "broker", "acct-harbor", "another person is signed in",
			harborBook(t, map[string]string{"counsel|acct-harbor": authentities.RoleMember})},
		{"the caller acts in another account", "ops", "acct-cedar", "acts in another account",
			harborBook(t, map[string]string{"counsel|acct-harbor": authentities.RoleMember})},
		{"the person is not a member", "ops", "acct-harbor", "is not a member of the caller's account",
			harborBook(t, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := &recordedLog{}
			impersonateFrom(t, tc.caller, tc.acting, "acct-harbor", tc.book, lockSet{}, logs, sentThroughAProxyHeader)
			lines := logs.mentioning(tc.line)
			if len(lines) != 1 {
				t.Fatalf("want one line mentioning %q, got %q", tc.line, lines)
			}
			if !strings.Contains(lines[0], "ip 192.0.2.10") {
				t.Errorf("the line %q does not record the connection's peer 192.0.2.10", lines[0])
			}
			for _, claimed := range []string{"203.0.113.9", "198.51.100.7"} {
				if strings.Contains(lines[0], claimed) {
					t.Errorf("the line %q records %s, an address a forwarding header claimed", lines[0], claimed)
				}
			}
		})
	}
}

func TestConnectionPeer(t *testing.T) {
	cases := []struct{ remote, want string }{
		{"192.0.2.10:52814", "192.0.2.10"},
		{"[2001:db8::7]:443", "2001:db8::7"},
		{"192.0.2.10", "192.0.2.10"},
		{"", ""},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		if got := ConnectionPeer(r); got != c.want {
			t.Errorf("ConnectionPeer(%q) = %q, want %q", c.remote, got, c.want)
		}
	}
}
