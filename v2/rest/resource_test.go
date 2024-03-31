package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos/v2/rest"
	"testing"
)

func TestBasicResource_FromSchema(t *testing.T) {
	schema, err := openapi3.NewLoader().LoadFromFile("fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error encountered loading schema '%s'", err)
	}
	t.Run("create a simple resource", func(t *testing.T) {
		resource, err := new(rest.BasicResource).FromBytes(schema, []byte(`{
        "@id": "http://example.com/resource/1",
		"@type": "http://schema.org/Blog",
		"title": "test"
}`))
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		if resource == nil {
			t.Fatalf("expected resource to be created")
		}
		// check that the expected events were created
		events := resource.GetNewChanges()
		if len(events) != 1 {
			t.Fatalf("expected 1 event to be created, got %d", len(events))
		}
		var event *rest.Event
		var ok bool
		if event, ok = events[0].(*rest.Event); !ok {
			t.Fatalf("expected event to be of type Event")
		}
		if event.Type != "create" {
			t.Errorf("expected event type to be create, got %s", event.Type)
		}
		if event.Meta.ResourceType != "http://schema.org/Blog" {
			t.Errorf("expected event resource type to be http://schema.org/Blog, got %s", event.Meta.ResourceType)
		}
		if event.Meta.ResourceID != "http://example.com/resource/1" {
			t.Errorf("expected event resource id to be http://example.com/resource/1, got %s", event.Meta.ResourceID)
		}
	})
	t.Run("resource type not specified should return an error", func(t *testing.T) {
		_, err := new(rest.BasicResource).FromBytes(schema, []byte(`{
        "@id": "http://example.com/resource/1",
		"title": "test"
}`))
		if err == nil {
			t.Fatalf("expected error, got %s", err)
		}
	})
}
