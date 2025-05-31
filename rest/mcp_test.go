package rest_test

import (
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/net/context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"

	"github.com/wepala/weos/v2/rest"
)

func TestMCPProviderWithBlogAPI(t *testing.T) {
	// Load the blog-mcp.yaml fixture
	apiPath, err := filepath.Abs("fixtures/blog-mcp.yaml")
	if err != nil {
		t.Fatalf("Failed to resolve API path: %v", err)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	api, err := loader.LoadFromFile(apiPath)
	if err != nil {
		t.Fatalf("Failed to load API spec: %v", err)
	}

	// Create an Echo instance
	e := echo.New()
	httpClient := &http.Client{}

	// Initialize API config with MCP support
	apiConfig := &rest.APIConfig{
		ServiceConfig: &rest.ServiceConfig{
			Title: "Blog with MCP Test",
			MCPConfig: &rest.MCPConfig{
				WithTools: true,
			},
		},
		Version: "1.0.0",
	}

	// Setup a mock logger
	logger := &LogMock{
		DebugFunc: func(args ...interface{}) {

		},
		DebugfFunc: func(format string, args ...interface{}) {

		},
	}

	// Create MCP provider parameters
	params := rest.MCPParams{
		Config:     api,
		APIConfig:  apiConfig,
		HttpClient: httpClient,
		Echo:       e,
		Logger:     logger,
	}

	// Initialize the MCP provider
	mcpResult, err := rest.NewMCP(params)
	if err != nil {
		t.Fatalf("Failed to initialize MCP provider: %v", err)
	}

	testServer := server.NewTestServer(mcpResult.Server)
	defer testServer.Close()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run tests
	t.Run("should return the tools available", func(t *testing.T) {
		var mcpClient *client.Client
		sseTransport, err := transport.NewSSE(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create SSE transport: %v", err)
		}
		//start the transport
		if err := sseTransport.Start(ctx); err != nil {
			t.Fatalf("Failed to start SSE transport: %v", err)
		}
		mcpClient = client.NewClient(sseTransport)

		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "MCP-Go Simple Client Example",
			Version: "1.0.0",
		}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}

		serverInfo, err := mcpClient.Initialize(ctx, initRequest)
		if err != nil {
			t.Errorf("Failed to initialize MCP provider: %v", err)
		}
		if serverInfo == nil {
			t.Errorf("Expected server info, got nil")
		}
		// Verify specific tools exist that we defined in the fixture
		expectedTools := []string{
			"listBlogs", "createBlog", "getBlog", "updateBlog", "deleteBlog",
			"ListBlogPosts", "createBlogPost", "getBlogPost", "updateBlogPost", "deleteBlogPost",
		}
		toolsListResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			t.Errorf("Failed to list tools: %v", err)
		}
		if toolsListResult == nil {
			t.Errorf("Expected tools list result, got nil")
		}
		if len(toolsListResult.Tools) == 0 {
			t.Errorf("Expected tools list result to have tools, got empty list")
		}
		for _, expectedTool := range expectedTools {
			found := false
			for _, tool := range toolsListResult.Tools {
				if tool.Name == expectedTool {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected tool '%s' not found in tools list", expectedTool)
			}
		}
	})
}

func TestMCPProviderWithComplexAPI(t *testing.T) {
	// Load the blog-mcp.yaml fixture
	apiPath, err := filepath.Abs("fixtures/mcp-complex.yaml")
	if err != nil {
		t.Fatalf("Failed to resolve API path: %v", err)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	api, err := loader.LoadFromFile(apiPath)
	if err != nil {
		t.Fatalf("Failed to load API spec: %v", err)
	}

	// Create an Echo instance
	e := echo.New()
	httpClient := &http.Client{}

	// Initialize API config with MCP support
	apiConfig := &rest.APIConfig{
		ServiceConfig: &rest.ServiceConfig{
			Title: "Blog with MCP Test",
			MCPConfig: &rest.MCPConfig{
				WithTools: true,
			},
		},
		Version: "1.0.0",
	}

	// Setup a mock logger
	logger := &LogMock{
		DebugFunc: func(args ...interface{}) {

		},
		DebugfFunc: func(format string, args ...interface{}) {

		},
	}

	// Create MCP provider parameters
	params := rest.MCPParams{
		Config:     api,
		APIConfig:  apiConfig,
		HttpClient: httpClient,
		Echo:       e,
		Logger:     logger,
	}

	// Initialize the MCP provider
	mcpResult, err := rest.NewMCP(params)
	if err != nil {
		t.Fatalf("Failed to initialize MCP provider: %v", err)
	}

	testServer := server.NewTestServer(mcpResult.Server)
	defer testServer.Close()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run tests
	t.Run("should return the tools available", func(t *testing.T) {
		var mcpClient *client.Client
		sseTransport, err := transport.NewSSE(testServer.URL + "/sse")
		if err != nil {
			t.Fatalf("Failed to create SSE transport: %v", err)
		}
		//start the transport
		if err := sseTransport.Start(ctx); err != nil {
			t.Fatalf("Failed to start SSE transport: %v", err)
		}
		mcpClient = client.NewClient(sseTransport)

		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "MCP-Go Simple Client Example",
			Version: "1.0.0",
		}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}

		serverInfo, err := mcpClient.Initialize(ctx, initRequest)
		if err != nil {
			t.Errorf("Failed to initialize MCP provider: %v", err)
		}
		if serverInfo == nil {
			t.Errorf("Expected server info, got nil")
		}
		// Verify specific tools exist that we defined in the fixture
		expectedTools := []string{
			"listTransactions", "upsertTransaction", "getTransaction", "deleteTransaction", "listAccounts",
			"upsertAccount", "getAccount", "deleteAccount", "listCustomers", "upsertCustomer",
		}
		toolsListResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			t.Errorf("Failed to list tools: %v", err)
		}
		if toolsListResult == nil {
			t.Errorf("Expected tools list result, got nil")
		}
		if len(toolsListResult.Tools) == 0 {
			t.Errorf("Expected tools list result to have tools, got empty list")
		}
		for _, expectedTool := range expectedTools {
			found := false
			for _, tool := range toolsListResult.Tools {
				if tool.Name == expectedTool {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected tool '%s' not found in tools list", expectedTool)
			}
		}
	})
}

