package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/wepala/weos/application"
	"github.com/wepala/weos/domain/entities"
	"github.com/wepala/weos/pkg/jsonld"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateResourceTypeInput struct {
	Name        string          `json:"name" jsonschema:"resource type display name"`
	Slug        string          `json:"slug" jsonschema:"URL-friendly identifier"`
	Description string          `json:"description,omitempty" jsonschema:"resource type description"`
	Context     json.RawMessage `json:"context,omitempty" jsonschema:"JSON-LD context"`
	Schema      json.RawMessage `json:"schema,omitempty" jsonschema:"JSON Schema for validation"`
}

type UpdateResourceTypeInput struct {
	ID          string          `json:"id" jsonschema:"resource type ID (URN)"`
	Name        string          `json:"name" jsonschema:"resource type display name"`
	Slug        string          `json:"slug,omitempty" jsonschema:"URL slug"`
	Description string          `json:"description,omitempty" jsonschema:"resource type description"`
	Context     json.RawMessage `json:"context,omitempty" jsonschema:"JSON-LD context"`
	Schema      json.RawMessage `json:"schema,omitempty" jsonschema:"JSON Schema for validation"`
	Status      string          `json:"status,omitempty" jsonschema:"status (active or archived)"`
}

type DeleteResourceTypeInput struct {
	ID string `json:"id" jsonschema:"resource type ID (URN)"`
}

type GetResourceTypeInput struct {
	ID string `json:"id" jsonschema:"resource type ID (URN)"`
}

type ListResourceTypesInput struct {
	Cursor     string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
	IncludeAll bool   `json:"includeAll,omitempty" jsonschema:"include value object and abstract types (hidden from navigation by default)"`
}

type ResourceTypeOutput struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description,omitempty"`
	Context     json.RawMessage `json:"context,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
}

type ListResourceTypesOutput struct {
	Data    []ResourceTypeOutput `json:"data"`
	Cursor  string               `json:"cursor,omitempty"`
	HasMore bool                 `json:"has_more"`
}

func toResourceTypeOutput(e *entities.ResourceType) ResourceTypeOutput {
	return ResourceTypeOutput{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		Context:     e.Context(),
		Schema:      e.Schema(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt(),
	}
}

func registerResourceTypeTools(server *mcp.Server, svc application.ResourceTypeService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_create",
		Description: "Create a new resource type with JSON-LD context and optional JSON Schema.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input CreateResourceTypeInput,
	) (*mcp.CallToolResult, ResourceTypeOutput, error) {
		entity, err := svc.Create(ctx, application.CreateResourceTypeCommand{
			Name: input.Name, Slug: input.Slug, Description: input.Description,
			Context: input.Context, Schema: input.Schema,
		})
		if err != nil {
			return nil, ResourceTypeOutput{}, err
		}
		return nil, toResourceTypeOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_get",
		Description: "Get a resource type by ID.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input GetResourceTypeInput,
	) (*mcp.CallToolResult, ResourceTypeOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, ResourceTypeOutput{}, err
		}
		return nil, toResourceTypeOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_list",
		Description: "List all resource types with cursor-based pagination.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input ListResourceTypesInput,
	) (*mcp.CallToolResult, ListResourceTypesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.Cursor, limit)
		if err != nil {
			return nil, ListResourceTypesOutput{}, err
		}
		out := ListResourceTypesOutput{
			Data:    make([]ResourceTypeOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			if !input.IncludeAll && (jsonld.IsValueObject(e.Context()) || jsonld.IsAbstract(e.Context())) {
				continue
			}
			out.Data = append(out.Data, toResourceTypeOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_update",
		Description: "Update an existing resource type.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input UpdateResourceTypeInput,
	) (*mcp.CallToolResult, ResourceTypeOutput, error) {
		entity, err := svc.Update(ctx, application.UpdateResourceTypeCommand{
			ID: input.ID, Name: input.Name, Slug: input.Slug,
			Description: input.Description, Context: input.Context,
			Schema: input.Schema, Status: input.Status,
		})
		if err != nil {
			return nil, ResourceTypeOutput{}, err
		}
		return nil, toResourceTypeOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_delete",
		Description: "Delete a resource type by ID.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input DeleteResourceTypeInput,
	) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteResourceTypeCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
