package mcp

import (
	"context"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateThemeInput struct {
	Name string `json:"name" jsonschema:"theme display name"`
	Slug string `json:"slug" jsonschema:"URL-friendly identifier"`
}

type UpdateThemeInput struct {
	ID           string `json:"id" jsonschema:"theme ID (URN)"`
	Name         string `json:"name" jsonschema:"theme display name"`
	Slug         string `json:"slug,omitempty" jsonschema:"URL slug"`
	Description  string `json:"description,omitempty" jsonschema:"theme description"`
	Version      string `json:"version,omitempty" jsonschema:"version string"`
	ThumbnailURL string `json:"thumbnail_url,omitempty" jsonschema:"thumbnail image URL"`
	Status       string `json:"status,omitempty" jsonschema:"status (draft or published)"`
}

type DeleteThemeInput struct {
	ID string `json:"id" jsonschema:"theme ID (URN)"`
}

type GetThemeInput struct {
	ID string `json:"id" jsonschema:"theme ID (URN)"`
}

type ListThemesInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type ThemeOutput struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	Description  string    `json:"description,omitempty"`
	Version      string    `json:"version,omitempty"`
	ThumbnailURL string    `json:"thumbnail_url,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type ListThemesOutput struct {
	Data    []ThemeOutput `json:"data"`
	Cursor  string        `json:"cursor,omitempty"`
	HasMore bool          `json:"has_more"`
}

func toThemeOutput(e *entities.Theme) ThemeOutput {
	return ThemeOutput{
		ID:           e.GetID(),
		Name:         e.Name(),
		Slug:         e.Slug(),
		Description:  e.Description(),
		Version:      e.Version(),
		ThumbnailURL: e.ThumbnailURL(),
		Status:       e.Status(),
		CreatedAt:    e.CreatedAt(),
	}
}

func registerThemeTools(server *mcp.Server, svc application.ThemeService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "theme_create",
		Description: "Create a new theme.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateThemeInput) (*mcp.CallToolResult, ThemeOutput, error) {
		entity, err := svc.Create(ctx, application.CreateThemeCommand{
			Name: input.Name, Slug: input.Slug,
		})
		if err != nil {
			return nil, ThemeOutput{}, err
		}
		return nil, toThemeOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "theme_get",
		Description: "Get a theme by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetThemeInput) (*mcp.CallToolResult, ThemeOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, ThemeOutput{}, err
		}
		return nil, toThemeOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "theme_list",
		Description: "List all themes with cursor-based pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListThemesInput) (*mcp.CallToolResult, ListThemesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.Cursor, limit)
		if err != nil {
			return nil, ListThemesOutput{}, err
		}
		out := ListThemesOutput{
			Data:    make([]ThemeOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toThemeOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "theme_update",
		Description: "Update an existing theme.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateThemeInput) (*mcp.CallToolResult, ThemeOutput, error) {
		entity, err := svc.Update(ctx, application.UpdateThemeCommand{
			ID: input.ID, Name: input.Name, Slug: input.Slug,
			Description: input.Description, Version: input.Version,
			ThumbnailURL: input.ThumbnailURL, Status: input.Status,
		})
		if err != nil {
			return nil, ThemeOutput{}, err
		}
		return nil, toThemeOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "theme_delete",
		Description: "Delete a theme by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteThemeInput) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteThemeCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
