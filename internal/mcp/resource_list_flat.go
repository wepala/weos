package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// errFlatAnswersWithDocuments is returned by listFlat when the type's rows
// cannot carry its values, and the caller answers from the entity path.
var errFlatAnswersWithDocuments = errors.New("the type's flat rows cannot carry its values")

// listFlat answers a listing with flat projection rows. It returns an error
// wrapping repositories.ErrNoProjectionTable when the type has no projection
// table, or errFlatAnswersWithDocuments when its rows cannot carry its values,
// and the caller answers from the entity path instead.
//
// A schema property the projection keeps no column for — one whose name a
// standard column shadows, such as a task's status, or any property of a
// schema the projection reads no columns from — is read by a condition from
// the stored document, so the row reads it there too: documentProperties
// lists them, and the row carries each one's value from the document, or null
// when the document has none. The documents are read by the page's
// identifiers, one more query.
func listFlat(
	ctx context.Context, svc application.ResourceService, types application.ResourceTypeService,
	typeSlug string, filters []repositories.FilterCondition, cursor string, limit int,
	sort repositories.SortOptions,
) (ListResourcesOutput, error) {
	fromDocument, err := documentProperties(ctx, types, typeSlug)
	if err != nil {
		return ListResourcesOutput{}, err
	}
	var page repositories.PaginatedResponse[map[string]any]
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
	if len(rows) > 0 && len(fromDocument) > 0 {
		if err := overlayDocumentProperties(ctx, svc, typeSlug, fromDocument, rows); err != nil {
			return ListResourcesOutput{}, err
		}
	}
	return ListResourcesOutput{Rows: rows, Cursor: page.Cursor, HasMore: page.HasMore}, nil
}

// overlayDocumentProperties sets each named property on the rows from the
// resources' stored documents, read by the rows' identifiers rather than by
// running the listing again: a write that lands between the two reads can
// then change a value, but cannot move a row off the page. A row whose
// resource the read does not return — deleted in between — fails the call,
// because a null in its place would be a quiet wrong answer.
func overlayDocumentProperties(
	ctx context.Context, svc application.ResourceService, typeSlug string, names []string,
	rows []map[string]any,
) error {
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
		for _, name := range names {
			v, err := documentProperty(doc, name)
			if err != nil {
				return fmt.Errorf("resource %q: read %q from its stored document: %w", id, name, err)
			}
			row[name] = v
		}
	}
	return nil
}

// documentProperties returns, in name order, the schema properties of a type
// that the projection keeps no column for, which buildListFilters marks
// DocumentProperty: a property whose name a standard column shadows, and every
// property of a schema the projection reads no columns from. A property named
// id is left out: the row's id is its identity, which the cursor and every
// caller rely on.
//
// It returns errFlatAnswersWithDocuments when a row cannot carry the type's
// values: the type has no JSON Schema, so nothing names its properties, or a
// reference has no column, and the document keeps a reference as an edge that
// documentProperty does not read.
func documentProperties(
	ctx context.Context, types application.ResourceTypeService, typeSlug string,
) ([]string, error) {
	rt, err := types.GetBySlug(ctx, typeSlug)
	if err != nil {
		return nil, fmt.Errorf("resource type %q: %w", typeSlug, err)
	}
	if rt == nil {
		return nil, fmt.Errorf("resource type %q: %w", typeSlug, repositories.ErrNotFound)
	}
	props, hasSchema, err := schemaProperties(rt.Schema())
	if err != nil {
		return nil, fmt.Errorf("resource type %q: %w", typeSlug, err)
	}
	if !hasSchema {
		return nil, fmt.Errorf("resource type %q has no JSON Schema: %w", typeSlug, errFlatAnswersWithDocuments)
	}
	var names []string
	for name, p := range props {
		if p.column || name == "id" {
			continue
		}
		if p.reference {
			return nil, fmt.Errorf("resource type %q: reference %q has no projection column: %w",
				typeSlug, name, errFlatAnswersWithDocuments)
		}
		names = append(names, name)
	}
	sort.Strings(names)
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
