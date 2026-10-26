package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/utils"
)

// ListFilterInput is one condition on a resource_list call. Value is `any`
// because a client may send a JSON string, number or boolean; anything else is
// refused by filterValue.
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
//
// A value is compared as the JSON type the schema declares for the property,
// else as the value's own type, so a number sent as "3" still matches a stored
// 3. A value the declared type cannot hold is refused.
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
		prop, declared := props[c.Field]
		value, valueType, err := filterValue(c.Value, prop.jsonType)
		if err != nil {
			return nil, fmt.Errorf("filter validation failed: condition %d on %q: %w", i+1, c.Field, err)
		}
		cond := repositories.FilterCondition{Field: c.Field, Operator: c.Operator, Value: value, ValueType: valueType}
		if declared {
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

// schemaProperty is what a condition needs to know about one schema property.
type schemaProperty struct {
	// jsonType is the declared JSON Schema type, or empty when the property
	// declares none, or more than one besides "null".
	jsonType string
}

// schemaProperties returns the schema's properties that the projection can
// filter on. hasSchema is false when the type declares no JSON Schema at all.
func schemaProperties(schema json.RawMessage) (props map[string]schemaProperty, hasSchema bool, err error) {
	trimmed := strings.TrimSpace(string(schema))
	if trimmed == "" || trimmed == "null" {
		return nil, false, nil
	}
	var s struct {
		Properties map[string]struct {
			Type json.RawMessage `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, false, fmt.Errorf("schema is not valid JSON: %w", err)
	}
	props = make(map[string]schemaProperty, len(s.Properties))
	for name, p := range s.Properties {
		if !projectionSkippedKeys[name] {
			props[name] = schemaProperty{jsonType: declaredType(p.Type)}
		}
	}
	return props, true, nil
}

// declaredType reads a JSON Schema "type", which is a name or a list of names.
// A list that names one type besides "null" declares that type.
func declaredType(raw json.RawMessage) string {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return name
	}
	var names []string
	if json.Unmarshal(raw, &names) != nil {
		return ""
	}
	declared := ""
	for _, n := range names {
		if n == "null" {
			continue
		}
		if declared != "" {
			return ""
		}
		declared = n
	}
	return declared
}

// filterableFields lists the schema properties in name order, then the
// standard columns a property does not already name.
func filterableFields(props map[string]schemaProperty) []string {
	fields := make([]string, 0, len(props)+len(standardFilterFields))
	for name := range props {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	for _, f := range standardFilterFields {
		if _, declared := props[f]; !declared {
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

// filterValue formats a condition's JSON value as the string FilterCondition
// carries, and names the JSON type it is compared as: the declared type of the
// property, else the value's own. A boolean compared as a boolean becomes "1"
// or "0": that is what a boolean projection column holds in SQLite, and
// PostgreSQL reads both as a boolean.
func filterValue(v any, declared string) (string, repositories.FilterValueType, error) {
	switch v.(type) {
	case string, float64, json.Number, bool:
	default:
		return "", "", fmt.Errorf("value must be a string, a number or a boolean")
	}
	switch declared {
	case "number", "integer":
		n, ok := numberOf(v)
		if !ok {
			return "", "", fmt.Errorf("value must be a number, because the property holds a number")
		}
		return strconv.FormatFloat(n, 'f', -1, 64), repositories.FilterValueNumber, nil
	case "boolean":
		b, ok := booleanOf(v)
		if !ok {
			return "", "", fmt.Errorf("value must be a boolean, because the property holds a boolean")
		}
		return boolDigit(b), repositories.FilterValueBoolean, nil
	}
	switch val := v.(type) {
	case string:
		return val, repositories.FilterValueString, nil
	case bool:
		if declared == "string" {
			return strconv.FormatBool(val), repositories.FilterValueString, nil
		}
		return boolDigit(val), repositories.FilterValueBoolean, nil
	default:
		// A float64 or a json.Number, which always reads as a number.
		n, _ := numberOf(v)
		if declared == "string" {
			return strconv.FormatFloat(n, 'f', -1, 64), repositories.FilterValueString, nil
		}
		return strconv.FormatFloat(n, 'f', -1, 64), repositories.FilterValueNumber, nil
	}
}

// numberOf reads a JSON number, or a string that spells a finite one.
func numberOf(v any) (float64, bool) {
	var n float64
	var err error
	switch val := v.(type) {
	case float64:
		return val, true
	case json.Number:
		n, err = val.Float64()
	case string:
		n, err = strconv.ParseFloat(strings.TrimSpace(val), 64)
	default:
		return 0, false
	}
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// booleanOf reads a JSON boolean, or the string "true" or "false".
func booleanOf(v any) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		switch strings.TrimSpace(val) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func boolDigit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
