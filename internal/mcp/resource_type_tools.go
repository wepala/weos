package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/jsonld"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// rawJSON converts a free-form MCP input value (decoded from JSON) back into a
// json.RawMessage for a service command. A nil value yields a nil message, which
// the service treats as "field absent".
func rawJSON(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// valueOr returns v unless it is empty, in which case it returns fallback. Used
// for patch-style updates where an omitted string means "leave unchanged".
func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// rawContextAndSchema converts the free-form context and schema inputs into
// json.RawMessage for a service command, attributing any error to its field.
func rawContextAndSchema(contextVal, schemaVal any) (json.RawMessage, json.RawMessage, error) {
	contextRaw, err := rawJSON(contextVal)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid context: %w", err)
	}
	schemaRaw, err := rawJSON(schemaVal)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid schema: %w", err)
	}
	return contextRaw, schemaRaw, nil
}

// Context and Schema are typed `any` (not json.RawMessage): the MCP SDK infers
// json.RawMessage — a []byte — as a JSON array/null, so an object argument is
// rejected at input validation before the handler runs (issue #382). `any`
// infers as free-form JSON, which correctly accepts an object Schema and a
// JSON-LD Context that may be a string, array, or object. The handler marshals
// them back to json.RawMessage for the service command.
type CreateResourceTypeInput struct {
	Name        string `json:"name" jsonschema:"resource type display name"`
	Slug        string `json:"slug" jsonschema:"URL-friendly identifier"`
	Description string `json:"description,omitempty" jsonschema:"resource type description"`
	Context     any    `json:"context,omitempty" jsonschema:"JSON-LD context (string, array, or object)"`
	Schema      any    `json:"schema,omitempty" jsonschema:"JSON Schema for validation (object)"`
}

// On update only ID is required; every other field is optional and, when
// omitted, left unchanged (patch semantics applied in the handler).
type UpdateResourceTypeInput struct {
	ID          string `json:"id" jsonschema:"resource type ID (URN)"`
	Name        string `json:"name,omitempty" jsonschema:"resource type display name"`
	Slug        string `json:"slug,omitempty" jsonschema:"URL slug"`
	Description string `json:"description,omitempty" jsonschema:"resource type description"`
	Context     any    `json:"context,omitempty" jsonschema:"JSON-LD context (string, array, or object)"`
	Schema      any    `json:"schema,omitempty" jsonschema:"JSON Schema for validation (object)"`
	Status      string `json:"status,omitempty" jsonschema:"status (active or archived)"`
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

// Context and Schema are typed `any` for the same reason as the input structs:
// the MCP SDK validates structured output against the inferred schema, and a
// json.RawMessage field infers as a JSON array/null — so returning an object
// context/schema would fail output validation (issue #382). toResourceTypeOutput
// decodes the stored json.RawMessage into a value.
type ResourceTypeOutput struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	// A jsonschema description tag is required on these `any` fields: without it the inferred
	// schema is empty and marshals to the boolean schema `true`, which MCP clients reject as an
	// invalid property schema (dropping the whole tool list). With a tag it marshals to
	// {"description": ...} — a valid, permissive object schema that still accepts the polymorphic
	// stored value (string | array | object). (weos issue #382, output side.)
	Context     any       `json:"context,omitempty" jsonschema:"JSON-LD context (string, array, or object)"`
	Schema      any       `json:"schema,omitempty" jsonschema:"JSON Schema for validation (object)"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
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
		Context:     jsonValue(e.Context()),
		Schema:      jsonValue(e.Schema()),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt(),
	}
}

// jsonValue decodes a stored json.RawMessage into a value for structured MCP
// output. Empty input yields nil (the field is omitted). Stored context/schema
// is validated on write, so a decode failure here is not expected; if it ever
// happens we surface the raw JSON as a string rather than silently dropping it,
// so corrupted data is visible to the caller instead of hidden.
func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

func registerResourceTypeTools(server *mcp.Server, svc application.ResourceTypeService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_create",
		Description: "Create a new resource type with JSON-LD context and optional JSON Schema.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input CreateResourceTypeInput,
	) (*mcp.CallToolResult, ResourceTypeOutput, error) {
		contextRaw, schemaRaw, err := rawContextAndSchema(input.Context, input.Schema)
		if err != nil {
			return nil, ResourceTypeOutput{}, err
		}
		entity, err := svc.Create(ctx, application.CreateResourceTypeCommand{
			Name: input.Name, Slug: input.Slug, Description: input.Description,
			Context: contextRaw, Schema: schemaRaw,
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
		// Patch semantics: the service does a full replace, so load the current
		// type and keep any field the caller omits. Otherwise updating one field
		// (e.g. description) would clear the slug — which the service then
		// rejects as empty — or silently wipe the context/schema (issue #382 review).
		existing, err := svc.GetByID(ctx, input.ID)
		if err != nil {
			return nil, ResourceTypeOutput{}, err
		}
		cmd := application.UpdateResourceTypeCommand{
			ID:          input.ID,
			Name:        valueOr(input.Name, existing.Name()),
			Slug:        valueOr(input.Slug, existing.Slug()),
			Description: valueOr(input.Description, existing.Description()),
			Status:      valueOr(input.Status, existing.Status()),
			Context:     existing.Context(),
			Schema:      existing.Schema(),
		}
		if input.Context != nil {
			if cmd.Context, err = rawJSON(input.Context); err != nil {
				return nil, ResourceTypeOutput{}, fmt.Errorf("invalid context: %w", err)
			}
		}
		if input.Schema != nil {
			if cmd.Schema, err = rawJSON(input.Schema); err != nil {
				return nil, ResourceTypeOutput{}, fmt.Errorf("invalid schema: %w", err)
			}
		}
		entity, err := svc.Update(ctx, cmd)
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
