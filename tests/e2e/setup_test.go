package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"weos/api/handlers"
	apimw "weos/api/middleware"
	"weos/application"
	"weos/application/presets"
	"weos/domain/entities"
	"weos/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// testEnv holds the bootstrapped server and services for E2E tests.
type testEnv struct {
	server          *httptest.Server
	app             *fx.App
	authService     authapp.AuthenticationService
	resourceService application.ResourceService
	adminAgentID    string
	adminAccountID  string
	memberAgentID   string
	memberAccountID string
}

// setupTestEnv boots the full application with an in-memory SQLite database,
// seeds test users, installs the tasks preset, and starts an httptest server.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	cfg := config.Default()
	// Use a temp file SQLite DB per test for proper WAL mode support.
	// In-memory shared cache has concurrency issues with event dispatch.
	tmpDir := t.TempDir()
	cfg.DatabaseDSN = filepath.Join(tmpDir, "test.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	cfg.LogLevel = "error"

	var resourceTypeService application.ResourceTypeService
	var resourceService application.ResourceService
	var resourcePermService application.ResourcePermissionService
	var authService authapp.AuthenticationService
	var credentialRepo authrepos.CredentialRepository
	var agentRepo authrepos.AgentRepository
	var accountRepo authrepos.AccountRepository
	var authzChecker *authcasbin.CasbinAuthorizationChecker
	var logger entities.Logger

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
		fx.Populate(&resourcePermService),
		fx.Populate(&authService),
		fx.Populate(&credentialRepo),
		fx.Populate(&agentRepo),
		fx.Populate(&accountRepo),
		fx.Populate(&authzChecker),
		fx.Populate(&logger),
	)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	})

	// Seed admin policies
	if err := application.SeedAdminPolicies(authzChecker); err != nil {
		t.Fatalf("failed to seed admin policies: %v", err)
	}

	ctx := context.Background()

	// Seed admin user
	adminAgent, _, adminAccount, err := authService.FindOrCreateAgent(ctx, authapp.UserInfo{
		ProviderUserID: "dev-admin-001",
		Email:          "admin@weos.dev",
		DisplayName:    "Admin User",
		Provider:       "dev",
	})
	if err != nil {
		t.Fatalf("failed to seed admin: %v", err)
	}
	adminAccountID := ""
	if adminAccount != nil {
		adminAccountID = adminAccount.GetID()
	}

	// Seed member user
	memberAgent, _, memberAccount, err := authService.FindOrCreateAgent(ctx, authapp.UserInfo{
		ProviderUserID: "dev-member-001",
		Email:          "member@weos.dev",
		DisplayName:    "Regular User",
		Provider:       "dev",
	})
	if err != nil {
		t.Fatalf("failed to seed member: %v", err)
	}
	memberAccountID := ""
	if memberAccount != nil {
		memberAccountID = memberAccount.GetID()
	}

	// Install tasks preset
	if _, err := resourceTypeService.InstallPreset(ctx, "tasks", true); err != nil {
		t.Fatalf("failed to install tasks preset: %v", err)
	}

	// Build Echo server with the same route layout as serve.go
	e := echo.New()
	e.HideBanner = true

	api := e.Group("/api")
	api.GET("/health", handlers.HealthHandler)

	protected := api.Group("")
	// OAuth is not enabled in tests, so use SoftAuth
	protected.Use(apimw.SoftAuth(credentialRepo, agentRepo, accountRepo, logger))

	rtHandler := handlers.NewResourceTypeHandler(resourceTypeService, authzChecker, accountRepo, logger)
	protected.POST("/resource-types", rtHandler.Create)
	protected.GET("/resource-types", rtHandler.List)
	protected.GET("/resource-types/:id", rtHandler.Get)

	permHandler := handlers.NewResourcePermissionHandler(resourcePermService)
	protected.POST("/:typeSlug/:id/permissions", permHandler.Grant)
	protected.GET("/:typeSlug/:id/permissions", permHandler.List)
	protected.DELETE("/:typeSlug/:id/permissions/:agentId", permHandler.Revoke)

	resourceHandler := handlers.NewResourceHandler(resourceService, resourceTypeService)
	protected.POST("/:typeSlug", resourceHandler.Create)
	protected.GET("/:typeSlug", resourceHandler.List)
	protected.GET("/:typeSlug/:id", resourceHandler.Get)
	protected.PUT("/:typeSlug/:id", resourceHandler.Update)
	protected.DELETE("/:typeSlug/:id", resourceHandler.Delete)

	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	return &testEnv{
		server:          server,
		app:             app,
		authService:     authService,
		resourceService: resourceService,
		adminAgentID:    adminAgent.GetID(),
		adminAccountID:  adminAccountID,
		memberAgentID:   memberAgent.GetID(),
		memberAccountID: memberAccountID,
	}
}

// request helpers

func (env *testEnv) doRequest(t *testing.T, method, path, body, devAgent string) *http.Response {
	t.Helper()
	url := env.server.URL + path
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if devAgent != "" {
		req.Header.Set("X-Dev-Agent", devAgent)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	return resp
}

func readJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nbody: %s", err, string(data))
	}
	return result
}

// seedProjectForUser creates a project via the API as the given dev agent and returns its ID.
func (env *testEnv) seedProjectForUser(t *testing.T, name, email string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"description":"test project","status":"active"}`, name)
	resp := env.doRequest(t, "POST", "/api/project", body, email)
	if resp.StatusCode != http.StatusCreated {
		result := readJSON(t, resp)
		t.Fatalf("create project: expected 201, got %d: %v", resp.StatusCode, result)
	}
	result := readJSON(t, resp)
	id, ok := result["id"].(string)
	if !ok || id == "" {
		t.Fatalf("create project: missing id in response: %v", result)
	}
	return id
}

// seedTaskForUser creates a task linked to a project via the API.
func (env *testEnv) seedTaskForUser(t *testing.T, name, projectID, email string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"status":"open","priority":"medium","project":%q}`, name, projectID)
	resp := env.doRequest(t, "POST", "/api/task", body, email)
	if resp.StatusCode != http.StatusCreated {
		result := readJSON(t, resp)
		t.Fatalf("create task: expected 201, got %d: %v", resp.StatusCode, result)
	}
	result := readJSON(t, resp)
	id, ok := result["id"].(string)
	if !ok || id == "" {
		t.Fatalf("create task: missing id in response: %v", result)
	}
	return id
}
