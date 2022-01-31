package model_test

import (
	context3 "context"
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	context2 "github.com/wepala/weos/context"
	weosContext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
)

type Blog struct {
	ID          string `json:"id"`
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
	application := &ServiceMock{
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
		entityType := "Blog"

		mockBlog := map[string]interface{}{"weos_id": "fsdf32432", "title": "New Blog", "description": "New Description", "url": "www.NewBlog.com"}
		reqBytes, err := json.Marshal(mockBlog)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx, model.Create(ctx, reqBytes, entityType, "fsdf32432"))
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 1 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 1, len(mockEventRepository.PersistCalls()))
		}
	})
	t.Run("Testing basic batch create", func(t *testing.T) {
		entityType := "Blog"

		mockBlogs := [3]map[string]interface{}{
			{"title": "Blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"},
			{"title": "Blog 2", "description": "Description testing 2", "url": "www.TestBlog2.com"},
			{"title": "Blog 3", "description": "Description testing 3", "url": "www.TestBlog3.com"},
		}
		reqBytes, err := json.Marshal(mockBlogs)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx, model.CreateBatch(ctx, reqBytes, entityType))
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 4 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 4, len(mockEventRepository.PersistCalls()))
		}
	})
}

func TestUpdateContentType(t *testing.T) {
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
			if len(entities) != 2 {
				t.Fatalf("expected %d event to be saved, got %d", 2, len(entities))
			}

			if event, ok = entities[0].(*model.Event); !ok {
				t.Fatalf("the entity is not an event")
			}

			if event.Type != "update" {
				t.Errorf("expected event to be '%s', got '%s'", "update", event.Type)
			}
			if event.Meta.EntityType == "" {
				t.Errorf("expected event to be '%s', got '%s'", "", event.Type)
			}

			return nil
		},
		AddSubscriberFunc: func(handler model.EventHandler) {
		},
	}

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	existingBlog := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "dsafdsdfdsf",
			},
			SequenceNo: int64(0),
		},
		Property: existingPayload,
	}
	event := model.NewEntityEvent("update", existingBlog, existingBlog.ID, existingPayload)
	existingBlog.NewChange(event)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, contentType weosContext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return existingPayload, nil
		},
	}

	application := &ServiceMock{
		DispatcherFunc: func() model.Dispatcher {
			return commandDispatcher
		},
		EventRepositoryFunc: func() model.EventRepository {
			return mockEventRepository
		},
		ProjectionsFunc: func() []model.Projection {
			return []model.Projection{projectionMock}
		},
	}

	err1 := model.Initialize(application)
	if err1 != nil {
		t.Fatalf("unexpected error setting up model '%s'", err1)
	}

	t.Run("Testing basic update entity", func(t *testing.T) {
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		entityType := "Blog"
		ctx = context.WithValue(ctx, context2.SEQUENCE_NO, 1)
		reqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx, model.Update(ctx, reqBytes, entityType))
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 1 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 1, len(mockEventRepository.PersistCalls()))
		}
	})
}

func TestDeleteContentType(t *testing.T) {
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
			if len(entities) != 2 {
				t.Fatalf("expected %d event to be saved, got %d", 2, len(entities))
			}

			if event, ok = entities[0].(*model.Event); !ok {
				t.Fatalf("the entity is not an event")
			}

			if event.Type != "delete" {
				t.Errorf("expected event to be '%s', got '%s'", "update", event.Type)
			}

			if event.Meta.EntityType == "" {
				t.Errorf("expected event to be '%s', got '%s'", "", event.Type)
			}

			return nil
		},
		AddSubscriberFunc: func(handler model.EventHandler) {
		},
	}

	existingPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "sequence_no": int64(1), "title": "blog 1", "description": "Description testing 1", "url": "www.TestBlog1.com"}
	existingBlog := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "dsafdsdfdsf",
			},
			SequenceNo: int64(0),
		},
		Property: existingPayload,
	}
	event := model.NewEntityEvent("delete", existingBlog, existingBlog.ID, existingPayload)
	existingBlog.NewChange(event)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, contentType weosContext.ContentType, identifiers map[string]interface{}) (map[string]interface{}, error) {
			return existingPayload, nil
		},
	}

	application := &ServiceMock{
		DispatcherFunc: func() model.Dispatcher {
			return commandDispatcher
		},
		EventRepositoryFunc: func() model.EventRepository {
			return mockEventRepository
		},
		ProjectionsFunc: func() []model.Projection {
			return []model.Projection{projectionMock}
		},
	}

	err1 := model.Initialize(application)
	if err1 != nil {
		t.Fatalf("unexpected error setting up model '%s'", err1)
	}

	t.Run("Testing basic delete entity", func(t *testing.T) {
		entityType := "Blog"
		err1 := commandDispatcher.Dispatch(ctx, model.Delete(ctx, nil, entityType, "dsafdsdfdsf"))
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 1 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 1, len(mockEventRepository.PersistCalls()))
		}
	})
}
