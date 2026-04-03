package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"weos/application"
	"weos/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Seed database with test users, presets, and sample data",
	Long: `Creates dev test users (admin and member), installs the tasks preset,
and creates sample projects and tasks. Idempotent — safe to run multiple times.`,
	RunE: runSeed,
}

func init() {
	rootCmd.AddCommand(seedCmd)
}

func runSeed(cmd *cobra.Command, _ []string) error {
	appCfg := GetConfig().Config

	var authService authapp.AuthenticationService
	var accountRepo authrepos.AccountRepository
	var resourceTypeService application.ResourceTypeService
	var resourceService application.ResourceService
	var authzChecker *authcasbin.CasbinAuthorizationChecker
	var logger entities.Logger

	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg),
		fx.Populate(&authService),
		fx.Populate(&accountRepo),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
		fx.Populate(&authzChecker),
		fx.Populate(&logger),
	)

	startCtx, startCancel := context.WithTimeout(cmd.Context(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	ctx := cmd.Context()
	manifest := seedManifest{
		Users:     make(map[string]seedManifestUser),
		Resources: make(map[string][]string),
	}

	// --- Seed users ---
	fmt.Fprintln(os.Stdout, "Seeding users...")
	var adminAgentID, adminAccountID string
	for i, def := range seedUsers {
		agent, _, account, err := authService.FindOrCreateAgent(ctx, userInfoFromDef(def))
		if err != nil {
			return fmt.Errorf("failed to seed user %q: %w", def.Email, err)
		}

		accountID := ""
		if account != nil {
			accountID = account.GetID()
		}

		// First user is admin/owner, second is member
		key := "member"
		if i == 0 {
			key = "admin"
			adminAgentID = agent.GetID()
			adminAccountID = accountID
		}

		manifest.Users[key] = seedManifestUser{
			AgentID:   agent.GetID(),
			AccountID: accountID,
			Email:     def.Email,
		}
		fmt.Fprintf(os.Stdout, "  %s: %s (agent=%s, account=%s)\n",
			key, def.Email, agent.GetID(), accountID)
	}

	// Seed Casbin admin policies
	if err := application.SeedAdminPolicies(authzChecker); err != nil {
		logger.Warn(ctx, "failed to seed admin policies", "error", err)
	}

	// --- Install presets ---
	fmt.Fprintln(os.Stdout, "Installing tasks preset...")
	result, err := resourceTypeService.InstallPreset(ctx, "tasks", true)
	if err != nil {
		return fmt.Errorf("failed to install tasks preset: %w", err)
	}
	manifest.Presets = []string{"tasks"}
	fmt.Fprintf(os.Stdout, "  created=%v updated=%v skipped=%v\n",
		result.Created, result.Updated, result.Skipped)

	// --- Seed sample data ---
	fmt.Fprintln(os.Stdout, "Creating sample data...")
	adminCtx := auth.ContextWithAgent(ctx, &auth.Identity{
		AgentID:         adminAgentID,
		AccountIDs:      []string{adminAccountID},
		ActiveAccountID: adminAccountID,
	})

	projectIDs, taskIDs, err := seedSampleData(adminCtx, resourceService)
	if err != nil {
		return fmt.Errorf("failed to seed sample data: %w", err)
	}
	manifest.Resources["projects"] = projectIDs
	manifest.Resources["tasks"] = taskIDs
	fmt.Fprintf(os.Stdout, "  %d projects, %d tasks\n", len(projectIDs), len(taskIDs))

	// --- Write manifest ---
	if err := writeSeedManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Seed manifest written to %s\n", seedManifestPath)
	fmt.Fprintln(os.Stdout, "Done.")
	return nil
}

func seedSampleData(
	ctx context.Context, svc application.ResourceService,
) (projectIDs, taskIDs []string, err error) {
	projects := []map[string]any{
		{"name": "WeOS Development", "description": "Core platform development", "status": "active"},
		{"name": "Marketing Site", "description": "Public marketing website", "status": "active"},
	}

	for _, p := range projects {
		data, _ := json.Marshal(p)
		entity, createErr := svc.Create(ctx, application.CreateResourceCommand{
			TypeSlug: "project",
			Data:     data,
		})
		if createErr != nil {
			return nil, nil, fmt.Errorf("failed to create project %q: %w", p["name"], createErr)
		}
		projectIDs = append(projectIDs, entity.GetID())
	}

	tasks := []struct {
		data       map[string]any
		projectIdx int
	}{
		{map[string]any{
			"name": "Set up CI pipeline", "description": "Configure GitHub Actions",
			"status": "done", "priority": "high", "dueDate": "2026-04-10",
		}, 0},
		{map[string]any{
			"name": "Add resource permissions API", "description": "Instance-level access control",
			"status": "in-progress", "priority": "high", "dueDate": "2026-04-15",
		}, 0},
		{map[string]any{
			"name": "Design landing page", "description": "Hero section and feature grid",
			"status": "open", "priority": "medium", "dueDate": "2026-04-20",
		}, 1},
		{map[string]any{
			"name": "Write blog post", "description": "Announce v3 release",
			"status": "open", "priority": "low", "dueDate": "2026-05-01",
		}, 1},
	}

	for _, t := range tasks {
		t.data["project"] = projectIDs[t.projectIdx]
		data, _ := json.Marshal(t.data)
		entity, createErr := svc.Create(ctx, application.CreateResourceCommand{
			TypeSlug: "task",
			Data:     data,
		})
		if createErr != nil {
			return nil, nil, fmt.Errorf("failed to create task %q: %w", t.data["name"], createErr)
		}
		taskIDs = append(taskIDs, entity.GetID())
	}

	return projectIDs, taskIDs, nil
}
