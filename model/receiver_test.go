package model_test

import (
	"encoding/json"
	"github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"testing"
)

type Blog struct {
	Id          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func TestCreateContentType(t *testing.T) {
	commandDispatcher := &model.DefaultCommandDispatcher{}
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
		mockBlog := &Blog{
			Id:    "123",
			Title: "Test Blog",
		}
		entityType := "Blog"
		reqBytes, err := json.Marshal(mockBlog)
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
	t.Run("Testing basic batch create", func(t *testing.T) {
		mockBlog := &Blog{
			Id:    "123",
			Title: "Test Blog 1",
		}
		entityType := "Blog"
		mockBlog2 := &Blog{
			Id:          "1234",
			Title:       "Test Blog 2",
			Description: "Description 2",
		}
		blogs := []*Blog{mockBlog, mockBlog2}
		reqBytes, err := json.Marshal(blogs)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(context.TODO(), model.CreateBatch(context.TODO(), reqBytes, entityType))
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 2 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 2, len(mockEventRepository.PersistCalls()))
		}
	})

}
