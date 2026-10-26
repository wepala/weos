package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Data is map[string]any (not any): an `any` field infers an empty schema that marshals to
// `true`, which omits `"type":"object"` from the input schema — so MCP clients send `data` as a
// JSON string and the server then validates a string against the type's object schema and fails
// ("got string, want object"). map[string]any infers `{"type":"object", ...}`, so the client
// sends an object. (weos issue #382, input side.)
type CreateResourceInput struct {
	TypeSlug string         `json:"type_slug" jsonschema:"resource type slug"`
	Data     map[string]any `json:"data" jsonschema:"resource data as JSON object"`
}

type UpdateResourceInput struct {
	ID   string         `json:"id" jsonschema:"resource ID (URN)"`
	Data map[string]any `json:"data" jsonschema:"updated resource data as JSON object"`
}

type DeleteResourceInput struct {
	ID string `json:"id" jsonschema:"resource ID (URN)"`
}

type GetResourceInput struct {
	ID string `json:"id" jsonschema:"resource ID (URN)"`
}

type ListResourcesInput struct {
	TypeSlug  string            `json:"type_slug" jsonschema:"resource type slug"`
	Cursor    string            `json:"cursor,omitempty" jsonschema:"pagination cursor from previous call"`
	Limit     int               `json:"limit,omitempty" jsonschema:"max items (1-100) defaults to 20"`
	SortBy    string            `json:"sort_by,omitempty" jsonschema:"column to sort by (e.g. submittedAt, createdAt)"`
	SortOrder string            `json:"sort_order,omitempty" jsonschema:"sort order: asc or desc"`
	Filters   []ListFilterInput `json:"filters,omitempty" jsonschema:"conditions a resource must all meet to be listed"`
	Flat      bool              `json:"flat,omitempty" jsonschema:"answer with projection rows instead of JSON-LD documents"`
}

type ResourceOutput struct {
	ID        string    `json:"id"`
	TypeSlug  string    `json:"type_slug"`
	Data      any       `json:"data" jsonschema:"resource data as JSON-LD object"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ListResourcesOutput carries one of two shapes: Data holds JSON-LD documents,
// Rows holds flat projection rows. omitzero leaves out the one a call did not
// ask for while keeping an empty page as [] rather than dropping the key.
type ListResourcesOutput struct {
	Data    []ResourceOutput `json:"data,omitzero" jsonschema:"resources as JSON-LD documents"`
	Rows    []map[string]any `json:"rows,omitzero" jsonschema:"resources as flat projection rows, when flat was asked for"`
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

// resourceListDescription tells the model how to filter, because the tool's
// input schema cannot carry the rules a refused condition would teach it.
const resourceListDescription = "List resources of a given type with cursor-based pagination. " +
	"Optional `filters` narrow the listing: an array of {field, operator, value} conditions, all of " +
	"which a resource must meet. operator is one of eq, ne, gt, gte, lt, lte. field is one of the " +
	"type's schema properties or a standard column (id, status, createdAt, createdBy, accountId, " +
	"sequenceNo, updatedAt); when the schema declares a property with a standard column's name " +
	"(such as a task's status), the condition is on the property. A property that holds an array " +
	"or an object is not filterable, except a reference, where eq matches a resource that holds " +
	"the identifier. A reference with a standard column's name is not filterable. value is a " +
	"string, number or boolean. A condition the listing cannot apply " +
	"is refused with an error that lists the valid fields or operators. ne also matches a " +
	"resource that has no value for the field. A type with no JSON Schema has no property list, " +
	"so its fields are not checked. To read the next page of a filtered listing, send the cursor " +
	"together with the same filters again: the cursor does not carry them, and a cursor sent " +
	"without them continues the unfiltered listing. " +
	"Set `flat` to true to get each resource as its projection row under `rows` — the type's " +
	"columns and the standard columns, named in camelCase — instead of a JSON-LD document under " +
	"`data`. A row costs a fraction of a document's tokens, so prefer flat for a large result set " +
	"and for a counting or summing question; leave it off when you need a resource's full " +
	"document. filters, cursor and limit work the same with flat. In a row, a property whose name " +
	"a standard column shadows (such as a task's status) holds the property, as a condition on it " +
	"does. A type with no projection table answers with `data` even when flat is set."

func registerResourceTools(
	server *mcp.Server, svc application.ResourceService, types application.ResourceTypeService,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_create",
		Description: "Create a new resource of a given type. Data is validated against the type's JSON Schema if defined.",
		Annotations: annAdditive(false),
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
		Annotations: annReadOnly(),
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
		Description: resourceListDescription,
		Annotations: annReadOnly(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input ListResourcesInput,
	) (*mcp.CallToolResult, ListResourcesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		sort := repositories.SortOptions{SortBy: input.SortBy, SortOrder: input.SortOrder}
		var filters []repositories.FilterCondition
		if len(input.Filters) > 0 {
			var err error
			if filters, err = buildListFilters(ctx, types, input.TypeSlug, input.Filters); err != nil {
				return nil, ListResourcesOutput{}, err
			}
		}
		if input.Flat {
			out, err := listFlat(ctx, svc, types, input.TypeSlug, filters, input.Cursor, limit, sort)
			// A type with no projection table has no rows to return, so it
			// answers from the entity path, keeping the conditions — the same
			// fallback the REST listing makes. Any other failure is reported.
			if !errors.Is(err, repositories.ErrNoProjectionTable) {
				return nil, out, err
			}
		}
		out, err := listEntities(ctx, svc, input.TypeSlug, filters, input.Cursor, limit, sort)
		return nil, out, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_update",
		Description: "Update an existing resource. Data is re-validated against the type's JSON Schema.",
		Annotations: annDestructive(),
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
		Annotations: annDestructive(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input DeleteResourceInput,
	) (*mcp.CallToolResult, DeletedOutput, error) {
		if err := svc.Delete(ctx, application.DeleteResourceCommand{ID: input.ID}); err != nil {
			return nil, DeletedOutput{}, err
		}
		return nil, DeletedOutput{Success: true}, nil
	})
}
