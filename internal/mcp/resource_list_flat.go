package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/utils"
)

// listFlat answers a listing with flat projection rows. It returns an error
// wrapping repositories.ErrNoProjectionTable when the type has no projection
// table, and the caller answers from the entity path instead.
//
// A schema property whose name a standard column shadows — a task's status —
// has no projection column; the row's column of that name holds the standard
// value (the resource lifecycle). A condition on such a name reads the
// property, so the row does too: shadowedProperties lists them, and the row
// carries each one's value from the stored document, or null when the
// document has none. That costs one more read of the same page.
func listFlat(
	ctx context.Context, svc application.ResourceService, types application.ResourceTypeService,
	typeSlug string, filters []repositories.FilterCondition, cursor string, limit int,
	sort repositories.SortOptions,
) (ListResourcesOutput, error) {
	var (
		page repositories.PaginatedResponse[map[string]any]
		err  error
	)
	if len(filters) == 0 {
		page, err = svc.ListFlat(ctx, typeSlug, cursor, limit, sort)
	} else {
		page, err = svc.ListFlatWithFilters(ctx, typeSlug, filters, cursor, limit, sort)
	}
	if err != nil {
		return ListResourcesOutput{}, err
	}
	rows := page.Data
	if rows == nil {
		rows = []map[string]any{}
	}
	if len(rows) > 0 {
		if err := overlayShadowedProperties(ctx, svc, types, typeSlug, filters, cursor, limit, sort, rows); err != nil {
			return ListResourcesOutput{}, err
		}
	}
	return ListResourcesOutput{Rows: rows, Cursor: page.Cursor, HasMore: page.HasMore}, nil
}

// overlayShadowedProperties sets each shadowed schema property on the rows
// from the resources' stored documents. The documents come from the entity
// listing of the same page — the same table, conditions, order, cursor and
// limit — and are matched to the rows by identifier, so a row whose document
// is not on that page (a write landed between the two reads) carries null
// rather than another resource's value.
func overlayShadowedProperties(
	ctx context.Context, svc application.ResourceService, types application.ResourceTypeService,
	typeSlug string, filters []repositories.FilterCondition, cursor string, limit int,
	sort repositories.SortOptions, rows []map[string]any,
) error {
	shadowed, err := shadowedProperties(ctx, types, typeSlug)
	if err != nil || len(shadowed) == 0 {
		return err
	}
	page, err := svc.ListWithFilters(ctx, typeSlug, filters, cursor, limit, sort)
	if err != nil {
		return fmt.Errorf("read the documents of the flat page: %w", err)
	}
	docs := make(map[string]json.RawMessage, len(page.Data))
	for _, e := range page.Data {
		docs[e.GetID()] = e.Data()
	}
	for _, row := range rows {
		doc := docs[fmt.Sprint(row["id"])]
		for _, name := range shadowed {
			row[name] = documentProperty(doc, name)
		}
	}
	return nil
}

// shadowedProperties returns the schema properties of a type whose names a
// standard column shadows, which buildListFilters marks DocumentProperty. A
// property named id is left out: the row's id is its identity, which the
// cursor and every caller rely on.
func shadowedProperties(
	ctx context.Context, types application.ResourceTypeService, typeSlug string,
) ([]string, error) {
	rt, err := types.GetBySlug(ctx, typeSlug)
	if err != nil {
		return nil, fmt.Errorf("resource type %q: %w", typeSlug, err)
	}
	if rt == nil {
		return nil, fmt.Errorf("resource type %q: %w", typeSlug, repositories.ErrNotFound)
	}
	props, _, err := schemaProperties(rt.Schema())
	if err != nil {
		return nil, fmt.Errorf("resource type %q: %w", typeSlug, err)
	}
	var names []string
	for name := range props {
		if col := utils.CamelToSnake(name); standardColumns[col] && col != "id" {
			names = append(names, name)
		}
	}
	return names, nil
}

// documentProperty reads a property from a stored JSON-LD document: from the
// entity node of an @graph or, for a legacy flat record, from the top level —
// the same places a DocumentProperty condition reads. It returns nil when the
// document has no value for it.
func documentProperty(doc json.RawMessage, name string) any {
	if len(doc) == 0 {
		return nil
	}
	var d map[string]any
	if json.Unmarshal(doc, &d) != nil {
		return nil
	}
	if graph, ok := d["@graph"].([]any); ok && len(graph) > 0 {
		if node, ok := graph[0].(map[string]any); ok {
			if v, ok := node[name]; ok && v != nil {
				return v
			}
		}
	}
	return d[name]
}

// listEntities answers a listing with JSON-LD documents.
func listEntities(
	ctx context.Context, svc application.ResourceService, typeSlug string,
	filters []repositories.FilterCondition, cursor string, limit int, sort repositories.SortOptions,
) (ListResourcesOutput, error) {
	var (
		page repositories.PaginatedResponse[*entities.Resource]
		err  error
	)
	if len(filters) == 0 {
		page, err = svc.List(ctx, typeSlug, cursor, limit, sort)
	} else {
		page, err = svc.ListWithFilters(ctx, typeSlug, filters, cursor, limit, sort)
	}
	if err != nil {
		return ListResourcesOutput{}, err
	}
	out := ListResourcesOutput{
		Data:    make([]ResourceOutput, 0, len(page.Data)),
		Cursor:  page.Cursor,
		HasMore: page.HasMore,
	}
	for _, e := range page.Data {
		out.Data = append(out.Data, toResourceOutput(e))
	}
	return out, nil
}
