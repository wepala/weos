package mcp

import (
	"context"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DeletedOutput struct {
	Success bool `json:"success"`
}

type CreateWebsiteInput struct {
	Name string `json:"name" jsonschema:"display name for the website"`
	URL  string `json:"url" jsonschema:"primary URL of the website"`
	Slug string `json:"slug,omitempty" jsonschema:"URL-friendly identifier (auto-generated from name if omitted)"`
}

type UpdateWebsiteInput struct {
	ID          string `json:"id" jsonschema:"website ID (URN)"`
	Name        string `json:"name" jsonschema:"display name"`
	URL         string `json:"url,omitempty" jsonschema:"primary URL"`
	Description string `json:"description,omitempty" jsonschema:"short description"`
	Language    string `json:"language,omitempty" jsonschema:"ISO language code"`
	Status      string `json:"status,omitempty" jsonschema:"status (draft or published)"`
}

type DeleteWebsiteInput struct {
	ID string `json:"id" jsonschema:"website ID (URN)"`
}

type GetWebsiteInput struct {
	ID string `json:"id" jsonschema:"website ID (URN)"`
}

type ListWebsitesInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type WebsiteOutput struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	URL         string    `json:"url"`
	Description string    `json:"description,omitempty"`
	Language    string    `json:"language"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListWebsitesOutput struct {
	Data    []WebsiteOutput `json:"data"`
	Cursor  string          `json:"cursor,omitempty"`
	HasMore bool            `json:"has_more"`
}

func toWebsiteOutput(e *entities.Website) WebsiteOutput {
	return WebsiteOutput{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		URL:         e.URL(),
		Description: e.Description(),
		Language:    e.Language(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt(),
	}
}

func registerWebsiteTools(server *mcp.Server, svc application.WebsiteService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "website_create",
		Description: "Create a new website. Returns the created website with its ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateWebsiteInput) (*mcp.CallToolResult, WebsiteOutput, error) {
		entity, err := svc.Create(ctx, application.CreateWebsiteCommand{
			Name: input.Name, URL: input.URL, Slug: input.Slug,
		})
		if err != nil {
			return nil, WebsiteOutput{}, err
		}
		return nil, toWebsiteOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "website_get",
		Description: "Get a website by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetWebsiteInput) (*mcp.CallToolResult, WebsiteOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, WebsiteOutput{}, err
		}
		return nil, toWebsiteOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "website_list",
		Description: "List all websites with cursor-based pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListWebsitesInput) (*mcp.CallToolResult, ListWebsitesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.Cursor, limit)
		if err != nil {
			return nil, ListWebsitesOutput{}, err
		}
		out := ListWebsitesOutput{
			Data:    make([]WebsiteOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toWebsiteOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "website_update",
		Description: "Update an existing website.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateWebsiteInput) (*mcp.CallToolResult, WebsiteOutput, error) {
		entity, err := svc.Update(ctx, application.UpdateWebsiteCommand{
			ID: input.ID, Name: input.Name, URL: input.URL,
			Description: input.Description, Language: input.Language, Status: input.Status,
		})
		if err != nil {
			return nil, WebsiteOutput{}, err
		}
		return nil, toWebsiteOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "website_delete",
		Description: "Delete a website by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteWebsiteInput) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteWebsiteCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
