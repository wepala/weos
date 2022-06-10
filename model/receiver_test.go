package model_test

import (
	context3 "context"
	"encoding/json"
	"github.com/labstack/echo/v4"
	api "github.com/wepala/weos/controllers/rest"
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
	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromFile("../controllers/rest/fixtures/blog-x-schema.yaml")
	if err != nil {
		t.Fatalf("unexpected error occured '%s'", err)
	}
	ctx := context.Background()
	ctx1 := context.Background()
	ctx2 := context.Background()
	contentEntity := "Author"
	contentEntity1 := "Category"
	contentEntity2 := "Archives"
	builder := api.CreateSchema(ctx, echo.New(), swagger)
	entityFactory := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentEntity, swagger.Components.Schemas[contentEntity].Value, builder[contentEntity])
	ctx = context.WithValue(ctx, weosContext.ENTITY_FACTORY, entityFactory)
	ctx = context.WithValue(ctx, weosContext.USER_ID, "123")
	entityFactory1 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentEntity1, swagger.Components.Schemas[contentEntity1].Value, builder[contentEntity1])
	ctx1 = context.WithValue(ctx1, weosContext.ENTITY_FACTORY, entityFactory1)
	ctx1 = context.WithValue(ctx1, weosContext.USER_ID, "123")
	entityFactory2 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentEntity2, swagger.Components.Schemas[contentEntity2].Value, builder[contentEntity2])
	ctx2 = context.WithValue(ctx2, weosContext.ENTITY_FACTORY, entityFactory2)
	ctx2 = context.WithValue(ctx2, weosContext.USER_ID, "123")
	commandDispatcher := &model.DefaultCommandDispatcher{}
	commandDispatcher.AddSubscriber(model.Create(context.Background(), nil, contentEntity, ""), model.CreateHandler)
	commandDispatcher.AddSubscriber(model.CreateBatch(context.Background(), nil, contentEntity), model.CreateBatchHandler)
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
			payload := entities[0].(*model.Event).Payload
			entity1 := map[string]interface{}{}
			err = json.Unmarshal(payload, &entity1)
			if err != nil {
				t.Errorf("unexpect error unmarshalling payload in event: %s", err)
			}
			if entity1["id"] == nil {
				t.Errorf("Unexpected error expected to find id but got nil")
			}
			return nil
		},
	}
	projectionMock := &ProjectionMock{
		GetByPropertiesFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) ([]*model.ContentEntity, error) {
			return nil, nil
		},
	}

	t.Run("Testing basic create entity with a auto generating id ksuid", func(t *testing.T) {

		mockAuthor := map[string]interface{}{"firstName": "New ", "lastName": "New nEW"}
		reqBytes, err := json.Marshal(mockAuthor)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx, model.Create(ctx, reqBytes, contentEntity, "fsdf32432"), mockEventRepository, projectionMock, echo.New().Logger)
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 1 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 1, len(mockEventRepository.PersistCalls()))
		}
	})
	t.Run("Testing basic create entity with a auto generating id uuid", func(t *testing.T) {

		mockCategory := map[string]interface{}{"title": "New Blog"}
		reqBytes, err := json.Marshal(mockCategory)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx1, model.Create(ctx1, reqBytes, contentEntity1, "fsdf32432"), mockEventRepository, projectionMock, echo.New().Logger)
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 2 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 2, len(mockEventRepository.PersistCalls()))
		}
	})
	t.Run("Testing basic batch create where the id is specified but the format is not specified", func(t *testing.T) {

		mockArchives := [3]map[string]interface{}{
			{"title": "Blog 1"},
			{"title": "Blog 2"},
			{"title": "Blog 3"},
		}
		reqBytes, err := json.Marshal(mockArchives)
		if err != nil {
			t.Fatalf("error converting payload to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx2, model.CreateBatch(ctx2, reqBytes, contentEntity2), mockEventRepository, projectionMock, echo.New().Logger)
		if err1 == nil {
			t.Fatalf("expected error dispatching command but got nil")
		}

		if len(mockEventRepository.PersistCalls()) != 2 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 2, len(mockEventRepository.PersistCalls()))
		}
	})
}

func TestUpdateContentType(t *testing.T) {
	t.SkipNow()
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
	reqBytes, err := json.Marshal(existingPayload)
	existingBlog := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "dsafdsdfdsf",
			},
			SequenceNo: int64(0),
		},
	}
	event := model.NewEntityEvent("update", existingBlog, existingBlog.ID, existingPayload)
	existingBlog.NewChange(event)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return new(model.ContentEntity).Init(context.Background(), reqBytes)
		},
	}

	application := &ServiceMock{
		DispatcherFunc: func() model.CommandDispatcher {
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
		t.SkipNow()
		updatedPayload := map[string]interface{}{"weos_id": "dsafdsdfdsf", "title": "Update Blog", "description": "Update Description", "url": "www.Updated!.com"}
		entityType := "Blog"
		ctx = context.WithValue(ctx, context2.SEQUENCE_NO, 1)
		reqBytes, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("error converting content type to bytes %s", err)
		}

		err1 := commandDispatcher.Dispatch(ctx, model.Update(ctx, reqBytes, entityType), nil, nil, nil)
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
	ctx := context.Background()
	entityType := "Blog"
	mockEntityFactory := &EntityFactoryMock{
		NewEntityFunc: func(ctx context3.Context) (*model.ContentEntity, error) {
			return &model.ContentEntity{}, nil
		},
		NameFunc: func() string {
			return entityType
		},
		SchemaFunc: func() *openapi3.Schema {
			return swagger.Components.Schemas[entityType].Value
		},
	}
	ctx = context.WithValue(ctx, weosContext.ENTITY_FACTORY, mockEntityFactory)
	ctx = context.WithValue(ctx, weosContext.USER_ID, "123")

	commandDispatcher := &model.DefaultCommandDispatcher{}
	commandDispatcher.AddSubscriber(model.Delete(context.Background(), "", ""), model.DeleteHandler)
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
	reqBytes, err := json.Marshal(existingPayload)
	existingBlog := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "dsafdsdfdsf",
			},
			SequenceNo: int64(0),
		},
	}
	event := model.NewEntityEvent("delete", existingBlog, existingBlog.ID, existingPayload)
	existingBlog.NewChange(event)

	projectionMock := &ProjectionMock{
		GetContentEntityFunc: func(ctx context3.Context, entityFactory model.EntityFactory, weosID string) (*model.ContentEntity, error) {
			return existingBlog, nil
		},
		GetByKeyFunc: func(ctxt context3.Context, entityFactory model.EntityFactory, identifiers map[string]interface{}) (*model.ContentEntity, error) {
			return new(model.ContentEntity).Init(context.Background(), reqBytes)
		},
	}

	t.Run("Testing basic delete entity", func(t *testing.T) {
		err1 := commandDispatcher.Dispatch(ctx, model.Delete(ctx, entityType, "dsafdsdfdsf"), mockEventRepository, projectionMock, echo.New().Logger)
		if err1 != nil {
			t.Fatalf("unexpected error dispatching command '%s'", err1)
		}

		if len(mockEventRepository.PersistCalls()) != 1 {
			t.Fatalf("expected change events to be persisted '%d' got persisted '%d' times", 1, len(mockEventRepository.PersistCalls()))
		}
	})
}
