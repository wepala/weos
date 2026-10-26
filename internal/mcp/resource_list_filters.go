package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/utils"
)

// ListFilterInput is one condition on a resource_list call. Value is `any`
// because a client may send a JSON string, number or boolean; anything else is
// refused by toFilterValue.
type ListFilterInput struct {
	Field    string `json:"field" jsonschema:"a schema property of the type, or a standard column"`
	Operator string `json:"operator" jsonschema:"one of eq, ne, gt, gte, lt, lte"`
	Value    any    `json:"value" jsonschema:"the value to compare with: a string, number or boolean"`
}

// filterOperators are the operators the projection repository applies. The
// repository drops any other operator without a word, so the tool refuses it
// first. Keep this list equal to operatorMap in
// infrastructure/database/gorm/resource_repository.go.
var filterOperators = []string{"eq", "ne", "gt", "gte", "lt", "lte"}

// standardFilterFields are the columns every projection table carries, named
// as a client names them.
var standardFilterFields = []string{"id", "status", "createdAt", "createdBy", "accountId", "sequenceNo", "updatedAt"}

// projectionSkippedKeys are schema properties the projection never makes into
// columns. Keep this set equal to jsonLDKeys in
// infrastructure/database/gorm/projection_manager.go.
var projectionSkippedKeys = map[string]bool{"@id": true, "@type": true, "@context": true}

// standardColumns are the projection's own columns in snake_case — the names a
// schema property is compared with to find one a standard column shadows.
var standardColumns = map[string]bool{
	"id": true, "type_slug": true, "data": true, "status": true, "created_by": true,
	"account_id": true, "sequence_no": true, "created_at": true, "updated_at": true,
}

// buildListFilters turns a resource_list call's conditions into repository
// filters, or refuses the call. The repository skips a condition it cannot
// apply and returns the unfiltered page, which a model then reports as a
// confident wrong answer — so every condition is checked here, and the error
// names what would have been accepted so the next call can be corrected.
//
// A condition on a field the type's schema declares wins over a standard
// column of the same name: on a task, "status" is the task's own state, not
// the resource lifecycle. Such a condition is marked DocumentProperty, because
// the projection has no column for it. A type with no schema has nothing to
// check a field against, so its fields are not checked.
func buildListFilters(
	ctx context.Context, types application.ResourceTypeService, typeSlug string, in []ListFilterInput,
) ([]repositories.FilterCondition, error) {
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

	out := make([]repositories.FilterCondition, 0, len(in))
	for i, c := range in {
		if c.Field == "" {
			return nil, fmt.Errorf("filter validation failed: condition %d has no field", i+1)
		}
		if !isFilterOperator(c.Operator) {
			return nil, fmt.Errorf("filter validation failed: operator %q is not supported; supported operators: %s",
				c.Operator, strings.Join(filterOperators, ", "))
		}
		value, err := toFilterValue(c.Value)
		if err != nil {
			return nil, fmt.Errorf("filter validation failed: condition %d on %q: %w", i+1, c.Field, err)
		}
		cond := repositories.FilterCondition{Field: c.Field, Operator: c.Operator, Value: value}
		if props[c.Field] {
			cond.DocumentProperty = standardColumns[utils.CamelToSnake(c.Field)]
		} else if hasSchema && !isStandardFilterField(c.Field) {
			return nil, fmt.Errorf(
				"filter validation failed: field %q is not filterable on type %q; filterable fields: %s",
				c.Field, typeSlug, strings.Join(filterableFields(props), ", "))
		}
		out = append(out, cond)
	}
	return out, nil
}

// schemaProperties returns the schema's property names that the projection can
// filter on. hasSchema is false when the type declares no JSON Schema at all.
func schemaProperties(schema json.RawMessage) (props map[string]bool, hasSchema bool, err error) {
	trimmed := strings.TrimSpace(string(schema))
	if trimmed == "" || trimmed == "null" {
		return nil, false, nil
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, false, fmt.Errorf("schema is not valid JSON: %w", err)
	}
	props = make(map[string]bool, len(s.Properties))
	for name := range s.Properties {
		if !projectionSkippedKeys[name] {
			props[name] = true
		}
	}
	return props, true, nil
}

// filterableFields lists the schema properties in name order, then the
// standard columns a property does not already name.
func filterableFields(props map[string]bool) []string {
	fields := make([]string, 0, len(props)+len(standardFilterFields))
	for name := range props {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	for _, f := range standardFilterFields {
		if !props[f] {
			fields = append(fields, f)
		}
	}
	return fields
}

func isFilterOperator(op string) bool {
	for _, o := range filterOperators {
		if o == op {
			return true
		}
	}
	return false
}

func isStandardFilterField(field string) bool {
	for _, f := range standardFilterFields {
		if f == field {
			return true
		}
	}
	return false
}

// toFilterValue formats a JSON value as the string FilterCondition carries. A
// boolean becomes "1" or "0": that is what a boolean projection column holds
// in SQLite, and PostgreSQL reads both as a boolean.
func toFilterValue(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case json.Number:
		return val.String(), nil
	case bool:
		if val {
			return "1", nil
		}
		return "0", nil
	default:
		return "", fmt.Errorf("value must be a string, a number or a boolean")
	}
}
