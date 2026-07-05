package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// TestNotificationInbox runs the notification-inbox acceptance scenarios
// (ticket #427) end to end: a service produces a notification through the real
// production entry point, and the recipient drives the real inbox HTTP handler
// (list, unread count, mark-read) over an in-process Echo instance with the
// same SoftAuth user scoping serve.go uses.
func TestNotificationInbox(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "notification-inbox",
		ScenarioInitializer: initNotificationInboxScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/notification_inbox.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("notification inbox acceptance scenarios failed")
	}
}

// seededUser is a dev user resolvable by SoftAuth via its email header.
type seededUser struct {
	email     string
	agentID   string
	accountID string
}

// notificationWorld boots the real application and drives the real notification
// production entry point and inbox HTTP handler.
type notificationWorld struct {
	app    *fx.App
	tmpDir string
	e      *echo.Echo

	rts   application.ResourceTypeService
	notes application.NotificationService
	rs    application.ResourceService // to assert generic-route (GetByID) access denial

	authService authapp.AuthenticationService
	credRepo    authrepos.CredentialRepository
	agentRepo   authrepos.AgentRepository
	accountRepo authrepos.AccountRepository
	logger      entities.Logger

	users      map[string]seededUser
	producedID map[string]string // notification title -> id, for cross-user reference
	occSeq     int

	lastStatus int
	lastBody   string
}

func initNotificationInboxScenario(sc *godog.ScenarioContext) {
	w := &notificationWorld{}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*w = notificationWorld{}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a running WeOS application with the notification capability$`, w.aRunningApplication)
	sc.Step(`^a user "([^"]*)"$`, w.aUser)
	sc.Step(`^the service notifies "([^"]*)" with title "([^"]*)" and key "([^"]*)"$`, w.serviceNotifies)
	sc.Step(`^"([^"]*)" has (\d+) notifications? in her inbox$`, w.userHasInboxCount)
	sc.Step(`^the newest notification in "([^"]*)" inbox is titled "([^"]*)"$`, w.newestInboxTitled)
	sc.Step(`^"([^"]*)" has an unread count of (\d+)$`, w.userHasUnreadCount)
	sc.Step(`^"([^"]*)" marks the notification titled "([^"]*)" as read$`, w.userMarksTitledRead)
	sc.Step(`^the notification titled "([^"]*)" in "([^"]*)" inbox is read$`, w.titledNotificationIsRead)
	sc.Step(`^"([^"]*)" marks all notifications read$`, w.userMarksAllRead)
	sc.Step(`^"([^"]*)" is an account admin of "([^"]*)"$`, w.isAccountAdminOf)
	sc.Step(`^"([^"]*)" cannot read the notification titled "([^"]*)"$`, w.cannotReadTitled)
	sc.Step(`^"([^"]*)" is denied marking the notification titled "([^"]*)" as read$`, w.deniedMarkingTitled)
}

// --- Boot & seeding ---

func (w *notificationWorld) aRunningApplication() error {
	cfg := config.Default()
	dir, err := os.MkdirTemp("", "weos-notify-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	cfg.DatabaseDSN = filepath.Join(dir, "test.db")
	cfg.LogLevel = "error"

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&w.rts, &w.notes, &w.rs),
		fx.Populate(&w.authService, &w.credRepo, &w.agentRepo, &w.accountRepo, &w.logger),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	w.users = map[string]seededUser{}
	w.producedID = map[string]string{}

	// The notifications preset is opt-in (only core auto-installs); a consuming
	// service enables the inbox by installing it, which is what we do here.
	if _, err := w.rts.InstallPreset(context.Background(), "notifications", true); err != nil {
		return fmt.Errorf("install notifications preset: %w", err)
	}

	// The same inbox routes serve.go registers, behind SoftAuth so the
	// X-Dev-Agent header selects which seeded user is the caller.
	w.e = echo.New()
	w.e.HideBanner = true
	api := w.e.Group("/api")
	api.Use(apimw.Messages())
	protected := api.Group("")
	protected.Use(apimw.SoftAuth(w.credRepo, w.agentRepo, w.accountRepo, w.logger))

	h := handlers.NewNotificationHandler(w.notes, w.logger)
	protected.GET("/notifications", h.List)
	protected.GET("/notifications/unread-count", h.UnreadCount)
	protected.POST("/notifications/mark-all-read", h.MarkAllRead)
	protected.POST("/notifications/:id/read", h.MarkRead)
	return nil
}

