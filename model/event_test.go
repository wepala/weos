package model_test

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	weos "github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"testing"
)

func TestNewBasicEvent(t *testing.T) {
	event, _ := weos.NewBasicEvent("TEST_EVENT", "1iNqlx5htN0oJ3viyfWkAofJX7k", "BaseAggregate", nil)
	if event.Type != "TEST_EVENT" {
		t.Errorf("expected event to be type '%s', got '%s'", "TEST_EVENT", event.Type)
	}

	if event.Meta.EntityID != "1iNqlx5htN0oJ3viyfWkAofJX7k" {
		t.Errorf("expected the entity id to be '%s', got'%s'", "1iNqlx5htN0oJ3viyfWkAofJX7k", event.Meta.EntityID)
	}

	if event.Meta.EntityType != "BaseAggregate" {
		t.Errorf("expected the entity to have entityType '%s', got '%s'", "BaseAggregate", event.Meta.EntityType)
	}
}

func TestEvent_IsValid(t *testing.T) {
	t.Run("valid event", func(t *testing.T) {
		event, _ := weos.NewBasicEvent("TEST_EVENT", "1iNqlx5htN0oJ3viyfWkAofJX7k", "BaseAggregate", nil)
		if !event.IsValid() {
			t.Errorf("expected the event to be valid")
		}
	})

	t.Run("no entity id, event invalid", func(t *testing.T) {
		event, _ := weos.NewBasicEvent("TEST_EVENT", "", "BaseAggregate", nil)
		if event.IsValid() {
			t.Fatalf("expected the event to be invalid")
		}

		if len(event.GetErrors()) == 0 {
			t.Errorf("expected the event to have errors")
		}
	})

	t.Run("no event id, event invalid", func(t *testing.T) {
		event := weos.Event{
			ID:      "",
			Type:    "Some Type",
			Payload: nil,
			Meta: weos.EventMeta{
				EntityID: "1iNqlx5htN0oJ3viyfWkAofJX7k",
			},
			Version: 1,
		}
		if event.IsValid() {
			t.Fatalf("expected the event to be invalid")
		}

		if len(event.GetErrors()) == 0 {
			t.Errorf("expected the event to have errors")
		}
	})

	t.Run("no version no, event invalid", func(t *testing.T) {
		event := weos.Event{
			ID:      "adfasdf",
			Type:    "Some Type",
			Payload: nil,
			Meta: weos.EventMeta{
				EntityID: "1iNqlx5htN0oJ3viyfWkAofJX7k",
			},
			Version: 0,
		}
		if event.IsValid() {
			t.Fatalf("expected the event to be invalid")
		}

		if len(event.GetErrors()) == 0 {
			t.Errorf("expected the event to have errors")
		}
	})

	t.Run("no type, event invalid", func(t *testing.T) {
		event := weos.Event{
			ID:      "adfasdf",
			Type:    "",
			Payload: nil,
			Meta: weos.EventMeta{
				EntityID: "1iNqlx5htN0oJ3viyfWkAofJX7k",
			},
			Version: 1,
		}
		if event.IsValid() {
			t.Fatalf("expected the event to be invalid")
		}

		if len(event.GetErrors()) == 0 {
			t.Errorf("expected the event to have errors")
		}
	})

}

type BaseAggregate struct {
	weos.AggregateRoot
}

func TestNewAggregateEvent(t *testing.T) {
	entity := &BaseAggregate{}
	entity.ID = "1iNqlx5htN0oJ3viyfWkAofJX7k"
	event := weos.NewAggregateEvent("TEST_EVENT", entity, nil)
	if event.Type != "TEST_EVENT" {
		t.Errorf("expected event to be type '%s', got '%s'", "TEST_EVENT", event.Type)
	}

	if event.Meta.EntityID != "1iNqlx5htN0oJ3viyfWkAofJX7k" {
		t.Errorf("expected the entity id to be '%s', got'%s'", "1iNqlx5htN0oJ3viyfWkAofJX7k", event.Meta.EntityID)
	}

	if event.Meta.EntityType != "BaseAggregate" {
		t.Errorf("expected the entity to have entityType '%s', got '%s'", "BaseAggregate", event.Meta.EntityType)
	}
}

func TestNewEntityEvent(t *testing.T) {
	t.Run("content entity with a name should use that name as the event type", func(t *testing.T) {
		//load open api spec
		swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
		if err != nil {
			t.Fatalf("unexpected error occured '%s'", err)
		}
		var contentType string
		contentType = "Blog"
		ctx := context.Background()
		blog := make(map[string]interface{})
		blog["title"] = "Test"
		payload, err := json.Marshal(blog)
		if err != nil {
			t.Fatalf("unexpected error marshalling payload '%s'", err)
		}

		entity, err := new(weos.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas[contentType].Value, payload)
		if err != nil {
			t.Fatalf("unexpected error instantiating content entity '%s'", err)
		}
		entity.Name = "Blog"
		event := weos.NewEntityEvent("TEST_EVENT", entity, "", nil)
		if event.Type != "TEST_EVENT" {
			t.Errorf("expected event to be type '%s', got '%s'", "TEST_EVENT", event.Type)
		}
		if event.Meta.EntityType != "Blog" {
			t.Errorf("expected the entity to have entityType '%s', got '%s'", "Blog", event.Meta.EntityType)
		}
	})
	t.Run("content entity without name should return ContentEntity as the type", func(t *testing.T) {
		//load open api spec
		swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
		if err != nil {
			t.Fatalf("unexpected error occured '%s'", err)
		}
		var contentType string
		contentType = "Blog"
		ctx := context.Background()
		blog := make(map[string]interface{})
		blog["title"] = "Test"
		payload, err := json.Marshal(blog)
		if err != nil {
			t.Fatalf("unexpected error marshalling payload '%s'", err)
		}

		entity, err := new(weos.ContentEntity).FromSchemaWithValues(ctx, swagger.Components.Schemas[contentType].Value, payload)
		if err != nil {
			t.Fatalf("unexpected error instantiating content entity '%s'", err)
		}
		entity.Name = "Blog"
		event := weos.NewEntityEvent("TEST_EVENT", entity, "", nil)
		if event.Type != "TEST_EVENT" {
			t.Errorf("expected event to be type '%s', got '%s'", "TEST_EVENT", event.Type)
		}
		if event.Meta.EntityType != "Blog" {
			t.Errorf("expected the entity to have entityType '%s', got '%s'", "Blog", event.Meta.EntityType)
		}
	})
}
