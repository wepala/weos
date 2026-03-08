package mcp

import (
	"context"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreatePageInput struct {
	WebsiteID string `json:"website_id" jsonschema:"parent website ID (URN)"`
	Name      string `json:"name" jsonschema:"page display name"`
	Slug      string `json:"slug,omitempty" jsonschema:"URL-friendly identifier (auto-generated from name if omitted)"`
}

type UpdatePageInput struct {
	ID          string `json:"id" jsonschema:"page ID (URN)"`
	WebsiteID   string `json:"website_id,omitempty" jsonschema:"parent website ID"`
	Name        string `json:"name" jsonschema:"page display name"`
	Slug        string `json:"slug,omitempty" jsonschema:"URL slug"`
	Description string `json:"description,omitempty" jsonschema:"page description"`
	Template    string `json:"template,omitempty" jsonschema:"template identifier"`
	Position    int    `json:"position,omitempty" jsonschema:"sort position"`
	Status      string `json:"status,omitempty" jsonschema:"status (draft or published)"`
}

type DeletePageInput struct {
	ID string `json:"id" jsonschema:"page ID (URN)"`
}

type GetPageInput struct {
	ID string `json:"id" jsonschema:"page ID (URN)"`
}

type ListPagesInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type ListPagesByWebsiteInput struct {
	WebsiteID string `json:"website_id" jsonschema:"parent website ID (URN)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"pagination cursor"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type PageOutput struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	Template    string    `json:"template,omitempty"`
	Position    int       `json:"position"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListPagesOutput struct {
	Data    []PageOutput `json:"data"`
	Cursor  string       `json:"cursor,omitempty"`
	HasMore bool         `json:"has_more"`
}

func toPageOutput(e *entities.Page) PageOutput {
	return PageOutput{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		Template:    e.Template(),
		Position:    e.Position(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt(),
	}
}

func registerPageTools(server *mcp.Server, svc application.PageService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "page_create",
		Description: "Create a new page within a website.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreatePageInput) (*mcp.CallToolResult, PageOutput, error) {
		entity, err := svc.Create(ctx, application.CreatePageCommand{
			WebsiteID: input.WebsiteID, Name: input.Name, Slug: input.Slug,
		})
		if err != nil {
			return nil, PageOutput{}, err
		}
		return nil, toPageOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "page_get",
		Description: "Get a page by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetPageInput) (*mcp.CallToolResult, PageOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, PageOutput{}, err
		}
		return nil, toPageOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "page_list",
		Description: "List all pages with cursor-based pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPagesInput) (*mcp.CallToolResult, ListPagesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.Cursor, limit)
		if err != nil {
			return nil, ListPagesOutput{}, err
		}
		out := ListPagesOutput{
			Data:    make([]PageOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toPageOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "page_list_by_website",
		Description: "List pages belonging to a specific website.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPagesByWebsiteInput) (*mcp.CallToolResult, ListPagesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.ListByWebsiteID(ctx, input.WebsiteID, input.Cursor, limit)
		if err != nil {
			return nil, ListPagesOutput{}, err
		}
		out := ListPagesOutput{
			Data:    make([]PageOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toPageOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "page_update",
		Description: "Update an existing page.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdatePageInput) (*mcp.CallToolResult, PageOutput, error) {
		entity, err := svc.Update(ctx, application.UpdatePageCommand{
			ID: input.ID, WebsiteID: input.WebsiteID, Name: input.Name,
			Slug: input.Slug, Description: input.Description,
			Template: input.Template, Position: input.Position, Status: input.Status,
		})
		if err != nil {
			return nil, PageOutput{}, err
		}
		return nil, toPageOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "page_delete",
		Description: "Delete a page by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeletePageInput) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeletePageCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
