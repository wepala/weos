package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/wepala/weos/application"
	"github.com/wepala/weos/domain/entities"
	"github.com/wepala/weos/domain/repositories"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateOrganizationInput struct {
	Name string `json:"name" jsonschema:"organization display name"`
	Slug string `json:"slug" jsonschema:"URL-friendly identifier"`
}

type UpdateOrganizationInput struct {
	ID          string `json:"id" jsonschema:"organization ID (URN)"`
	Name        string `json:"name" jsonschema:"organization display name"`
	Slug        string `json:"slug,omitempty" jsonschema:"URL slug"`
	Description string `json:"description,omitempty" jsonschema:"organization description"`
	URL         string `json:"url,omitempty" jsonschema:"organization website URL"`
	LogoURL     string `json:"logo_url,omitempty" jsonschema:"logo image URL"`
	Status      string `json:"status,omitempty" jsonschema:"status (active or archived)"`
}

type DeleteOrganizationInput struct {
	ID string `json:"id" jsonschema:"organization ID (URN)"`
}

type GetOrganizationInput struct {
	ID string `json:"id" jsonschema:"organization ID (URN)"`
}

type ListOrganizationsInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type OrganizationOutput struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	URL         string    `json:"url,omitempty"`
	LogoURL     string    `json:"logo_url,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListOrganizationsOutput struct {
	Data    []OrganizationOutput `json:"data"`
	Cursor  string               `json:"cursor,omitempty"`
	HasMore bool                 `json:"has_more"`
}

func toOrganizationOutput(r *entities.Resource) OrganizationOutput {
	fields, _ := application.ExtractResourceFields(r)
	return OrganizationOutput{
		ID:          r.GetID(),
		Name:        application.StringField(fields, "name"),
		Slug:        application.StringField(fields, "slug"),
		Description: application.StringField(fields, "description"),
		URL:         application.StringField(fields, "url"),
		LogoURL:     application.StringField(fields, "logoURL"),
		Status:      r.Status(),
		CreatedAt:   r.CreatedAt(),
	}
}

func registerOrganizationTools(server *mcp.Server, svc application.ResourceService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "organization_create",
		Description: "Create a new organization.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateOrganizationInput) (*mcp.CallToolResult, OrganizationOutput, error) {
		data, _ := json.Marshal(map[string]any{
			"name": input.Name, "slug": input.Slug,
		})
		entity, err := svc.Create(ctx, application.CreateResourceCommand{TypeSlug: "organization", Data: data})
		if err != nil {
			return nil, OrganizationOutput{}, err
		}
		return nil, toOrganizationOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "organization_get",
		Description: "Get an organization by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetOrganizationInput) (*mcp.CallToolResult, OrganizationOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, OrganizationOutput{}, err
		}
		return nil, toOrganizationOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "organization_list",
		Description: "List all organizations with cursor-based pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListOrganizationsInput) (*mcp.CallToolResult, ListOrganizationsOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, "organization", input.Cursor, limit, repositories.SortOptions{})
		if err != nil {
			return nil, ListOrganizationsOutput{}, err
		}
		out := ListOrganizationsOutput{
			Data:    make([]OrganizationOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toOrganizationOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "organization_update",
		Description: "Update an existing organization.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateOrganizationInput) (*mcp.CallToolResult, OrganizationOutput, error) {
		data, _ := json.Marshal(map[string]any{
			"name": input.Name, "slug": input.Slug,
			"description": input.Description, "url": input.URL, "logoURL": input.LogoURL,
		})
		entity, err := svc.Update(ctx, application.UpdateResourceCommand{ID: input.ID, Data: data})
		if err != nil {
			return nil, OrganizationOutput{}, err
		}
		return nil, toOrganizationOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "organization_delete",
		Description: "Delete an organization by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteOrganizationInput) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteResourceCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
