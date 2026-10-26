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
// document has none. The documents are read by the page's identifiers, one
// more query.
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
		if err := overlayShadowedProperties(ctx, svc, types, typeSlug, rows); err != nil {
			return ListResourcesOutput{}, err
		}
	}
	return ListResourcesOutput{Rows: rows, Cursor: page.Cursor, HasMore: page.HasMore}, nil
}

// overlayShadowedProperties sets each shadowed schema property on the rows
// from the resources' stored documents, read by the rows' identifiers rather
// than by running the listing again: a write that lands between the two reads
// can then change a value, but cannot move a row off the page. A row whose
// resource the read does not return — deleted in between — fails the call,
// because a null in its place would be a quiet wrong answer.
func overlayShadowedProperties(
	ctx context.Context, svc application.ResourceService, types application.ResourceTypeService,
	typeSlug string, rows []map[string]any,
) error {
	shadowed, err := shadowedProperties(ctx, types, typeSlug)
	if err != nil || len(shadowed) == 0 {
		return err
	}
	lister, ok := svc.(application.ResourceIDLister)
	if !ok {
		return fmt.Errorf("read the documents of the flat page: the resource service cannot read resources by id")
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = fmt.Sprint(row["id"])
	}
	found, err := lister.ListByIDs(ctx, typeSlug, ids)
	if err != nil {
		return fmt.Errorf("read the documents of the flat page: %w", err)
	}
	docs := make(map[string]json.RawMessage, len(found))
	for _, e := range found {
		docs[e.GetID()] = e.Data()
	}
	for i, row := range rows {
		id := ids[i]
		doc, ok := docs[id]
		if !ok {
			return fmt.Errorf("resource %q is on the flat page but its stored document was not found; "+
				"it may have been deleted during the listing, so list again", id)
		}
		for _, name := range shadowed {
			v, err := documentProperty(doc, name)
			if err != nil {
				return fmt.Errorf("resource %q: read %q from its stored document: %w", id, name, err)
			}
			row[name] = v
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
// document has no value for it, and an error when the document is not a JSON
// object: a null there would be a quiet wrong answer.
func documentProperty(doc json.RawMessage, name string) (any, error) {
	if len(doc) == 0 {
		return nil, nil
	}
	var d map[string]any
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, fmt.Errorf("the document is not a JSON object: %w", err)
	}
	if graph, ok := d["@graph"].([]any); ok && len(graph) > 0 {
		if node, ok := graph[0].(map[string]any); ok {
			if v, ok := node[name]; ok && v != nil {
				return v, nil
			}
		}
	}
	return d[name], nil
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
