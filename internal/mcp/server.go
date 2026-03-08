package mcp

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"weos/application"
	"weos/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

func Run() error {
	cfg := loadConfig()

	var websiteService application.WebsiteService
	var pageService application.PageService
	var sectionService application.SectionService
	var themeService application.ThemeService
	var templateService application.TemplateService
	var personService application.PersonService
	var organizationService application.OrganizationService

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg),
		fx.Populate(&websiteService),
		fx.Populate(&pageService),
		fx.Populate(&sectionService),
		fx.Populate(&themeService),
		fx.Populate(&templateService),
		fx.Populate(&personService),
		fx.Populate(&organizationService),
	)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()

	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "weos",
		Title:   "WeOS MCP Server",
		Version: "0.1.0",
	}, nil)

	registerWebsiteTools(server, websiteService)
	registerPageTools(server, pageService)
	registerSectionTools(server, sectionService)
	registerThemeTools(server, themeService)
	registerTemplateTools(server, templateService)
	registerPersonTools(server, personService)
	registerOrganizationTools(server, organizationService)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err := server.Run(ctx, &mcp.StdioTransport{})

	stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer stopCancel()

	_ = app.Stop(stopCtx)

	return err
}

func loadConfig() config.Config {
	cfg := config.Default()
	cfg.LoadFromEnvironment()
	return cfg
}
