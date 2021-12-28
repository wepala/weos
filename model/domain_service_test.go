package model_test

import (
	"encoding/json"
	model "github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"testing"
)

func TestDomainService_Create(t *testing.T) {

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			var event *model.Event
			var ok bool
			entities := entity.GetNewChanges()
			if len(entities) != 1 {
				t.Fatalf("expected %d event to be saved, got %d", 1, len(entities))
			}

			if event, ok = entities[0].(*model.Event); !ok {
				t.Fatalf("the entity is not an event")
			}

			if event.Type != "create" {
				t.Errorf("expected event to be '%s', got '%s'", "create", event.Type)
			}
			if event.Meta.EntityType == "" {
				t.Errorf("expected event to be '%s', got '%s'", "", event.Type)
			}

			return nil
		},
	}

	input := model.AmorphousEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{ID: "123"},
		},
		BasicEntity: &model.BasicEntity{ID: "123"},
		Properties:  map[string]model.Property{},
	}
	entityType := "testing"

	reqBytes, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("error converting content type to bytes %s", err)
	}

	dService := model.NewDomainService(context.Background(), mockEventRepository)
	contentType, err := dService.Create(context.Background(), reqBytes, entityType)

	if err != nil {
		t.Fatalf("unexpected error creating content type '%s'", err)
	}
	if contentType == nil {
		t.Fatal("expected content type to be returned")
	}
	//TODO check for the attributes in the content type
}
