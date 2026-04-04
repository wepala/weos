package application

import (
	"encoding/json"

	"weos/domain/entities"
)

// ExtractResourceFields parses intrinsic properties from a Resource's JSON-LD data.
func ExtractResourceFields(r *entities.Resource) (map[string]any, error) {
	simplified, err := entities.SimplifyJSONLD(r.Data(), nil)
	if err != nil {
		simplified = r.Data()
	}
	var fields map[string]any
	if err := json.Unmarshal(simplified, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// StringField extracts a string value from a map, returning "" if missing.
func StringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
