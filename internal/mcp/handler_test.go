package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubResourceTypeService is a minimal stub satisfying application.ResourceTypeService.
type stubResourceTypeService struct{}

func (s *stubResourceTypeService) Create(
	_ context.Context, _ application.CreateResourceTypeCommand,
) (*entities.ResourceType, error) {
	return nil, nil
}

func (s *stubResourceTypeService) GetByID(_ context.Context, _ string) (*entities.ResourceType, error) {
	return nil, nil
}

func (s *stubResourceTypeService) GetBySlug(_ context.Context, _ string) (*entities.ResourceType, error) {
	return nil, nil
}

func (s *stubResourceTypeService) List(
	_ context.Context, _ string, _ int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	return repositories.PaginatedResponse[*entities.ResourceType]{}, nil
}

func (s *stubResourceTypeService) Update(
	_ context.Context, _ application.UpdateResourceTypeCommand,
) (*entities.ResourceType, error) {
	return nil, nil
}

func (s *stubResourceTypeService) Delete(_ context.Context, _ application.DeleteResourceTypeCommand) error {
	return nil
}

func (s *stubResourceTypeService) ListPresets() []application.PresetDefinition {
	return nil
}

func (s *stubResourceTypeService) InstallPreset(
	_ context.Context, _ string, _ bool,
) (*application.InstallPresetResult, error) {
	return nil, nil
}

func (s *stubResourceTypeService) ListBehaviors(
	_ context.Context, _ string,
) ([]application.BehaviorInfo, error) {
	return nil, nil
}

func (s *stubResourceTypeService) SetBehaviors(_ context.Context, _ string, _ []string) error {
	return nil
}

// stubResourceService is a minimal stub satisfying application.ResourceService.
type stubResourceService struct{}

func (s *stubResourceService) Create(
	_ context.Context, _ application.CreateResourceCommand,
) (*entities.Resource, error) {
	return nil, nil
}

func (s *stubResourceService) GetByID(_ context.Context, _ string) (*entities.Resource, error) {
	return nil, nil
}

func (s *stubResourceService) List(
	_ context.Context, _, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (s *stubResourceService) ListFlat(
	_ context.Context, _, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	return repositories.PaginatedResponse[map[string]any]{}, nil
}

func (s *stubResourceService) ListByField(
	_ context.Context, _, _, _ string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (s *stubResourceService) ListWithFilters(
	_ context.Context, _ string, _ []repositories.FilterCondition, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (s *stubResourceService) ListFlatWithFilters(
	_ context.Context, _ string, _ []repositories.FilterCondition, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	return repositories.PaginatedResponse[map[string]any]{}, nil
}

func (s *stubResourceService) Update(
	_ context.Context, _ application.UpdateResourceCommand,
) (*entities.Resource, error) {
	return nil, nil
}

func (s *stubResourceService) Delete(_ context.Context, _ application.DeleteResourceCommand) error {
	return nil
}

// toolNames connects to an MCP server via in-memory transport and returns the registered tool names.
func toolNames(t *testing.T, server *gomcp.Server) []string {
	t.Helper()
	serverTransport, clientTransport := gomcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		_, err := server.Connect(ctx, serverTransport, nil)
		serverErr <- err
	}()

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	sort.Strings(names)

	// Cancel context and wait for server goroutine to finish.
	cancel()
	if sErr := <-serverErr; sErr != nil && sErr != context.Canceled {
		t.Errorf("server connect error: %v", sErr)
	}

	return names
}

func TestNewMCPServer_AllServices(t *testing.T) {
	server, err := NewMCPServer(&stubResourceTypeService{}, &stubResourceService{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if server == nil {
		t.Fatal("expected non-nil server")
	}

	names := toolNames(t, server)

	// All 4 service groups should be registered (22 tools total).
	expectedPrefixes := []string{"person_", "organization_", "resource_type_", "resource_"}
	for _, prefix := range expectedPrefixes {
		found := false
		for _, name := range names {
			if strings.HasPrefix(name, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected at least one tool with prefix %q, got: %v", prefix, names)
		}
	}

	if len(names) != 24 {
		t.Errorf("expected 24 tools, got %d: %v", len(names), names)
	}
}

func TestNewMCPServer_Subset(t *testing.T) {
	server, err := NewMCPServer(&stubResourceTypeService{}, &stubResourceService{}, []string{"person"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := toolNames(t, server)

	for _, name := range names {
		if !strings.HasPrefix(name, "person_") {
			t.Errorf("unexpected tool %q for person-only config", name)
		}
	}
	if len(names) != 5 {
		t.Errorf("expected 5 person tools, got %d: %v", len(names), names)
	}
}

func TestNewMCPServer_ResourceTypeIncludesPresets(t *testing.T) {
	server, err := NewMCPServer(
		&stubResourceTypeService{}, &stubResourceService{}, []string{"resource-type"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := toolNames(t, server)

	hasPreset := false
	for _, name := range names {
		if strings.HasPrefix(name, "resource_type_preset_") {
			hasPreset = true
			break
		}
	}
	if !hasPreset {
		t.Errorf("expected resource_type_preset_* tools to be registered with resource-type service, got: %v", names)
	}
}

func TestNewMCPServer_NilResourceTypeService(t *testing.T) {
	_, err := NewMCPServer(nil, &stubResourceService{}, nil)
	if err == nil {
		t.Fatal("expected error for nil resourceTypeService")
	}
}

func TestNewMCPServer_NilResourceService(t *testing.T) {
	_, err := NewMCPServer(&stubResourceTypeService{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil resourceService")
	}
}

func TestNewHTTPHandler_ReturnsHandler(t *testing.T) {
	handler, err := NewHTTPHandler(&stubResourceTypeService{}, &stubResourceService{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewHTTPHandler_AcceptsMCPRequest(t *testing.T) {
	handler, err := NewHTTPHandler(&stubResourceTypeService{}, &stubResourceService{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Send a valid MCP initialize JSON-RPC request.
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
		`"protocolVersion":"2025-06-18",` +
		`"capabilities":{},` +
		`"clientInfo":{"name":"test","version":"0.1.0"}}}`

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "weos") {
		t.Errorf("expected response to contain server name 'weos', got: %s", rec.Body.String())
	}
}

func TestNewHTTPHandler_NilServices(t *testing.T) {
	_, err := NewHTTPHandler(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil services")
	}
}
