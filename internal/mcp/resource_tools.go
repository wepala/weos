package mcp

import (
	"context"
	"encoding/json"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateResourceInput struct {
	TypeSlug string          `json:"type_slug" jsonschema:"resource type slug"`
	Data     json.RawMessage `json:"data" jsonschema:"resource data as JSON"`
}

type UpdateResourceInput struct {
	ID   string          `json:"id" jsonschema:"resource ID (URN)"`
	Data json.RawMessage `json:"data" jsonschema:"updated resource data as JSON"`
}

type DeleteResourceInput struct {
	ID string `json:"id" jsonschema:"resource ID (URN)"`
}

type GetResourceInput struct {
	ID string `json:"id" jsonschema:"resource ID (URN)"`
}

type ListResourcesInput struct {
	TypeSlug string `json:"type_slug" jsonschema:"resource type slug"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type ResourceOutput struct {
	ID        string          `json:"id"`
	TypeSlug  string          `json:"type_slug"`
	Data      json.RawMessage `json:"data"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
}

type ListResourcesOutput struct {
	Data    []ResourceOutput `json:"data"`
	Cursor  string           `json:"cursor,omitempty"`
	HasMore bool             `json:"has_more"`
}

func toResourceOutput(e *entities.Resource) ResourceOutput {
	return ResourceOutput{
		ID:        e.GetID(),
		TypeSlug:  e.TypeSlug(),
		Data:      e.Data(),
		Status:    e.Status(),
		CreatedAt: e.CreatedAt(),
	}
}

func registerResourceTools(server *mcp.Server, svc application.ResourceService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_create",
		Description: "Create a new resource of a given type. Data is validated against the type's JSON Schema if defined.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input CreateResourceInput,
	) (*mcp.CallToolResult, ResourceOutput, error) {
		entity, err := svc.Create(ctx, application.CreateResourceCommand{
			TypeSlug: input.TypeSlug, Data: input.Data,
		})
		if err != nil {
			return nil, ResourceOutput{}, err
		}
		return nil, toResourceOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_get",
		Description: "Get a resource by ID. Returns full JSON-LD data.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input GetResourceInput,
	) (*mcp.CallToolResult, ResourceOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, ResourceOutput{}, err
		}
		return nil, toResourceOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_list",
		Description: "List resources of a given type with cursor-based pagination.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input ListResourcesInput,
	) (*mcp.CallToolResult, ListResourcesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.TypeSlug, input.Cursor, limit)
		if err != nil {
			return nil, ListResourcesOutput{}, err
		}
		out := ListResourcesOutput{
			Data:    make([]ResourceOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toResourceOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_update",
		Description: "Update an existing resource. Data is re-validated against the type's JSON Schema.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input UpdateResourceInput,
	) (*mcp.CallToolResult, ResourceOutput, error) {
		entity, err := svc.Update(ctx, application.UpdateResourceCommand{
			ID: input.ID, Data: input.Data,
		})
		if err != nil {
			return nil, ResourceOutput{}, err
		}
		return nil, toResourceOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_delete",
		Description: "Delete a resource by ID.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input DeleteResourceInput,
	) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteResourceCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
