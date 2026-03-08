package mcp

import (
	"context"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateTemplateInput struct {
	ThemeID string `json:"theme_id" jsonschema:"parent theme ID (URN)"`
	Name    string `json:"name" jsonschema:"template display name"`
	Slug    string `json:"slug" jsonschema:"URL-friendly identifier"`
}

type UpdateTemplateInput struct {
	ID          string `json:"id" jsonschema:"template ID (URN)"`
	ThemeID     string `json:"theme_id,omitempty" jsonschema:"parent theme ID"`
	Name        string `json:"name" jsonschema:"template display name"`
	Slug        string `json:"slug,omitempty" jsonschema:"URL slug"`
	Description string `json:"description,omitempty" jsonschema:"template description"`
	FilePath    string `json:"file_path,omitempty" jsonschema:"path to the HTML template file"`
	Status      string `json:"status,omitempty" jsonschema:"status (draft or published)"`
}

type DeleteTemplateInput struct {
	ID string `json:"id" jsonschema:"template ID (URN)"`
}

type GetTemplateInput struct {
	ID string `json:"id" jsonschema:"template ID (URN)"`
}

type ListTemplatesInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type ListTemplatesByThemeInput struct {
	ThemeID string `json:"theme_id" jsonschema:"parent theme ID (URN)"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"pagination cursor"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type TemplateOutput struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	FilePath    string    `json:"file_path,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListTemplatesOutput struct {
	Data    []TemplateOutput `json:"data"`
	Cursor  string           `json:"cursor,omitempty"`
	HasMore bool             `json:"has_more"`
}

func toTemplateOutput(e *entities.Template) TemplateOutput {
	return TemplateOutput{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		FilePath:    e.FilePath(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt(),
	}
}

func registerTemplateTools(server *mcp.Server, svc application.TemplateService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "template_create",
		Description: "Create a new HTML template within a theme.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateTemplateInput) (*mcp.CallToolResult, TemplateOutput, error) {
		entity, err := svc.Create(ctx, application.CreateTemplateCommand{
			ThemeID: input.ThemeID, Name: input.Name, Slug: input.Slug,
		})
		if err != nil {
			return nil, TemplateOutput{}, err
		}
		return nil, toTemplateOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "template_get",
		Description: "Get a template by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetTemplateInput) (*mcp.CallToolResult, TemplateOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, TemplateOutput{}, err
		}
		return nil, toTemplateOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "template_list",
		Description: "List all templates with cursor-based pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListTemplatesInput) (*mcp.CallToolResult, ListTemplatesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.Cursor, limit)
		if err != nil {
			return nil, ListTemplatesOutput{}, err
		}
		out := ListTemplatesOutput{
			Data:    make([]TemplateOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toTemplateOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "template_list_by_theme",
		Description: "List templates belonging to a specific theme.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListTemplatesByThemeInput) (*mcp.CallToolResult, ListTemplatesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.ListByThemeID(ctx, input.ThemeID, input.Cursor, limit)
		if err != nil {
			return nil, ListTemplatesOutput{}, err
		}
		out := ListTemplatesOutput{
			Data:    make([]TemplateOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toTemplateOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "template_update",
		Description: "Update an existing template.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateTemplateInput) (*mcp.CallToolResult, TemplateOutput, error) {
		entity, err := svc.Update(ctx, application.UpdateTemplateCommand{
			ID: input.ID, ThemeID: input.ThemeID, Name: input.Name,
			Slug: input.Slug, Description: input.Description,
			FilePath: input.FilePath, Status: input.Status,
		})
		if err != nil {
			return nil, TemplateOutput{}, err
		}
		return nil, toTemplateOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "template_delete",
		Description: "Delete a template by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteTemplateInput) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteTemplateCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
