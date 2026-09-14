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
	h := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store: sessions.NewCookieStore([]byte("test-secret")),
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
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
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
