package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/wepala/weos/v3/application"
)

// runCallback drives authCallbackHandler with a stub inner handler and returns
// the recorded response.
func runCallback(t *testing.T, req *http.Request, inner http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	if err := authCallbackHandler(inner)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return rec
}

func redirectInner(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://app.example/", http.StatusFound)
}

func TestAuthCallbackHandler_AppendsNewAccountMarkerOnFirstSignup(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		if flag := application.NewAccountFlagFromContext(r.Context()); flag != nil {
			*flag = true // simulate FindOrCreateAgent creating a fresh account
		}
		redirectInner(w, r)
	}

	rec := runCallback(t, httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc&state=xyz", nil), inner)

	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("bad Location %q: %v", loc, err)
	}
	if got := u.Query().Get("new_account"); got != "1" {
		t.Errorf("Location = %q, want new_account=1", loc)
	}
}

func TestAuthCallbackHandler_NoMarkerForReturningUser(t *testing.T) {
	// inner never sets the flag → returning login.
	rec := runCallback(t, httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc&state=xyz", nil), redirectInner)

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "new_account") {
		t.Errorf("returning user redirect should not carry new_account: %q", loc)
	}
}

func TestAuthCallbackHandler_PromotesApplePostFormToQuery(t *testing.T) {
	var seenCode, seenState string
	inner := func(w http.ResponseWriter, r *http.Request) {
		seenCode = r.URL.Query().Get("code")
		seenState = r.URL.Query().Get("state")
		redirectInner(w, r)
	}

	body := strings.NewReader(url.Values{"code": {"apple-code"}, "state": {"apple-state"}}.Encode())
	req := httptest.NewRequest(http.MethodPost, "/api/auth/callback", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	runCallback(t, req, inner)

	if seenCode != "apple-code" || seenState != "apple-state" {
		t.Errorf("form_post fields not promoted to query: code=%q state=%q", seenCode, seenState)
	}
}

func TestAuthCallbackHandler_LeavesJSONErrorResponseUntouched(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		// Even if a fresh-account flag were set, a non-redirect response must
		// pass through verbatim.
		if flag := application.NewAccountFlagFromContext(r.Context()); flag != nil {
			*flag = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}

	rec := runCallback(t, httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil), inner)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"error":"boom"}` {
		t.Errorf("body = %q, want untouched JSON error", body)
	}
	if rec.Header().Get("Location") != "" {
		t.Errorf("error response should not have a Location header")
	}
}
