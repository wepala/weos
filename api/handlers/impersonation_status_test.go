package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// readStatusAs sends GET /api/admin/impersonation-status as caller, straight to
// the handler, carrying an impersonation cookie started by ops for counsel when
// withCookie is set. Nothing applied the cookie, as when the impersonation
// middleware passed it through.
func readStatusAs(t *testing.T, caller string, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()
	store := sessions.NewCookieStore([]byte("test-secret"))
	h := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store:       store,
		AccountRepo: stubAccounts{},
		CredRepo:    stubCreds{},
		Logger:      nopLogger{},
	})
	signedIn := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{AgentID: caller, ActiveAccountID: "acct-harbor"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
	e := echo.New()
	e.GET("/api/admin/impersonation-status", h.Status, signedIn)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/impersonation-status", nil)
	if withCookie {
		minted := httptest.NewRecorder()
		sess, err := store.New(req, apimw.ImpersonationSessionName)
		if err != nil {
			t.Fatalf("start the impersonation session: %v", err)
		}
		sess.Values[apimw.KeyImpersonatedAgentID] = "counsel"
		sess.Values[apimw.KeyRealAgentID] = "ops"
		sess.Values[apimw.KeyRealAccountID] = "acct-harbor"
		if err := sess.Save(req, minted); err != nil {
			t.Fatalf("save the impersonation session: %v", err)
		}
		for _, c := range minted.Result().Cookies() {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func statusCookieCleared(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == apimw.ImpersonationSessionName && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

// wm-ptcuk, Copilot review 5203947880. A status read whose impersonation cookie
// was not applied — another person started it — answers inactive, names nobody,
// and expires the cookie, so the browser does not send it again on every
// request after.
func TestStatus_ExpiresAnImpersonationCookieItDidNotApply(t *testing.T) {
	rec := readStatusAs(t, "broker", true)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status answered %d %s", rec.Code, rec.Body.String())
	}
	if envelope.Data["active"] != false {
		t.Fatalf("status answered %s, want active false", rec.Body.String())
	}
	if _, named := envelope.Data["user"]; named || strings.Contains(rec.Body.String(), "counsel") {
		t.Fatalf("status for a cookie it did not apply names a person: %s", rec.Body.String())
	}
	if !statusCookieCleared(rec) {
		t.Fatal("status did not expire an impersonation cookie it did not apply")
	}

	// A request that carries no impersonation cookie gets no cookie written.
	plain := readStatusAs(t, "broker", false)
	if plain.Code != http.StatusOK || plain.Header().Get("Set-Cookie") != "" {
		t.Fatalf("status with no impersonation cookie answered %d and set %q, want 200 and no cookie",
			plain.Code, plain.Header().Get("Set-Cookie"))
	}
}
