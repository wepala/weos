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
	Url         string `json:"url"`
}

func TestCreateContentType(t *testing.T) {
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	contentType = "Blog"
	contentTypeSchema = swagger.Components.Schemas[contentType]
	ctx := context.Background()
	ctx = context.WithValue(ctx, weosContext.CONTENT_TYPE, &weosContext.ContentType{
		Name:   contentType,
		Schema: contentTypeSchema.Value,
	})
	ctx = context.WithValue(ctx, weosContext.USER_ID, "123")
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
			Url:   "ww.testingBlog.com",
		}
		entityType := "Blog"
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx, model.Create(ctx, reqBytes, entityType))
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
			Url:   "ww.testBlog.com",
		}
		entityType := "Blog"
		mockBlog2 := &Blog{
			Id:          "1234",
			Title:       "Test Blog 2",
			Description: "Description 2",
			Url:         "ww.testingBlog.com",
		}
		blogs := []*Blog{mockBlog, mockBlog2}
		reqBytes, err := json.Marshal(blogs)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx, model.CreateBatch(ctx, reqBytes, entityType))
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 3 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 3, len(mockEventRepository.PersistCalls()))
		}
	})

}
