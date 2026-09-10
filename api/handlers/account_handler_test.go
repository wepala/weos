package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

type stubErasure struct {
	result *application.ErasureResult
	err    error
	calls  []application.EraseAccountCommand
}

func (s *stubErasure) Erase(_ context.Context, cmd application.EraseAccountCommand) (*application.ErasureResult, error) {
	s.calls = append(s.calls, cmd)
	return s.result, s.err
}

type stubAccounts struct {
	authrepos.AccountRepository
	accounts map[string]*authentities.Account
	roles    map[string]string
}

func (s stubAccounts) FindByID(_ context.Context, id string) (*authentities.Account, error) {
	return s.accounts[id], nil
}

func (s stubAccounts) FindMemberRole(_ context.Context, accountID, agentID string) (string, error) {
	return s.roles[agentID+"|"+accountID], nil
}

type destroyingSessions struct {
	session.SessionManager
	destroyed int
}

func (d *destroyingSessions) DestroyHTTPSession(w http.ResponseWriter, _ *http.Request) error {
	d.destroyed++
	http.SetCookie(w, &http.Cookie{Name: "weos-session", Value: "", MaxAge: -1, Path: "/"})
	return nil
}

func harborAccount(t *testing.T) *authentities.Account {
	t.Helper()
	a, err := (&authentities.Account{}).With("acct-harbor", "Harbor Legal", authentities.AccountTypePersonal)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type deleteFixture struct {
	erasure  *stubErasure
	accounts stubAccounts
	sessions *destroyingSessions
	store    sessions.Store
	handler  *handlers.AccountHandler
}

func newDeleteFixture(t *testing.T) *deleteFixture {
	t.Helper()
	f := &deleteFixture{
		erasure: &stubErasure{result: &application.ErasureResult{AccountID: "acct-harbor", MembersLost: 2}},
		accounts: stubAccounts{
			accounts: map[string]*authentities.Account{"acct-harbor": harborAccount(t)},
			roles:    map[string]string{"ops|acct-harbor": authentities.RoleOwner, "counsel|acct-harbor": "member"},
		},
		sessions: &destroyingSessions{},
		store:    sessions.NewCookieStore([]byte("test-secret")),
	}
	f.handler = handlers.NewAccountHandler(handlers.AccountHandlerConfig{
		Erasure:        f.erasure,
		Accounts:       f.accounts,
		SessionManager: f.sessions,
		Store:          f.store,
		Logger:         nopLogger{},
	})
	return f
}

// deleteAs sends DELETE /api/account as agentID acting in accountID, with the
// given body and any extra cookies.
func (f *deleteFixture) deleteAs(agentID, accountID, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	e := echo.New()
	identity := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if agentID != "" {
				ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{
					AgentID: agentID, AccountIDs: []string{accountID}, ActiveAccountID: accountID,
				})
				c.SetRequest(c.Request().WithContext(ctx))
			}
			return next(c)
		}
	}
	e.DELETE("/api/account", f.handler.Delete, identity)
	req := httptest.NewRequest(http.MethodDelete, "/api/account", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// The @unit-pinned scenario: a caller the instance still authenticates whose
// account is gone is told it is not found, and nothing is erased.
func TestAccountDelete_NotFoundForACallerWhoseAccountIsGone(t *testing.T) {
	f := newDeleteFixture(t)
	delete(f.accounts.accounts, "acct-harbor")
	rec := f.deleteAs("ops", "acct-harbor", `{"confirm":"DELETE"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d %s, want 404", rec.Code, rec.Body.String())
	}
	if len(f.erasure.calls) != 0 {
		t.Error("the erasure ran for an account that does not exist")
	}
}

func TestAccountDelete_NotFoundWhenTheServiceFindsTheAccountGone(t *testing.T) {
	f := newDeleteFixture(t)
	f.erasure.err = application.ErrAccountNotFound
	rec := f.deleteAs("ops", "acct-harbor", `{"confirm":"DELETE"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d %s, want 404", rec.Code, rec.Body.String())
	}
}

func TestAccountDelete_RefusesEveryBodyThatDoesNotConfirm(t *testing.T) {
	for _, body := range []string{
		`{"confirm":"delete"}`, `{"confirm":"DELETE "}`, `{"confirmation":"DELETE"}`, `{"confirm":""}`, `{}`, ``, `not json`,
		// wm-q7knw: the field name in another case, an extra field, and a
		// second document after the first.
		`{"Confirm":"DELETE"}`, `{"CONFIRM":"DELETE"}`, `{"confirm":"DELETE","extra":true}`, `{"confirm":"DELETE"} {}`,
		`{"confirm":["DELETE"]}`, `"DELETE"`,
		// PR 561 Copilot review: json.Decoder.More reports false when the next
		// byte is a closing bracket, so a stray one after the document must be
		// refused by something other than More.
		`{"confirm":"DELETE"}}`, `{"confirm":"DELETE"}]`, `{"confirm":"DELETE"} ]`, `{"confirm":"DELETE"}"x"`,
	} {
		t.Run(body, func(t *testing.T) {
			f := newDeleteFixture(t)
			rec := f.deleteAs("ops", "acct-harbor", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("body %q got %d %s, want 400", body, rec.Code, rec.Body.String())
			}
			if len(f.erasure.calls) != 0 {
				t.Errorf("body %q reached the erasure service", body)
			}
		})
	}
}

// wm-q7knw: the confirmation is JSON, and is refused under any other type.
func TestAccountDelete_RefusesTheConfirmationUnderAnotherContentType(t *testing.T) {
	for _, contentType := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		t.Run(contentType, func(t *testing.T) {
			f := newDeleteFixture(t)
			e := echo.New()
			e.DELETE("/api/account", f.handler.Delete, func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{
						AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor",
					})
					c.SetRequest(c.Request().WithContext(ctx))
					return next(c)
				}
			})
			req := httptest.NewRequest(http.MethodDelete, "/api/account", strings.NewReader(`{"confirm":"DELETE"}`))
			if contentType != "" {
				req.Header.Set("Content-Type", contentType)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("content type %q got %d %s, want 400", contentType, rec.Code, rec.Body.String())
			}
			if len(f.erasure.calls) != 0 {
				t.Errorf("content type %q reached the erasure service", contentType)
			}
		})
	}
	f := newDeleteFixture(t)
	e := echo.New()
	e.DELETE("/api/account", f.handler.Delete, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{
				AgentID: "ops", AccountIDs: []string{"acct-harbor"}, ActiveAccountID: "acct-harbor",
			})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/account", strings.NewReader(`{"confirm":"DELETE"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("application/json with a charset got %d %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestAccountDelete_RefusesAnOrdinaryMember(t *testing.T) {
	f := newDeleteFixture(t)
	rec := f.deleteAs("counsel", "acct-harbor", `{"confirm":"DELETE"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d %s, want 403", rec.Code, rec.Body.String())
	}
	if len(f.erasure.calls) != 0 {
		t.Error("a member's request reached the erasure service")
	}
}

func TestAccountDelete_RefusesWithoutAnIdentity(t *testing.T) {
	f := newDeleteFixture(t)
	rec := f.deleteAs("", "", `{"confirm":"DELETE"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d %s, want 401", rec.Code, rec.Body.String())
	}
}

func TestAccountDelete_RefusesWhileImpersonating(t *testing.T) {
	f := newDeleteFixture(t)
	// Stage the impersonation cookie the way the Start handler writes it.
	req := httptest.NewRequest(http.MethodPost, "/api/admin/impersonate", nil)
	rec := httptest.NewRecorder()
	sess, _ := f.store.Get(req, apimw.ImpersonationSessionName)
	sess.Values[apimw.KeyImpersonatedAgentID] = "counsel"
	sess.Values[apimw.KeyRealAgentID] = "ops"
	if err := sess.Save(req, rec); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no impersonation cookie was written")
	}

	res := f.deleteAs("ops", "acct-harbor", `{"confirm":"DELETE"}`, cookies...)
	if res.Code != http.StatusForbidden {
		t.Fatalf("got %d %s, want 403", res.Code, res.Body.String())
	}
	if len(f.erasure.calls) != 0 {
		t.Error("an impersonating administrator's request reached the erasure service")
	}
}

