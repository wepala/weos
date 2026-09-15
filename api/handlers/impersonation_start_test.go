package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

// agentsThatFail cannot read any agent, as when the store is unavailable.
type agentsThatFail struct {
	authrepos.AgentRepository
}

func (agentsThatFail) FindByID(context.Context, string) (*authentities.Agent, error) {
	return nil, errors.New("database unavailable")
}

// startLog records every line the start route logs.
type startLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *startLog) add(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf("%s: %s %v", level, msg, fields))
}

func (l *startLog) Debug(_ context.Context, m string, f ...any) { l.add("debug", m, f) }
func (l *startLog) Info(_ context.Context, m string, f ...any)  { l.add("info", m, f) }
func (l *startLog) Warn(_ context.Context, m string, f ...any)  { l.add("warn", m, f) }
func (l *startLog) Error(_ context.Context, m string, f ...any) { l.add("error", m, f) }

func (l *startLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// startImpersonationAs sends POST /api/admin/impersonate for target as ops,
// the owner of Harbor Legal, where counsel is a member and broker is not.
func startImpersonationAs(t *testing.T, agents authrepos.AgentRepository, logger entities.Logger, target string) *httptest.ResponseRecorder {
	t.Helper()
	return startImpersonationHolding(t, agents, logger, target, false)
}

// startImpersonationHolding is startImpersonationAs sent, when holding is set,
// with the cookie of an impersonation of counsel that ops already holds.
func startImpersonationHolding(t *testing.T, agents authrepos.AgentRepository, logger entities.Logger, target string, holding bool) *httptest.ResponseRecorder {
	t.Helper()
	store := sessions.NewCookieStore([]byte("test-secret"))
	h := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store: store,
		AccountRepo: stubAccounts{roles: map[string]string{
			"ops|acct-harbor":     authentities.RoleOwner,
			"counsel|acct-harbor": authentities.RoleMember,
			"broker|acct-cedar":   authentities.RoleMember,
		}},
		AgentRepo: agents,
		CredRepo:  stubCreds{},
		Logger:    logger,
	})
	signedIn := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{AgentID: "ops", ActiveAccountID: "acct-harbor"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
	e := echo.New()
	e.POST("/api/admin/impersonate", h.Start, signedIn)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/impersonate", strings.NewReader(fmt.Sprintf(`{"agent_id":%q}`, target)))
	req.Header.Set("Content-Type", "application/json")
	if holding {
		minted := httptest.NewRecorder()
		sess, err := store.New(req, apimw.ImpersonationSessionName)
		if err != nil {
			t.Fatalf("start the held impersonation session: %v", err)
		}
		sess.Values[apimw.KeyImpersonatedAgentID] = "counsel"
		sess.Values[apimw.KeyRealAgentID] = "ops"
		sess.Values[apimw.KeyRealAccountID] = "acct-harbor"
		if err := sess.Save(req, minted); err != nil {
			t.Fatalf("save the held impersonation session: %v", err)
		}
		for _, c := range minted.Result().Cookies() {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// wm-ptcuk, Copilot review 5203947880. The refusal code tells the admin the
// impersonation ended, and the admin then reads the identity again. A start
// refused with that code while an impersonation is held therefore ends the held
// one, or the identity read would put its banner back. A person outside the
// account and a person who does not exist still get one identical answer.
func TestStart_ARefusalWithTheCodeEndsTheImpersonationHeld(t *testing.T) {
	outside := startImpersonationHolding(t, stubAgents{}, nopLogger{}, "broker", true)
	if outside.Code != http.StatusForbidden || meCode(t, outside) != apimw.CodeImpersonationTargetNotMember {
		t.Fatalf("a person outside the account got %d %s, want 403 %s",
			outside.Code, outside.Body.String(), apimw.CodeImpersonationTargetNotMember)
	}
	if !statusCookieCleared(outside) {
		t.Fatal("the refused start left the held impersonation cookie in place")
	}
	unknown := startImpersonationHolding(t, stubAgents{}, nopLogger{}, "nobody", true)
	if unknown.Code != outside.Code || unknown.Body.String() != outside.Body.String() ||
		unknown.Header().Get("Set-Cookie") != outside.Header().Get("Set-Cookie") {
		t.Fatalf("an unknown person got %d %s %q; a person outside the account got %d %s %q",
			unknown.Code, unknown.Body.String(), unknown.Header().Get("Set-Cookie"),
			outside.Code, outside.Body.String(), outside.Header().Get("Set-Cookie"))
	}

	// With no impersonation held, the refusal writes no cookie.
	plain := startImpersonationAs(t, stubAgents{}, nopLogger{}, "broker")
	if plain.Code != http.StatusForbidden || plain.Header().Get("Set-Cookie") != "" {
		t.Fatalf("a refused start with no impersonation held answered %d and set %q, want 403 and no cookie",
			plain.Code, plain.Header().Get("Set-Cookie"))
	}
}

// wm-ljypy. When the person is a member of the caller's account but their
// record cannot be read, the start fails as a failure — a 500, logged — and is
// not answered as the refusal a person outside the account gets.
func TestStart_AFailedLookupOfAMemberIsAFailureNotARefusal(t *testing.T) {
	logs := &startLog{}
	rec := startImpersonationAs(t, agentsThatFail{}, logs, "counsel")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a member whose record cannot be read got %d %s, want 500", rec.Code, rec.Body.String())
	}
	if code := meCode(t, rec); code == apimw.CodeImpersonationTargetNotMember {
		t.Fatalf("a member whose record cannot be read was answered with %s", code)
	}
	if !strings.Contains(logs.text(), "error: ") || !strings.Contains(logs.text(), "database unavailable") {
		t.Fatalf("the failed lookup was not logged as an error:\n%s", logs.text())
	}

	// A person outside the account and a person who does not exist still get
	// one identical refusal: neither is looked up.
	outside := startImpersonationAs(t, agentsThatFail{}, nopLogger{}, "broker")
	unknown := startImpersonationAs(t, agentsThatFail{}, nopLogger{}, "nobody")
	if outside.Code != http.StatusForbidden || meCode(t, outside) != apimw.CodeImpersonationTargetNotMember {
		t.Fatalf("a person outside the account got %d %s, want 403 %s",
			outside.Code, outside.Body.String(), apimw.CodeImpersonationTargetNotMember)
	}
	if unknown.Code != outside.Code || unknown.Body.String() != outside.Body.String() {
		t.Fatalf("an unknown person got %d %s; a person outside the account got %d %s",
			unknown.Code, unknown.Body.String(), outside.Code, outside.Body.String())
	}
}

// auditedImpersonationRequest sends one start (for target) or stop through the
// impersonation handler, signed in as caller acting in Harbor Legal, from the
// peer 192.0.2.10 with forwarding headers that claim other addresses, as any
// caller can write them. When heldBy is set the request carries an
// impersonation of counsel that heldBy started.
func auditedImpersonationRequest(t *testing.T, logger entities.Logger, caller string, stop bool, target, heldBy string) *httptest.ResponseRecorder {
	t.Helper()
	store := sessions.NewCookieStore([]byte("test-secret"))
	h := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store: store,
		AccountRepo: stubAccounts{roles: map[string]string{
			"ops|acct-harbor":     authentities.RoleOwner,
			"counsel|acct-harbor": authentities.RoleMember,
			"broker|acct-cedar":   authentities.RoleMember,
		}},
		AgentRepo: stubAgents{},
		CredRepo:  stubCreds{},
		Logger:    logger,
	})
	signedIn := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{AgentID: caller, ActiveAccountID: "acct-harbor"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
	e := echo.New()
	e.POST("/api/admin/impersonate", h.Start, signedIn)
	e.POST("/api/admin/stop-impersonation", h.Stop, signedIn)
	path, body := "/api/admin/impersonate", fmt.Sprintf(`{"agent_id":%q}`, target)
	if stop {
		path, body = "/api/admin/stop-impersonation", ""
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if heldBy != "" {
		minted := httptest.NewRecorder()
		sess, err := store.New(req, apimw.ImpersonationSessionName)
		if err != nil {
			t.Fatalf("start the held impersonation session: %v", err)
		}
		sess.Values[apimw.KeyImpersonatedAgentID] = "counsel"
		sess.Values[apimw.KeyRealAgentID] = heldBy
		sess.Values[apimw.KeyRealAccountID] = "acct-harbor"
		if err := sess.Save(req, minted); err != nil {
			t.Fatalf("save the held impersonation session: %v", err)
		}
		for _, c := range minted.Result().Cookies() {
			req.AddCookie(c)
		}
	}
	req.RemoteAddr = "192.0.2.10:52814"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Real-IP", "198.51.100.7")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// wm-ptcuk, Copilot review 5204289188. The handler's impersonation lines record
// the address of the connection's peer, not an address a forwarding header
// claims, so a caller cannot choose the address recorded against them.
func TestImpersonationAudit_RecordsTheConnectionPeerNotAForwardedAddress(t *testing.T) {
	cases := []struct {
		name, caller string
		stop         bool
		target       string
		heldBy       string
		line         string
	}{
		{"a member asks to start", "counsel", false, "ops", "", "not an owner or admin"},
		{"a start for a person outside the account", "ops", false, "broker", "", "is not a member of the caller's account"},
		{"a stop", "ops", true, "", "ops", "impersonation stopped"},
		{"a stop of another person's impersonation", "broker", true, "", "ops", "not started by the person signed in"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := &startLog{}
			auditedImpersonationRequest(t, logs, tc.caller, tc.stop, tc.target, tc.heldBy)
			var lines []string
			for _, line := range strings.Split(logs.text(), "\n") {
				if strings.Contains(line, tc.line) {
					lines = append(lines, line)
				}
			}
			if len(lines) != 1 {
				t.Fatalf("want one line mentioning %q, got:\n%s", tc.line, logs.text())
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
