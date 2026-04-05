package mcp

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	"weos/application"
	"weos/application/presets"
	"weos/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

// DeletedOutput is the standard MCP output for delete operations.
type DeletedOutput struct {
	Success bool `json:"success"`
}

// ServiceName identifies an MCP tool group.
type ServiceName string

const (
	ServicePerson       ServiceName = "person"
	ServiceOrganization ServiceName = "organization"
	ServiceResourceType ServiceName = "resource-type"
	ServiceResource     ServiceName = "resource"
)

// AllServices is the ordered list of every available service.
var AllServices = []ServiceName{
	ServicePerson,
	ServiceOrganization,
	ServiceResourceType,
	ServiceResource,
}

// ValidServiceNames returns the service names as strings (useful for help text).
func ValidServiceNames() []string {
	names := make([]string, len(AllServices))
	for i, s := range AllServices {
		names[i] = string(s)
	}
	return names
}

// ValidateServiceNames returns an error if any name is not a known service.
func ValidateServiceNames(names []string) error {
	valid := make(map[string]bool, len(AllServices))
	for _, s := range AllServices {
		valid[string(s)] = true
	}
	var invalid []string
	for _, n := range names {
		if !valid[n] {
			invalid = append(invalid, n)
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf(
			"unknown service(s): %s (valid: %s)",
			strings.Join(invalid, ", "),
			strings.Join(ValidServiceNames(), ", "),
		)
	}
	return nil
}

// resolveEnabled returns a set of enabled services. If the input is nil or empty, all services are enabled.
func resolveEnabled(services []string) map[ServiceName]bool {
	enabled := make(map[ServiceName]bool, len(AllServices))
	if len(services) == 0 {
		for _, s := range AllServices {
			enabled[s] = true
		}
		return enabled
	}
	for _, s := range services {
		enabled[ServiceName(s)] = true
	}
	return enabled
}

// NewMCPServer creates a configured MCP server with the specified tool groups registered.
// If enabledServices is nil or empty, all tool groups are registered.
func NewMCPServer(
	resourceTypeService application.ResourceTypeService,
	resourceService application.ResourceService,
	enabledServices []string,
) (*mcp.Server, error) {
	if resourceTypeService == nil {
		return nil, fmt.Errorf("resourceTypeService must not be nil")
	}
	if resourceService == nil {
		return nil, fmt.Errorf("resourceService must not be nil")
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "weos",
		Title:   "WeOS MCP Server",
		Version: "0.1.0",
	}, nil)

	enabled := resolveEnabled(enabledServices)

	if enabled[ServicePerson] {
		registerPersonTools(server, resourceService)
	}
	if enabled[ServiceOrganization] {
		registerOrganizationTools(server, resourceService)
	}
	if enabled[ServiceResourceType] {
		registerResourceTypeTools(server, resourceTypeService)
		registerResourceTypePresetTools(server, resourceTypeService)
	}
	if enabled[ServiceResource] {
		registerResourceTools(server, resourceService)
	}

	return server, nil
}

// Run starts the MCP server on stdio, registering only the tool groups listed in enabledServices.
// If enabledServices is empty, all tool groups are registered.
func Run(enabledServices []string) error {
	cfg := loadConfig()

	var resourceTypeService application.ResourceTypeService
	var resourceService application.ResourceService

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
	)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()

	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	server, err := NewMCPServer(resourceTypeService, resourceService, enabledServices)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err = server.Run(ctx, &mcp.StdioTransport{})

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