func TestAccountDelete_ErasesReportsWhoLostItAndSignsOut(t *testing.T) {
	f := newDeleteFixture(t)
	rec := f.deleteAs("ops", "acct-harbor", `{"confirm":"DELETE"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
	}
	if len(f.erasure.calls) != 1 || f.erasure.calls[0].AccountID != "acct-harbor" || f.erasure.calls[0].RequestedBy != "ops" {
		t.Fatalf("erasure calls = %+v, want one for acct-harbor by ops", f.erasure.calls)
	}
	var body struct {
		Data handlers.AccountDeletedResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("answer is not the envelope: %s", rec.Body.String())
	}
	if body.Data.MembersLost != 2 || body.Data.AccountID != "acct-harbor" {
		t.Errorf("answer = %+v, want 2 members lost of acct-harbor", body.Data)
	}
	if f.sessions.destroyed != 1 {
		t.Error("the session cookie was not cleared")
	}
	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	if !cleared["pericarp_token"] || !cleared["weos-session"] {
		t.Errorf("cookies cleared = %v, want both the token cookie and the session cookie", cleared)
	}
}

func TestAccountDelete_ARunAlreadyInProgressAnswersConflict(t *testing.T) {
	f := newDeleteFixture(t)
	f.erasure.result = nil
	f.erasure.err = application.ErrErasureInProgress
	rec := f.deleteAs("ops", "acct-harbor", `{"confirm":"DELETE"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d %s, want 409", rec.Code, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != handlers.CodeAccountErasureInProgress {
		t.Errorf("code = %q, want %s", body.Code, handlers.CodeAccountErasureInProgress)
	}
	if f.sessions.destroyed != 0 {
		t.Error("the session was cleared while the first run was still going")
	}
}

func TestAccountDelete_UnfinishedDeletionAnswersWithTheCode(t *testing.T) {
	f := newDeleteFixture(t)
	f.erasure.result = nil
	f.erasure.err = errors.New("bucket refused")
	rec := f.deleteAs("ops", "acct-harbor", `{"confirm":"DELETE"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d %s, want 500", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != handlers.CodeAccountErasureUnfinished || !strings.Contains(body.Error, "run again") {
		t.Errorf("answer = %+v, want code %s and a message saying it can be run again", body, handlers.CodeAccountErasureUnfinished)
	}
	if f.sessions.destroyed != 0 {
		t.Error("the session was cleared even though the deletion did not finish; the re-run needs it")
	}
}

// --- export -----------------------------------------------------------------

type stubResources struct {
	repositories.ResourceRepository
	rows       []*entities.Resource
	gotFilters []repositories.FilterCondition
	gotSlug    string
}

func (s *stubResources) FindAllByTypeWithFilters(
	_ context.Context, typeSlug string, filters []repositories.FilterCondition,
	_ string, _ int, _ repositories.SortOptions, _ *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.gotSlug = typeSlug
	s.gotFilters = filters
	var out []*entities.Resource
	for _, r := range s.rows {
		for _, f := range filters {
			if f.Field == "accountId" && r.AccountID() == f.Value {
				out = append(out, r)
			}
		}
	}
	return repositories.PaginatedResponse[*entities.Resource]{Data: out}, nil
}

type stubTypes struct {
	application.ResourceTypeService
	recipe *entities.ResourceType
}

func (s stubTypes) GetBySlug(_ context.Context, slug string) (*entities.ResourceType, error) {
	if slug != "recipe" || s.recipe == nil {
		return nil, repositories.ErrNotFound
	}
	return s.recipe, nil
}

func recipeOf(t *testing.T, id, account, name string) *entities.Resource {
	t.Helper()
	r := &entities.Resource{}
	data := json.RawMessage(`{"@context":{"@vocab":"https://schema.org/"},"@graph":[{"@id":"` + id + `","@type":"Recipe","name":"` + name + `"}]}`)
	if err := r.Restore(id, "recipe", "active", data, "ops", account, time.Now(), 1); err != nil {
		t.Fatal(err)
	}
	return r
}

func exportAs(t *testing.T, h *handlers.AccountHandler, accountID string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.GET("/api/account/export", h.Export, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if accountID != "" {
				ctx := auth.ContextWithAgent(c.Request().Context(), &auth.Identity{AgentID: "ops", AccountIDs: []string{accountID}, ActiveAccountID: accountID})
				c.SetRequest(c.Request().WithContext(ctx))
			}
			return next(c)
		}
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/account/export", nil))
	return rec
}

func TestAccountExport_HoldsTheAccountsRecipesOnlyAndSaysSo(t *testing.T) {
	rt := &entities.ResourceType{}
	if err := rt.Restore("urn:type:recipe", "Recipe", "recipe", "", "active", json.RawMessage(`{"@vocab":"https://schema.org/"}`), nil, time.Now(), 1); err != nil {
		t.Fatal(err)
	}
	resources := &stubResources{rows: []*entities.Resource{
		recipeOf(t, "urn:recipe:1", "acct-harbor", "Sunday Lasagna"),
		recipeOf(t, "urn:recipe:2", "acct-harbor", "Weeknight Dal"),
		recipeOf(t, "urn:recipe:3", "acct-cedar", "Cedar Soup"),
	}}
	h := handlers.NewAccountHandler(handlers.AccountHandlerConfig{
		Resources: resources, ResourceTypes: stubTypes{recipe: rt}, Logger: nopLogger{},
	})

	rec := exportAs(t, h, "acct-harbor")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/ld+json") {
		t.Errorf("Content-Type = %q, want application/ld+json", ct)
	}
	if resources.gotSlug != "recipe" || len(resources.gotFilters) != 1 || resources.gotFilters[0].Field != "accountId" || resources.gotFilters[0].Value != "acct-harbor" {
		t.Errorf("the listing was not filtered on the account explicitly: slug %q filters %+v", resources.gotSlug, resources.gotFilters)
	}
	var doc struct {
		Context map[string]any   `json:"@context"`
		Type    string           `json:"@type"`
		Graph   []map[string]any `json:"@graph"`
		Scope   struct {
			Includes []string `json:"includes"`
			Note     string   `json:"note"`
		} `json:"weos:exportScope"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("the export is not a JSON-LD document: %v\n%s", err, rec.Body.String())
	}
	if doc.Context["@vocab"] != "https://schema.org/" || doc.Context["weos"] == nil {
		t.Errorf("@context = %v, want the type's context plus the weos term", doc.Context)
	}
	names := []string{}
	for _, node := range doc.Graph {
		names = append(names, node["name"].(string))
	}
	if strings.Join(names, ",") != "Sunday Lasagna,Weeknight Dal" {
		t.Errorf("@graph names = %v, want the two Harbor Legal recipes and not Cedar Realty's", names)
	}
	if len(doc.Scope.Includes) != 1 || doc.Scope.Includes[0] != "recipe" || !strings.Contains(doc.Scope.Note, "nothing else") {
		t.Errorf("the document does not describe its recipes-only scope: %+v", doc.Scope)
	}
}

func TestAccountExport_EmptyWhenTheTypeIsNotInstalled(t *testing.T) {
	h := handlers.NewAccountHandler(handlers.AccountHandlerConfig{
		Resources: &stubResources{}, ResourceTypes: stubTypes{}, Logger: nopLogger{},
	})
	rec := exportAs(t, h, "acct-harbor")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
	}
	var doc struct {
		Graph []any          `json:"@graph"`
		Scope map[string]any `json:"weos:exportScope"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || doc.Graph == nil || len(doc.Graph) != 0 || doc.Scope == nil {
		t.Fatalf("export with no recipe type = %s, want an empty graph that still names its scope", rec.Body.String())
	}
}

func TestAccountExport_RefusesWithoutAnIdentity(t *testing.T) {
	h := handlers.NewAccountHandler(handlers.AccountHandlerConfig{Resources: &stubResources{}, ResourceTypes: stubTypes{}, Logger: nopLogger{}})
	if rec := exportAs(t, h, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

// --- signing in to a locked account ------------------------------------------

type membershipAccounts struct {
	stubAccounts
	memberships []*authentities.Account
}

func (m membershipAccounts) FindByMember(context.Context, string) ([]*authentities.Account, error) {
	return m.memberships, nil
}

type lockedSet map[string]bool

func (l lockedSet) Lock(_ context.Context, id, _ string) error { l[id] = true; return nil }
func (l lockedSet) IsLocked(_ context.Context, id string) (bool, error) {
	return l[id], nil
}

func loginLocked(t *testing.T, role string, locked bool) (*httptest.ResponseRecorder, *fakeAuthService) {
	t.Helper()
	harbor := newAccount(t, "acct-harbor", "Harbor Legal")
	if err := harbor.Deactivate(); err != nil {
		t.Fatal(err)
	}
	authSvc := &fakeAuthService{
		verifyAgent:   newAgent(t, "agent-1", "alice"),
		verifyCred:    newCredential(t),
		verifyAccount: nil, // sign-in resolution passes over an inactive account
		sessionResult: newAuthSession(t),
		tokenString:   "should-not-be-issued",
	}
	locks := lockedSet{}
	if locked {
		locks["acct-harbor"] = true
	}
	h := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    authSvc,
		SessionManager: &fakeSessionManager{},
		Logger:         nopLogger{},
		AccountRepo: membershipAccounts{
			stubAccounts: stubAccounts{roles: map[string]string{"agent-1|acct-harbor": role}},
			memberships:  []*authentities.Account{harbor},
		},
		ErasureLocks: locks,
	})
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(newJSONRequest(http.MethodPost, "/api/auth/password-login",
		`{"email":"alice@example.com","password":"pw"}`), rec)
	if err := h.Login(c); err != nil {
		t.Fatalf("Login: %v", err)
	}
	return rec, authSvc
}

func TestPasswordAuthHandler_Login_LockedAccountSignsInForTheDeletionOnly(t *testing.T) {
	rec, authSvc := loginLocked(t, authentities.RoleOwner, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if authSvc.gotSessionAccountID != "acct-harbor" || !authSvc.gotSessionVouched {
		t.Errorf("session scoped to %q vouched=%v, want the locked account, vouched for", authSvc.gotSessionAccountID, authSvc.gotSessionVouched)
	}
	var body struct {
		Data struct {
			Account        *struct{ ID string } `json:"account"`
			ErasurePending bool                 `json:"erasure_pending"`
			Code           string               `json:"code"`
			Token          string               `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Account == nil || body.Data.Account.ID != "acct-harbor" || !body.Data.ErasurePending || body.Data.Code != apimw.CodeAccountErasurePending {
		t.Errorf("answer = %+v, want the locked account named with erasure_pending and its code", body.Data)
	}
	if body.Data.Token != "" || authSvc.gotTokenAccountID != "" {
		t.Error("a token was issued for a locked account")
	}
}

func TestPasswordAuthHandler_Login_SuspendedOrMemberStaysUnscoped(t *testing.T) {
	for name, tc := range map[string]struct {
		role   string
		locked bool
	}{
		"suspended, not locked":              {authentities.RoleOwner, false},
		"a plain member of a locked account": {"member", true},
	} {
		t.Run(name, func(t *testing.T) {
			rec, authSvc := loginLocked(t, tc.role, tc.locked)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			if authSvc.gotSessionAccountID != "" {
				t.Errorf("session scoped to %q, want no account", authSvc.gotSessionAccountID)
			}
			if strings.Contains(rec.Body.String(), "erasure_pending") {
				t.Errorf("the sign-in offered the deletion: %s", rec.Body.String())
			}
		})
	}
}
