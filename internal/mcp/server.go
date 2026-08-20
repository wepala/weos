package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	ServiceMemory         ServiceName = "memory"
	ServiceFeature        ServiceName = "feature"
)

// AllServices is the ordered list of every available service.
var AllServices = []ServiceName{
	ServicePerson,
	ServiceOrganization,
	ServiceResourceType,
	ServiceResource,
	ServiceKnowledgeGraph,
	ServiceMemory,
	ServiceFeature,
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
	lexicalSearch application.LexicalSearch,
	episodicRecall application.EpisodicRecall,
	featureAdmin *application.FeatureAdminService,
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
	if enabled[ServiceFeature] && featureAdmin != nil {
		registerFeatureTools(server, featureAdmin)
	}
	if enabled[ServiceMemory] {
		// The playbook and recall services are thin stateless wrappers over
		// services this constructor already receives, so they are built here
		// rather than threaded through every NewMCPServer caller. A nil
		// kgService means recall serves from the working set alone. The
		// lexical search needs the FTS5 index and IS threaded in; when nil
		// (tests, minimal wiring) memory_search is simply not registered.
		kg := kgService
		if isNilInterface(kgService) {
			kg = nil
		}
		recall := application.NewMemoryRecall(kg, application.NewWorkingMemory(resourceService))
		var search application.LexicalSearch
		if !isNilInterface(lexicalSearch) {
			search = lexicalSearch
		}
		registerMemoryTools(server, application.NewPlaybookService(resourceService), recall, search)
		// Episodic recall needs the event-log repository and IS threaded in;
		// when nil (tests, minimal wiring) episodic_recall is not registered.
		if !isNilInterface(episodicRecall) {
			registerEpisodicTools(server, episodicRecall)
		}
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
	var lexicalSearch application.LexicalSearch
	var episodicRecall application.EpisodicRecall
	var featureAdmin *application.FeatureAdminService

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
		fx.Populate(&kgService),
		fx.Populate(&lexicalSearch),
		fx.Populate(&episodicRecall),
		fx.Populate(&featureAdmin),
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

	server, err := NewMCPServer(
		resourceTypeService, resourceService, kgService, lexicalSearch, episodicRecall,
		featureAdmin, enabledServices)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}
	// Custom tools registered by downstream binaries run on every
	// transport; the stdio path applies them here. Use an explicit stderr
	// logger rather than slog.Default(): the default can be redirected to
	// stdout by downstream code, which would corrupt the stdio MCP
	// protocol stream.
	applyConfigurers(server, ConfigurerDeps{
		ResourceService:     resourceService,
		ResourceTypeService: resourceTypeService,
		Logger:              slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// The stdio transport is a single local caller with no per-request identity.
	// Mark the session so per-account knowledge-graph mode serves the local graph
	// (the stdio exception) instead of failing closed on an unresolved account.
	ctx = application.WithLocalTransport(ctx)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !isCleanShutdown(ctx, err) {
		return err
	}
	return nil
}

// isCleanShutdown reports whether err is the normal end of a stdio MCP session
// rather than a real failure, so the caller can exit 0 instead of surfacing it.
// Two signals count as clean:
//
//   - ctx was canceled — the process got SIGINT/SIGTERM and server.Run returned
//     ctx.Err(). This is the common interactive-shutdown path.
//   - the client closed stdin mid-handshake — the SDK returns an error whose
//     message is "server is closing: EOF". The EOF cause is joined with %v (not
//     %w), so errors.Is(err, io.EOF) can't see it; we match the message but also
//     require the EOF cause, so a non-EOF transport failure wrapped in the same
//     "server is closing" prefix (e.g. a broken pipe) still propagates as a real
//     error rather than being mistaken for a clean exit.
//
// Note: a clean stdin EOF on an already-established session does NOT reach here.
// The SDK's jsonrpc2 wait() swallows a bare io.EOF read error and server.Run
// returns nil, which the caller treats as success before calling this. The
// errors.Is(io.EOF) check below is only defensive cover for wrapped-EOF variants.
//
// Verified against modelcontextprotocol/go-sdk v1.4.1.
func isCleanShutdown(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "server is closing") && strings.Contains(msg, "EOF")
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
