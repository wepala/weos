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
