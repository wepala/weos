package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/internal/config"

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
	ServicePerson         ServiceName = "person"
	ServiceOrganization   ServiceName = "organization"
	ServiceResourceType   ServiceName = "resource-type"
	ServiceResource       ServiceName = "resource"
	ServiceKnowledgeGraph ServiceName = "knowledge-graph"
)

// AllServices is the ordered list of every available service.
var AllServices = []ServiceName{
	ServicePerson,
	ServiceOrganization,
	ServiceResourceType,
	ServiceResource,
	ServiceKnowledgeGraph,
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
//
// kgService may be nil. When nil, the knowledge-graph tool group is omitted
// entirely (calls surface as "tool not found"). When non-nil but wrapping an
// inactive store (Oxigraph not configured), the tools ARE registered and each
// call returns ErrKGUnavailable so the LLM gets a clear "knowledge graph not
// configured" signal instead of a missing-tool error.
func NewMCPServer(
	resourceTypeService application.ResourceTypeService,
	resourceService application.ResourceService,
	kgService application.KnowledgeGraphService,
	enabledServices []string,
) (*mcp.Server, error) {
	if isNilInterface(resourceTypeService) {
		return nil, fmt.Errorf("resourceTypeService must not be nil")
	}
	if isNilInterface(resourceService) {
		return nil, fmt.Errorf("resourceService must not be nil")
	}
	if len(enabledServices) > 0 {
		if err := ValidateServiceNames(enabledServices); err != nil {
			return nil, err
		}
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
	if enabled[ServiceKnowledgeGraph] && !isNilInterface(kgService) {
		registerKnowledgeGraphTools(server, kgService)
	}

	return server, nil
}

// Run starts the MCP server on stdio, registering only the tool groups listed in enabledServices.
// If enabledServices is nil or empty, all tool groups are registered.
func Run(enabledServices []string) error {
	cfg := loadConfig()

	var resourceTypeService application.ResourceTypeService
	var resourceService application.ResourceService
	var kgService application.KnowledgeGraphService

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
		fx.Populate(&kgService),
	)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()

	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		if stopErr := app.Stop(stopCtx); stopErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to stop application: %v\n", stopErr)
		}
	}()

	server, err := NewMCPServer(resourceTypeService, resourceService, kgService, enabledServices)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !isCleanShutdown(ctx, err) {
		return err
	}
	return nil
}

// isCleanShutdown reports whether err is the normal end of a stdio MCP session
// rather than a real failure. A desktop client ends the session by closing the
// server's stdin (surfaces as io.EOF) or by signaling the process
// (SIGINT/SIGTERM, which cancels ctx). The SDK reports a closed stdin as an
// "...server is closing: EOF" error whose cause is joined with %v, so io.EOF is
// not in the chain — fall back to matching that message for the case where EOF
// arrives mid-handshake.
func isCleanShutdown(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	return strings.Contains(err.Error(), "server is closing")
}

// isNilInterface returns true if v is nil or a typed-nil (interface wrapping a nil pointer).
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func loadConfig() config.Config {
	cfg := config.Default()
	cfg.LoadFromEnvironment()
	return cfg
}
