package model_test

import (
	weos "github.com/wepala/weos/model"
	"testing"
)

type Entity1 struct {
	weos.AggregateRoot
}

func TestGetEntityType(t *testing.T) {
	t.Run("basic struct", func(t *testing.T) {
		entityType1 := weos.GetType(Entity1{})
		if entityType1 != "Entity1" {
			t.Errorf("expected the type to be %s, got '%s'", "Entity1", entityType1)
		}
	})

	t.Run("struct with pointer", func(t *testing.T) {
		entityType2 := weos.GetType(&Entity1{})
		if entityType2 != "Entity1" {
			t.Errorf("expected the type to be %s, got '%s'", "Entity1", entityType2)
		}
	})

}

func TestSchemaFromPayload(t *testing.T) {
	t.Run("create schema with string property", func(t *testing.T) {
		payload := []byte(`{
 "name": "Sojourner"
}`)
		schema := weos.SchemaFromPayload(payload)
		if _, ok := schema.Properties["name"]; !ok {
			t.Fatalf("expected property '%s'", "name")
		}

		if schema.Properties["name"].Value == nil || schema.Properties["name"].Value.Type != "string" {
			t.Errorf("expected property '%s' to be '%s'", "name", "string")
		}
	})

	t.Run("create schema with boolean property", func(t *testing.T) {
		payload := []byte(`{
 "testing": true
}`)
		schema := weos.SchemaFromPayload(payload)
		if _, ok := schema.Properties["testing"]; !ok {
			t.Fatalf("expected property '%s'", "testing")
		}

		if schema.Properties["testing"].Value == nil || schema.Properties["testing"].Value.Type != "boolean" {
			t.Errorf("expected property '%s' to be '%s'", "name", "boolean")
		}
	})
}
