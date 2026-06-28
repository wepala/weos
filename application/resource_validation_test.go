// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package application

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// objectSchema requires a JSON object with a string "name" and an optional
// "status" constrained to an enum.
const objectSchema = `{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "status": {"enum": ["active", "archived"]}
  },
  "required": ["name"]
}`

func TestValidateAgainstSchema_ValidObjectPasses(t *testing.T) {
	err := validateAgainstSchema(json.RawMessage(objectSchema),
		json.RawMessage(`{"name":"Summarize Inbox","status":"active"}`))
	if err != nil {
		t.Fatalf("expected valid data to pass, got: %v", err)
	}
}

func TestValidateAgainstSchema_EmptySchemaSkips(t *testing.T) {
	if err := validateAgainstSchema(nil, json.RawMessage(`"anything"`)); err != nil {
		t.Fatalf("expected no validation without a schema, got: %v", err)
	}
}

func TestValidateAgainstSchema_StringPayloadIsClearAndLeakFree(t *testing.T) {
	err := validateAgainstSchema(json.RawMessage(objectSchema), json.RawMessage(`"not an object"`))
	if err == nil {
		t.Fatal("expected a validation error for a string payload")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error must satisfy errors.Is(ErrValidation) so handlers map it to 4xx; got: %v", err)
	}

	msg := err.Error()
	// Clear + actionable: names the argument and the expected type.
	for _, want := range []string{"data", "object", "must"} {
		if !strings.Contains(strings.ToLower(msg), want) {
			t.Errorf("message should mention %q; got: %s", want, msg)
		}
	}
	// Regression guards for issue #381.
	for _, banned := range []string{"got string, want object", "file://", "schema.json", "/Users/"} {
		if strings.Contains(msg, banned) {
			t.Errorf("message leaked %q (issue #381); got: %s", banned, msg)
		}
	}
}

func TestValidateAgainstSchema_MissingRequiredNamesProperty(t *testing.T) {
	err := validateAgainstSchema(json.RawMessage(objectSchema), json.RawMessage(`{"status":"active"}`))
	if err == nil {
		t.Fatal("expected a validation error for missing required property")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got: %v", err)
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "name") || !strings.Contains(msg, "missing") {
		t.Errorf("expected the missing property to be named; got: %s", err.Error())
	}
}

func TestValidateAgainstSchema_EnumNamesAllowedValues(t *testing.T) {
	err := validateAgainstSchema(json.RawMessage(objectSchema),
		json.RawMessage(`{"name":"x","status":"bogus"}`))
	if err == nil {
		t.Fatal("expected a validation error for an out-of-enum value")
	}
	msg := err.Error()
	if !strings.Contains(msg, "data.status") {
		t.Errorf("expected the nested field path data.status; got: %s", msg)
	}
	if !strings.Contains(msg, "active") || !strings.Contains(msg, "archived") {
		t.Errorf("expected the allowed enum values to be listed; got: %s", msg)
	}
}

func TestValidateAgainstSchema_InvalidJSONData(t *testing.T) {
	err := validateAgainstSchema(json.RawMessage(objectSchema), json.RawMessage(`{not json`))
	if err == nil {
		t.Fatal("expected an error for malformed JSON data")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "valid json") {
		t.Errorf("expected a 'not valid JSON' message; got: %s", err.Error())
	}
}