func (w *notificationWorld) aUser(name string) error {
	email := strings.ToLower(name) + "@weos.dev"
	agent, _, account, err := w.authService.FindOrCreateAgent(context.Background(), authapp.UserInfo{
		ProviderUserID: "dev-" + strings.ToLower(name),
		Email:          email,
		DisplayName:    name,
		Provider:       "dev",
	})
	if err != nil {
		return fmt.Errorf("seed user %q: %w", name, err)
	}
	accountID := ""
	if account != nil {
		accountID = account.GetID()
	}
	w.users[name] = seededUser{email: email, agentID: agent.GetID(), accountID: accountID}
	return nil
}

func (w *notificationWorld) user(name string) (seededUser, error) {
	u, ok := w.users[name]
	if !ok {
		return seededUser{}, fmt.Errorf("unknown user %q — declare it with a 'Given a user' step", name)
	}
	return u, nil
}

// nextOccurredAt hands out strictly increasing timestamps so newest-first
// ordering is deterministic regardless of wall-clock resolution.
func (w *notificationWorld) nextOccurredAt() time.Time {
	w.occSeq++
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(w.occSeq) * time.Minute)
}

// --- Actions ---

func (w *notificationWorld) serviceNotifies(name, title, key string) error {
	u, err := w.user(name)
	if err != nil {
		return err
	}
	res, err := w.notes.Notify(context.Background(), application.NotificationInput{
		Recipient:  u.agentID,
		Kind:       "test.signal",
		Title:      title,
		Body:       "body of " + title,
		DedupeKey:  key,
		OccurredAt: w.nextOccurredAt(),
	})
	if err != nil {
		return fmt.Errorf("notify %q: %w", name, err)
	}
	w.producedID[title] = res.GetID()
	return nil
}

// isAccountAdminOf makes adminName an admin member of targetName's account,
// which activates the ResourceService admin/owner access bypass — the exact
// path the mark endpoint must still refuse for a notification the admin does
// not own.
func (w *notificationWorld) isAccountAdminOf(adminName, targetName string) error {
	admin, err := w.user(adminName)
	if err != nil {
		return err
	}
	target, err := w.user(targetName)
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(context.Background(), target.accountID, admin.agentID, "admin"); err != nil {
		return fmt.Errorf("make %q admin of %q's account: %w", adminName, targetName, err)
	}
	return nil
}

// cannotReadTitled asserts the named user cannot read another member's
// notification content. It goes through ResourceService.GetByID — the same
// access check the generic resource routes (GET /:typeSlug/:id) use — with the
// user's identity, and requires ErrAccessDenied. This is the read half of the
// hole: without account-less creation an account admin would get the body back.
func (w *notificationWorld) cannotReadTitled(name, title string) error {
	u, err := w.user(name)
	if err != nil {
		return err
	}
	id, ok := w.producedID[title]
	if !ok {
		return fmt.Errorf("no notification titled %q was produced", title)
	}
	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{AgentID: u.agentID})
	if _, err := w.rs.GetByID(ctx, id); !errors.Is(err, entities.ErrAccessDenied) {
		return fmt.Errorf("reading another member's notification: expected access denied, got: %v", err)
	}
	return nil
}

// deniedMarkingTitled has the named user attempt to mark a notification they do
// not own (referenced by title, not via their own inbox) and asserts a 403.
func (w *notificationWorld) deniedMarkingTitled(name, title string) error {
	id, ok := w.producedID[title]
	if !ok {
		return fmt.Errorf("no notification titled %q was produced", title)
	}
	if err := w.do(name, http.MethodPost, "/api/notifications/"+id+"/read", ""); err != nil {
		return err
	}
	if w.lastStatus != http.StatusForbidden {
		return fmt.Errorf("marking another user's notification: expected 403, got %d: %s", w.lastStatus, w.lastBody)
	}
	return nil
}

