package model_test

import (
	weos "github.com/wepala/weos-content-service/model"
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
