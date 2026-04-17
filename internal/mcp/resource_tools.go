package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wepala/weos/application"
	"github.com/wepala/weos/domain/entities"
	"github.com/wepala/weos/domain/repositories"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateResourceInput struct {
	TypeSlug string `json:"type_slug" jsonschema:"resource type slug"`
	Data     any    `json:"data" jsonschema:"resource data as JSON object"`
}

type UpdateResourceInput struct {
	ID   string `json:"id" jsonschema:"resource ID (URN)"`
	Data any    `json:"data" jsonschema:"updated resource data as JSON object"`
}

type DeleteResourceInput struct {
	ID string `json:"id" jsonschema:"resource ID (URN)"`
}

type GetResourceInput struct {
	ID string `json:"id" jsonschema:"resource ID (URN)"`
}

type ListResourcesInput struct {
	TypeSlug  string `json:"type_slug" jsonschema:"resource type slug"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
	SortBy    string `json:"sort_by,omitempty" jsonschema:"column to sort by (e.g. submittedAt, createdAt)"`
	SortOrder string `json:"sort_order,omitempty" jsonschema:"sort order: asc or desc"`
}

type ResourceOutput struct {
	ID        string    `json:"id"`
	TypeSlug  string    `json:"type_slug"`
	Data      any       `json:"data"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ListResourcesOutput struct {
	Data    []ResourceOutput `json:"data"`
	Cursor  string           `json:"cursor,omitempty"`
	HasMore bool             `json:"has_more"`
}

func toResourceOutput(e *entities.Resource) ResourceOutput {
	var data any
	_ = json.Unmarshal(e.Data(), &data)
	return ResourceOutput{
		ID:        e.GetID(),
		TypeSlug:  e.TypeSlug(),
		Data:      data,
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
		dataBytes, err := json.Marshal(input.Data)
		if err != nil {
			return nil, ResourceOutput{}, fmt.Errorf("invalid data: %w", err)
		}
		entity, err := svc.Create(ctx, application.CreateResourceCommand{
			TypeSlug: input.TypeSlug, Data: dataBytes,
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
		sort := repositories.SortOptions{SortBy: input.SortBy, SortOrder: input.SortOrder}
		result, err := svc.List(ctx, input.TypeSlug, input.Cursor, limit, sort)
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
		dataBytes, err := json.Marshal(input.Data)
		if err != nil {
			return nil, ResourceOutput{}, fmt.Errorf("invalid data: %w", err)
		}
		entity, err := svc.Update(ctx, application.UpdateResourceCommand{
			ID: input.ID, Data: dataBytes,
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
