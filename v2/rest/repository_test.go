package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"testing"
)

func TestResourceRepository_AddSubscriber(t *testing.T) {
	t.Run("add subscriber for event type only", func(t *testing.T) {
		resourceRepository := new(rest.ResourceRepository)
		err := resourceRepository.AddSubscriber(rest.EventHandlerConfig{
			Type: "create",
			Handler: func(ctx context.Context, logger rest.Log, event rest.Event) error {
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		handlers := resourceRepository.GetSubscribers("")
		if len(handlers) != 1 {
			t.Errorf("expected 1 handler, got %d", len(handlers))
		}
		if handler, ok := handlers["create"]; !ok {
			t.Errorf("expected handler for create event type")
		} else {
			if handler == nil {
				t.Errorf("expected handler for create event type")
			}
		}
	})
	t.Run("add subscriber for resource type and event", func(t *testing.T) {
		resourceRepository := new(rest.ResourceRepository)
		err := resourceRepository.AddSubscriber(rest.EventHandlerConfig{
			ResourceType: "Article",
			Type:         "create",
			Handler: func(ctx context.Context, logger rest.Log, event rest.Event) error {
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		handlers := resourceRepository.GetSubscribers("Article")
		if len(handlers) != 1 {
			t.Errorf("expected 1 handler, got %d", len(handlers))
		}
		if handler, ok := handlers["create"]; !ok {
			t.Errorf("expected handler for create event type")
		} else {
			if handler == nil {
				t.Errorf("expected handler for create event type")
			}
		}
	})
	t.Run("adding subscriber without handler should throw error", func(t *testing.T) {
		resourceRepository := new(rest.ResourceRepository)
		err := resourceRepository.AddSubscriber(rest.EventHandlerConfig{
			Type: "create",
		})
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})
}

func TestResourceRepository_Dispatch(t *testing.T) {
	logger := &LogMock{
		DebugfFunc: func(format string, args ...interface{}) {

		},
		DebugFunc: func(args ...interface{}) {

		},
		ErrorfFunc: func(format string, args ...interface{}) {

		},
		ErrorFunc: func(args ...interface{}) {

		},
	}
	t.Run("should trigger resource specific handler and generic event type handler", func(t *testing.T) {
		createHandlerHit := false
		articleCreateHandlerHit := false
		resourceRepository := new(rest.ResourceRepository)
		err := resourceRepository.AddSubscriber(rest.EventHandlerConfig{
			Type: "create",
			Handler: func(ctx context.Context, logger rest.Log, event rest.Event) error {
				createHandlerHit = true
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		err = resourceRepository.AddSubscriber(rest.EventHandlerConfig{
			Type:         "create",
			ResourceType: "Article",
			Handler: func(ctx context.Context, logger rest.Log, event rest.Event) error {
				articleCreateHandlerHit = true
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		errors := resourceRepository.Dispatch(context.Background(), rest.Event{
			Type: "create",
			Meta: rest.EventMeta{
				ResourceType: "Article",
			},
		}, logger)
		if len(errors) != 0 {
			t.Errorf("expected no errors, got %d", len(errors))
		}
		if !createHandlerHit {
			t.Errorf("expected create handler to be hit")
		}
		if !articleCreateHandlerHit {
			t.Errorf("expected article create handler to be hit")
		}
	})
}

func TestResourceRepository_Persist(t *testing.T) {
	var blogSchema *openapi3.SchemaRef
	var ok bool
	logger := &LogMock{
		DebugfFunc: func(format string, args ...interface{}) {

		},
		DebugFunc: func(args ...interface{}) {

		},
		ErrorfFunc: func(format string, args ...interface{}) {

		},
		ErrorFunc: func(args ...interface{}) {

		},
	}
	schema, err := openapi3.NewLoader().LoadFromFile("fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("error encountered loading schema '%s'", err)
	}
	if blogSchema, ok = schema.Components.Schemas["Blog"]; !ok {
		t.Fatalf("expected schema to have Blog, got %v", schema.Components.Schemas)

	}
	t.Run("should trigger Blog create event", func(t *testing.T) {
		createBlogHandlerHit := false

		resource, err := new(rest.BasicResource).FromSchema("", blogSchema.Value, []byte(`{"title": "test"}`))

		resourceRepository := new(rest.ResourceRepository)
		err = resourceRepository.AddSubscriber(rest.EventHandlerConfig{
			Type:         "create",
			ResourceType: "http://schema.org/Blog",
			Handler: func(ctx context.Context, logger rest.Log, event rest.Event) error {
				createBlogHandlerHit = true
				return nil
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		errs := resourceRepository.Persist(context.Background(), logger, []rest.Resource{resource})
		if len(errs) > 0 {
			t.Errorf("expected no error, got %v", errs)
		}
		if !createBlogHandlerHit {
			t.Errorf("expected create handler to be hit")
		}
	})
}
