package model_test

import (
	"encoding/json"
	"github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"testing"
)

func TestCreateContentType(t *testing.T) {
	commandDispatcher := &model.DefaultCommandDispatcher{}
	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
		AddSubscriberFunc: func(handler model.EventHandler) {
		},
	}
	application := &ApplicationMock{
		DispatcherFunc: func() model.Dispatcher {
			return commandDispatcher
		},
		EventRepositoryFunc: func() model.EventRepository {
			return mockEventRepository
		},
		ProjectionsFunc: func() []model.Projection {
			return []model.Projection{}
		},
	}

	err1 := model.Initialize(application)
	if err1 != nil {
		t.Fatalf("unexpected error setting up model '%s'", err1)
	}

	t.Run("Testing basic create entity", func(t *testing.T) {
		input1 := model.AmorphousEntity{
			BasicEntity: &model.BasicEntity{ID: "123"},
			Properties:  map[string]model.Property{},
		}
		entityType := "Testing"
		prop := &model.StringProperty{
			BasicProperty: model.BasicProperty{
				Type:       "string",
				Label:      "title",
				Value:      "Testing Title of 1st Property",
				IsRequired: false,
			},
			Value: "Testing Title of 1st Property",
		}
		input1.Set(prop)
		reqBytes, err := json.Marshal(input1)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(context.TODO(), model.Create(context.TODO(), reqBytes, entityType))
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 1 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 1, len(mockEventRepository.PersistCalls()))
		}
	})
}
