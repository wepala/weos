package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
	}, signedIn, Impersonation(store, book, locks, nopLogger{}))

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
// the person signed in.
func TestImpersonation_ACookieStartedByAnotherPersonIsIgnored(t *testing.T) {
	book := harborBook(t, map[string]string{"counsel|acct-harbor": authentities.RoleMember})
	got := impersonateAs(t, "broker", book, lockSet{})
	if got.rec.Code != http.StatusOK || got.seen == nil || got.seen.AgentID != "broker" || got.impersonator != nil {
		t.Fatalf("got %d identity %+v impersonator %+v, want broker served as broker", got.rec.Code, got.seen, got.impersonator)
	}
}

type unreadableRoles struct{ accountBook }

func (unreadableRoles) FindMemberRole(context.Context, string, string) (string, error) {
	return "", errors.New("database unavailable")
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
