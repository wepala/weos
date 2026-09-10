package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// impersonate runs one request as the admin "ops" impersonating "counsel",
// through Impersonation, and reports what the handler was given.
func impersonate(t *testing.T, book accountBook, locks lockSet) (*httptest.ResponseRecorder, *auth.Identity) {
	t.Helper()
	store := sessions.NewCookieStore([]byte("test-secret"))
	e := echo.New()
	var seen *auth.Identity
	admin := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{AgentID: "ops", ActiveAccountID: "acct-harbor"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
	e.GET("/api/thing", func(c echo.Context) error {
		seen = auth.AgentFromCtx(c.Request().Context())
		return c.String(http.StatusOK, "served")
	}, admin, Impersonation(store, book, locks, nopLogger{}))

	// Mint the impersonation cookie the way the start route does.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, _ := store.Get(req, ImpersonationSessionName)
	sess.Values[KeyImpersonatedAgentID] = "counsel"
	sess.Values[KeyRealAgentID] = "ops"
	if err := sess.Save(req, rec); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec, seen
}

func TestImpersonation_LandsInThePersonsFirstActiveAccount(t *testing.T) {
	book := accountBook{members: map[string][]*authentities.Account{
		"counsel": {account(t, "acct-old", false), account(t, "acct-cedar", true)},
	}}
	rec, seen := impersonate(t, book, lockSet{})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want the request served", rec.Code, rec.Body.String())
	}
	if seen == nil || seen.AgentID != "counsel" || seen.ActiveAccountID != "acct-cedar" {
		t.Fatalf("identity = %+v, want counsel in acct-cedar, the active account, not the inactive one listed first", seen)
	}
}

// wm-iiasy: while an account is locked for deletion it serves nothing but
// the deletion, an impersonating admin included.
func TestImpersonation_RefusesAMemberOfALockedAccountWithTheCode(t *testing.T) {
	book := accountBook{members: map[string][]*authentities.Account{"counsel": {account(t, "acct-cedar", false)}}}
	rec, _ := impersonate(t, book, lockSet{"acct-cedar": true})
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountErasurePending {
		t.Fatalf("got %d %s, want 401 %s", rec.Code, rec.Body.String(), CodeAccountErasurePending)
	}

	rec, _ = impersonate(t, book, lockSet{})
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != CodeAccountDeactivated {
		t.Fatalf("a suspended account got %d %s, want 401 %s", rec.Code, rec.Body.String(), CodeAccountDeactivated)
	}
}

func TestImpersonation_APersonWithNoAccountIsImpersonatedWithNone(t *testing.T) {
	rec, seen := impersonate(t, accountBook{members: map[string][]*authentities.Account{}}, lockSet{})
	if rec.Code != http.StatusOK || seen == nil || seen.AgentID != "counsel" || seen.ActiveAccountID != "" {
		t.Fatalf("got %d %s identity %+v, want counsel with no account", rec.Code, rec.Body.String(), seen)
	}
}