func TestToolHandler(t *testing.T) {
	// Load the blog-mcp.yaml fixture
	apiPath, err := filepath.Abs("fixtures/mcp-complex.yaml")
	if err != nil {
		t.Fatalf("Failed to resolve API path: %v", err)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	api, err := loader.LoadFromFile(apiPath)
	if err != nil {
		t.Fatalf("Failed to load API spec: %v", err)
	}

	// Create an Echo instance
	e := echo.New()

	// Initialize API config with MCP support
	apiConfig := &rest.APIConfig{
		ServiceConfig: &rest.ServiceConfig{
			Title: "Blog with MCP Test",
			MCPConfig: &rest.MCPConfig{
				WithTools: true,
			},
		},
		Version: "1.0.0",
	}

	// Setup a mock logger
	logger := &LogMock{
		DebugFunc: func(args ...interface{}) {

		},
		DebugfFunc: func(format string, args ...interface{}) {

		},
	}
	type Filter struct {
		// General fields
		ID        rest.Operator `json:"id,omitempty"`
		Created   rest.Operator `json:"created,omitempty"`
		Updated   rest.Operator `json:"updated,omitempty"`
		DeletedAt rest.Operator `json:"deletedAt,omitempty"`

		// Transaction fields
		TransactionType rest.Operator `json:"transactionType,omitempty"`
		Amount          rest.Operator `json:"amount,omitempty"`
	}
	type ListRequest struct {
		Filter       Filter   `json:"_filter"`
		Cursor       string   `json:"cursor"`
		Integrations []string `json:"integrations"`
	}
	var req ListRequest
	e.GET("/transactions", func(c echo.Context) error {
		// Extract query parameters from the context
		return rest.BindComplexParams(c, &req)
	})

	t.Run("should pass integrations as query parameters to route handler ", func(t *testing.T) {
		toolHandler := rest.ToolHandler(logger, "/transactions", "listTransactions", http.MethodGet, apiConfig, api.Paths.Value("/transactions").Get, e)

		// Create a context and MCP call tool request with test arguments
		ctx := context.Background()
		request := mcp.CallToolRequest{}
		request.Params.Name = "listTransactions"
		request.Params.Arguments = make(map[string]interface{})
		request.Params.Arguments["integrations"] = []string{"quickbooks"}

		// Call tool handler with integration arguments
		_, err = toolHandler(ctx, request)
		if err != nil {
			t.Fatalf("Tool handler returned an error: %v", err)
		}
		if len(req.Integrations) != 1 {
			t.Fatalf("Expected 1 integration, got %d", len(req.Integrations))
		}
	})
	t.Run("should pass filter query parameters to the route handler", func(t *testing.T) {
		toolHandler := rest.ToolHandler(logger, "/transactions", "listTransactions", http.MethodGet, apiConfig, api.Paths.Value("/transactions").Get, e)

		// Create a context and MCP call tool request with test arguments
		ctx := context.Background()
		request := mcp.CallToolRequest{}
		request.Params.Name = "listTransactions"
		request.Params.Arguments = make(map[string]interface{})
		request.Params.Arguments["_filter"] = map[string]interface{}{
			"id": map[string]interface{}{
				"eq": "1234",
			},
		}

		// Call tool handler with integration arguments
		_, err = toolHandler(ctx, request)
		if err != nil {
			t.Fatalf("Tool handler returned an error: %v", err)
		}
		if req.Filter.ID.Eq != "1234" {
			t.Fatalf("Expected filter ID to be '1234', got '%s'", req.Filter.ID.Eq)
		}
	})
}
