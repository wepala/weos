package rest

import (
	"net/url"
	"regexp"
	"strings"
)

// addFiltersToQueryParams adds filter parameters to query params in a generic way
// It takes a url.Values object and any struct containing Operator fields
// ...existing code...

// ParseFilterQueryParams converts query parameters with square bracket notation
// like "_filter[amount][gt]=100" into a nested map structure
// Result: {"_filter": {"amount": {"gt": "100"}}}
func ParseFilterQueryParams(queryParams url.Values) map[string]interface{} {
	result := make(map[string]interface{})

	// Regular expression to match bracket notation
	// This matches: base[key1][key2]...
	// Captures: base, key1, key2, ...
	re := regexp.MustCompile(`^([^\[]+)(?:\[([^\]]+)\])+$`)

	for key, values := range queryParams {
		if strings.Contains(key, "[") && strings.Contains(key, "]") {
			// This is a bracket notation parameter
			matches := re.FindStringSubmatch(key)
			if len(matches) < 2 {
				continue
			}

			// Extract all keys from the bracket notation
			// Format: base[key1][key2]...
			baseKey := matches[1]
			keys := extractKeysFromBrackets(key)

			// Skip if we couldn't extract any keys
			if len(keys) == 0 {
				continue
			}

			// Build the nested map structure
			buildNestedMap(result, baseKey, keys, values[0])
		}
	}

	return result
}

// extractKeysFromBrackets extracts keys from a string with bracket notation
// Example: "_filter[id][eq]" returns ["id", "eq"]
func extractKeysFromBrackets(s string) []string {
	re := regexp.MustCompile(`\[([^\]]+)\]`)
	matches := re.FindAllStringSubmatch(s, -1)

	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			result = append(result, match[1])
		}
	}

	return result
}

// buildNestedMap builds a nested map structure from a list of keys and a value
func buildNestedMap(root map[string]interface{}, baseKey string, keys []string, value string) {
	// Make sure the base key exists in the root map
	if _, exists := root[baseKey]; !exists {
		root[baseKey] = make(map[string]interface{})
	}

	// Cast to the right type
	current, ok := root[baseKey].(map[string]interface{})
	if !ok {
		// If it's not a map, we can't proceed
		return
	}

	// Navigate through the keys, building nested maps as needed
	for i, key := range keys {
		if i == len(keys)-1 {
			// Last key, set the value
			current[key] = value
		} else {
			// Not the last key, ensure the next level map exists
			if _, exists := current[key]; !exists {
				current[key] = make(map[string]interface{})
			}

			// Move to the next level
			next, ok := current[key].(map[string]interface{})
			if !ok {
				// If it's not a map, we can't proceed
				return
			}
			current = next
		}
	}
}

// ConvertQueryParamsToJSON converts all query parameters with bracket notation to a JSON-friendly structure
func ConvertQueryParamsToJSON(queryParams url.Values) map[string]interface{} {
	return ParseFilterQueryParams(queryParams)
}