// do drives the in-process Echo handler as the named user.
func (w *notificationWorld) do(name, method, path, body string) error {
	u, err := w.user(name)
	if err != nil {
		return err
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	req.Header.Set("X-Dev-Agent", u.email)
	rec := httptest.NewRecorder()
	w.e.ServeHTTP(rec, req)
	w.lastStatus = rec.Code
	w.lastBody = rec.Body.String()
	return nil
}

// inbox lists the named user's notifications newest-first.
func (w *notificationWorld) inbox(name string) ([]map[string]any, error) {
	if err := w.do(name, http.MethodGet, "/api/notifications", ""); err != nil {
		return nil, err
	}
	if w.lastStatus != http.StatusOK {
		return nil, fmt.Errorf("list inbox for %q: expected 200, got %d: %s", name, w.lastStatus, w.lastBody)
	}
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(w.lastBody), &env); err != nil {
		return nil, fmt.Errorf("inbox response is not an envelope: %s", w.lastBody)
	}
	return env.Data, nil
}

func (w *notificationWorld) findByTitle(name, title string) (map[string]any, error) {
	items, err := w.inbox(name)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if fmt.Sprint(it["title"]) == title {
			return it, nil
		}
	}
	return nil, fmt.Errorf("no notification titled %q in %q inbox (%d items)", title, name, len(items))
}

func (w *notificationWorld) userMarksTitledRead(name, title string) error {
	item, err := w.findByTitle(name, title)
	if err != nil {
		return err
	}
	id := fmt.Sprint(item["id"])
	if err := w.do(name, http.MethodPost, "/api/notifications/"+id+"/read", ""); err != nil {
		return err
	}
	if w.lastStatus != http.StatusOK {
		return fmt.Errorf("mark read: expected 200, got %d: %s", w.lastStatus, w.lastBody)
	}
	return nil
}

func (w *notificationWorld) userMarksAllRead(name string) error {
	if err := w.do(name, http.MethodPost, "/api/notifications/mark-all-read", ""); err != nil {
		return err
	}
	if w.lastStatus != http.StatusOK {
		return fmt.Errorf("mark all read: expected 200, got %d: %s", w.lastStatus, w.lastBody)
	}
	return nil
}

// --- Outcomes ---

func (w *notificationWorld) userHasInboxCount(name string, want int) error {
	items, err := w.inbox(name)
	if err != nil {
		return err
	}
	if len(items) != want {
		return fmt.Errorf("%q inbox has %d notifications, want %d", name, len(items), want)
	}
	return nil
}

func (w *notificationWorld) newestInboxTitled(name, title string) error {
	items, err := w.inbox(name)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("%q inbox is empty, expected newest titled %q", name, title)
	}
	if got := fmt.Sprint(items[0]["title"]); got != title {
		return fmt.Errorf("newest notification in %q inbox is %q, want %q", name, got, title)
	}
	return nil
}

func (w *notificationWorld) userHasUnreadCount(name string, want int) error {
	if err := w.do(name, http.MethodGet, "/api/notifications/unread-count", ""); err != nil {
		return err
	}
	if w.lastStatus != http.StatusOK {
		return fmt.Errorf("unread count for %q: expected 200, got %d: %s", name, w.lastStatus, w.lastBody)
	}
	var env struct {
		Data struct {
			Unread int `json:"unread"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(w.lastBody), &env); err != nil {
		return fmt.Errorf("unread-count response is not an envelope: %s", w.lastBody)
	}
	if env.Data.Unread != want {
		return fmt.Errorf("%q unread count is %d, want %d", name, env.Data.Unread, want)
	}
	return nil
}

func (w *notificationWorld) titledNotificationIsRead(title, name string) error {
	item, err := w.findByTitle(name, title)
	if err != nil {
		return err
	}
	read, _ := item["read"].(bool)
	if !read {
		return fmt.Errorf("notification titled %q in %q inbox is not read: %v", title, name, item["read"])
	}
	return nil
}

func (w *notificationWorld) teardown() {
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(stopCtx)
	}
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
	}
}
