package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"golang.org/x/net/context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/fx"
)

type MCPParams struct {
	fx.In
	Config     *openapi3.T
	APIConfig  *APIConfig
	HttpClient *http.Client
	Echo       *echo.Echo
	Logger     Log
}

type MCPResult struct {
	fx.Out
	Server *server.MCPServer
}

// WithJSONSchema adds an object property to the tool schema.
func WithJSONSchema(name string, schema *openapi3.Schema) mcp.ToolOption {
	return func(t *mcp.Tool) {
		//convert the schema to a mcp ToolInputSchema
		properties := map[string]interface{}{}
		for propertyName, prop := range schema.Properties {
			if prop.Value != nil {
				properties[propertyName] = prop.Value
			}
		}

		toolInputSchema := mcp.ToolInputSchema{
			Type:       schema.Type,
			Properties: properties,
			Required:   schema.Required,
		}

		t.InputSchema.Properties[name] = toolInputSchema
	}
}

func NewMCP(p MCPParams) (result MCPResult, err error) {
	//get the endpoints with the x-mcp endpoint extension
	var mcpServer *server.MCPServer
	if p.APIConfig.MCPConfig == nil {
		return
	}

	mcpServer = server.NewMCPServer(
		p.APIConfig.Title,
		p.APIConfig.Version,
		server.WithToolCapabilities(p.APIConfig.MCPConfig.WithTools),
		server.WithRecovery(),
	)
	result.Server = mcpServer

	for path, pathItem := range p.Config.Paths.Map() {
		for method, operation := range pathItem.Operations() {
			if mcpConfig, ok := operation.Extensions[MCPExtension].(map[string]interface{}); ok {
				var toolOptions []mcp.ToolOption
				var toolHandler server.ToolHandlerFunc
				//check to see if the mcp config has a name for the tool if not use the operation id
				var toolName string
				if name, ok := mcpConfig["name"].(string); ok {
					toolName = name
				} else {
					toolName = operation.OperationID
				}
				//setup the mcp operation
				toolOptions = append(toolOptions, mcp.WithDescription(mcpConfig["description"].(string)))
				//loop through the parameters and add them to the mcp operation
				for _, param := range operation.Parameters {
					if param.Value != nil {
						var options []mcp.PropertyOption
						//if param is required, add the option
						if param.Value.Required {
							options = append(options, mcp.Required())
						}
						//check the parameter type and add it to the mcp operation
						switch param.Value.Schema.Value.Type {
						case "string":
							toolOptions = append(toolOptions, mcp.WithString(param.Value.Name, options...))
							p.Logger.Debugf("add option '%s' for mcp tool '%s'", param.Value.Name, toolName)
						case "integer":
							toolOptions = append(toolOptions, mcp.WithNumber(param.Value.Name, options...))
							p.Logger.Debugf("add option '%s' for mcp tool '%s'", param.Value.Name, toolName)
						case "boolean":
							toolOptions = append(toolOptions, mcp.WithBoolean(param.Value.Name, options...))
							p.Logger.Debugf("add option '%s' for mcp tool '%s'", param.Value.Name, toolName)
						case "array":
							toolOptions = append(toolOptions, mcp.WithArray(param.Value.Name, options...))
							p.Logger.Debugf("add option '%s' for mcp tool '%s'", param.Value.Name, toolName)
						case "object":
							toolOptions = append(toolOptions, mcp.WithObject(param.Value.Name, options...))
							p.Logger.Debugf("add option '%s' for mcp tool '%s'", param.Value.Name, toolName)
						}
					}
				}
				//if there is a request body, add it to the mcp operation
				if operation.RequestBody != nil {
					if operation.RequestBody.Value.Content != nil {
						for _, content := range operation.RequestBody.Value.Content {
							if content.Schema != nil {
								if content.Schema.Value != nil {
									toolOptions = append(toolOptions, WithJSONSchema("body", content.Schema.Value))
								}
							}
						}
					}
				}

				toolHandler = func(ctx context.Context, request mcp.CallToolRequest) (response *mcp.CallToolResult, err error) {
					// Create a new HTTP request for the endpoint
					httpUrl := path
					queryParams := url.Values{}
					headerValues := make(map[string]string)
					//for all the path parameters, replace them in the url
					for _, param := range operation.Parameters {
						if param.Value != nil && param.Value.In == "path" {
							httpUrl = strings.ReplaceAll(httpUrl, "{"+param.Value.Name+"}", fmt.Sprintf("%v", request.Params.Arguments[param.Value.Name]))
						}
						if param.Value != nil && param.Value.In == "query" {
							// Add query parameters to the URL
							queryParams.Add(param.Value.Name, fmt.Sprintf("%v", request.Params.Arguments[param.Value.Name]))
						}
						if param.Value != nil && param.Value.In == "header" {
							// Add header parameters to the request
							headerValues[param.Value.Name] = fmt.Sprintf("%v", request.Params.Arguments[param.Value.Name])
						}
					}

					// Create the request body if needed
					var reqBody io.Reader
					if request.Params.Arguments["body"] != nil {
						jsonBody, err := json.Marshal(request.Params.Arguments["body"])
						if err != nil {
							return nil, err
						}
						reqBody = bytes.NewBuffer(jsonBody)
					}

					// Create the HTTP request
					httpReq := httptest.NewRequest(string(method), httpUrl, reqBody)

					httpReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

					// Create a recorder to capture the response
					rec := httptest.NewRecorder()
					// Call the endpoint
					p.Echo.ServeHTTP(rec, httpReq)

					// Get the response body
					respBody, err := io.ReadAll(rec.Body)
					if err != nil {
						return mcp.NewToolResultErrorFromErr("error reading response", err), err
					}

					if rec.Code > 399 {
						// If the response code is not 2xx, return an error
						return mcp.NewToolResultErrorFromErr("error calling endpoint", fmt.Errorf("error calling endpoint: %s", string(respBody))), fmt.Errorf("error calling endpoint: %s", string(respBody))
					}

					// Try to parse the response as JSON
					var jsonResp interface{}
					if err := json.Unmarshal(respBody, &jsonResp); err == nil {
						response = mcp.NewToolResultResource(string(respBody), mcp.BlobResourceContents{
							Blob:     string(respBody),
							MIMEType: rec.Header().Get(echo.HeaderContentType),
							URI:      httpUrl,
						})
					} else {
						// If not JSON, use the raw string
						response = mcp.NewToolResultText(string(respBody))
					}

					return
				}

				tool := mcp.NewTool(toolName, toolOptions...)
				mcpServer.AddTool(tool, toolHandler)
				p.Logger.Debugf("mcp tool '%s' added for path '%s' with method '%s'", toolName, path, method)
			}

		}
	}
	return
}

// mcpStartupHook registers the hooks for the application
func mcpSSEStartupHook(lifecycle fx.Lifecycle, mcpServer *server.MCPServer) {
	//setup MCP SSE Server
	sseServer := server.NewSSEServer(mcpServer)
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := sseServer.Start(":" + os.Getenv("WEOS_PORT")); err != nil {

				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return sseServer.Shutdown(ctx)
		},
	})
}

func mcpStdIOHook(lifecycle fx.Lifecycle, mcpServer *server.MCPServer) {
	//setup MCP SSE Server
	sseServer := server.NewSSEServer(mcpServer)
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if err := server.ServeStdio(mcpServer); err != nil {
				return err
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return sseServer.Shutdown(ctx)
		},
	})
}

var MCP = fx.Module("mcp",
	fx.Provide(Core, NewMCP),
	fx.Invoke(mcpStdIOHook))

var MCPSSE = fx.Module("mcp-sse",
	fx.Provide(Core, NewMCP),
	fx.Invoke(mcpSSEStartupHook))
