package mcp

import (
	"context"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreatePersonInput struct {
	GivenName  string `json:"given_name" jsonschema:"first name"`
	FamilyName string `json:"family_name" jsonschema:"last name"`
	Email      string `json:"email" jsonschema:"email address"`
}

type UpdatePersonInput struct {
	ID         string `json:"id" jsonschema:"person ID (URN)"`
	GivenName  string `json:"given_name" jsonschema:"first name"`
	FamilyName string `json:"family_name" jsonschema:"last name"`
	Email      string `json:"email,omitempty" jsonschema:"email address"`
	AvatarURL  string `json:"avatar_url,omitempty" jsonschema:"avatar image URL"`
	Status     string `json:"status,omitempty" jsonschema:"status (active or archived)"`
}

type DeletePersonInput struct {
	ID string `json:"id" jsonschema:"person ID (URN)"`
}

type GetPersonInput struct {
	ID string `json:"id" jsonschema:"person ID (URN)"`
}

type ListPersonsInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
}

type PersonOutput struct {
	ID         string    `json:"id"`
	GivenName  string    `json:"given_name"`
	FamilyName string    `json:"family_name"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	AvatarURL  string    `json:"avatar_url,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type ListPersonsOutput struct {
	Data    []PersonOutput `json:"data"`
	Cursor  string         `json:"cursor,omitempty"`
	HasMore bool           `json:"has_more"`
}

func toPersonOutput(e *entities.Person) PersonOutput {
	return PersonOutput{
		ID:         e.GetID(),
		GivenName:  e.GivenName(),
		FamilyName: e.FamilyName(),
		Name:       e.Name(),
		Email:      e.Email(),
		AvatarURL:  e.AvatarURL(),
		Status:     e.Status(),
		CreatedAt:  e.CreatedAt(),
	}
}

func registerPersonTools(server *mcp.Server, svc application.PersonService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "person_create",
		Description: "Create a new person.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreatePersonInput) (*mcp.CallToolResult, PersonOutput, error) {
		entity, err := svc.Create(ctx, application.CreatePersonCommand{
			GivenName: input.GivenName, FamilyName: input.FamilyName, Email: input.Email,
		})
		if err != nil {
			return nil, PersonOutput{}, err
		}
		return nil, toPersonOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "person_get",
		Description: "Get a person by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetPersonInput) (*mcp.CallToolResult, PersonOutput, error) {
		entity, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, PersonOutput{}, err
		}
		return nil, toPersonOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "person_list",
		Description: "List all persons with cursor-based pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPersonsInput) (*mcp.CallToolResult, ListPersonsOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		result, err := svc.List(ctx, input.Cursor, limit)
		if err != nil {
			return nil, ListPersonsOutput{}, err
		}
		out := ListPersonsOutput{
			Data:    make([]PersonOutput, 0, len(result.Data)),
			Cursor:  result.Cursor,
			HasMore: result.HasMore,
		}
		for _, e := range result.Data {
			out.Data = append(out.Data, toPersonOutput(e))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "person_update",
		Description: "Update an existing person.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdatePersonInput) (*mcp.CallToolResult, PersonOutput, error) {
		entity, err := svc.Update(ctx, application.UpdatePersonCommand{
			ID: input.ID, GivenName: input.GivenName, FamilyName: input.FamilyName,
			Email: input.Email, AvatarURL: input.AvatarURL, Status: input.Status,
		})
		if err != nil {
			return nil, PersonOutput{}, err
		}
		return nil, toPersonOutput(entity), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "person_delete",
		Description: "Delete a person by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeletePersonInput) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeletePersonCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
