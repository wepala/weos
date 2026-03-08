package mcp

import (
	"context"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateSectionInput struct {
	PageID string `json:"page_id" jsonschema:"parent page ID (URN)"`
	Name   string `json:"name" jsonschema:"section display name"`
	Slot   string `json:"slot" jsonschema:"template slot to place the section in (e.g. hero.headline)"`
}

type UpdateSectionInput struct {
	ID         string `json:"id" jsonschema:"section ID (URN)"`
	PageID     string `json:"page_id,omitempty" jsonschema:"parent page ID"`
	Name       string `json:"name" jsonschema:"section display name"`
	Slot       string `json:"slot,omitempty" jsonschema:"template slot"`
	EntityType string `json:"entity_type,omitempty" jsonschema:"RDF entity type (e.g. schema:Product)"`
	Content    string `json:"content,omitempty" jsonschema:"section content (HTML or structured)"`
	Position   int    `json:"position,omitempty" jsonschema:"sort position"`
}

type DeleteSectionInput struct {
	ID string `json:"id" jsonschema:"section ID (URN)"`
}

type GetSectionInput struct {
	ID string `json:"id" jsonschema:"section ID (URN)"`
}

type ListSectionsInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type ListSectionsByPageInput struct {
	PageID string `json:"page_id" jsonschema:"parent page ID (URN)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type SectionOutput struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slot       string    `json:"slot"`
	EntityType string    `json:"entity_type,omitempty"`
	Content    string    `json:"content,omitempty"`
	Position   int       `json:"position"`
	CreatedAt  time.Time `json:"created_at"`
}

type ListSectionsOutput struct {
	Data    []SectionOutput `json:"data"`
	Cursor  string          `json:"cursor,omitempty"`
	HasMore bool            `json:"has_more"`
}

func toSectionOutput(e *entities.Section) SectionOutput {
	return SectionOutput{
		ID:         e.GetID(),
		Name:       e.Name(),
		Slot:       e.Slot(),
		EntityType: e.EntityType(),
		Content:    e.Content(),
		Position:   e.Position(),
		CreatedAt:  e.CreatedAt(),
	}
}

func registerSectionTools(server *mcp.Server, svc application.SectionService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "section_create",
		Description: "Create a new content section within a page.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateSectionInput) (*mcp.CallToolResult, SectionOutput, error) {
		entity, err := svc.Create(ctx, application.CreateSectionCommand{
			PageID: input.PageID, Name: input.Name, Slot: input.Slot,
		})
		if err != nil {
			return nil, SectionOutput{}, err
		}
		return nil, toSectionOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "section_get",
		Description: "Get a section by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetSectionInput) (*mcp.CallToolResult, SectionOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, SectionOutput{}, err
		}
		return nil, toSectionOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "section_list",
		Description: "List all sections with cursor-based pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSectionsInput) (*mcp.CallToolResult, ListSectionsOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.Cursor, limit)
		if err != nil {
			return nil, ListSectionsOutput{}, err
		}
		out := ListSectionsOutput{
			Data:    make([]SectionOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toSectionOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "section_list_by_page",
		Description: "List sections belonging to a specific page.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSectionsByPageInput) (*mcp.CallToolResult, ListSectionsOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.ListByPageID(ctx, input.PageID, input.Cursor, limit)
		if err != nil {
			return nil, ListSectionsOutput{}, err
		}
		out := ListSectionsOutput{
			Data:    make([]SectionOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toSectionOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "section_update",
		Description: "Update an existing section.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateSectionInput) (*mcp.CallToolResult, SectionOutput, error) {
		entity, err := svc.Update(ctx, application.UpdateSectionCommand{
			ID: input.ID, PageID: input.PageID, Name: input.Name,
			Slot: input.Slot, EntityType: input.EntityType,
			Content: input.Content, Position: input.Position,
		})
		if err != nil {
			return nil, SectionOutput{}, err
		}
		return nil, toSectionOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "section_delete",
		Description: "Delete a section by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteSectionInput) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteSectionCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
