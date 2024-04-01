package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"testing"
)

func TestResourceRepository_Initialize(t *testing.T) {
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
	t.Run("create new resource if one doesn't already exist", func(t *testing.T) {
		defaultProjection := &ProjectionMock{
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				return nil, nil
			},
			GetEventHandlersFunc: func() []rest.EventHandlerConfig {
				return nil
			},
		}
		eventStore := &EventStoreMock{
			AddSubscriberFunc: func(config rest.EventHandlerConfig) error {
				return nil
			},
		}
		params := rest.ResourceRepositoryParams{
			EventStore:        eventStore,
			DefaultProjection: defaultProjection,
			Config:            schema,
		}
		result, err := rest.NewResourceRepository(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		resourceRepository := result.Repository
		resource, err := resourceRepository.Initialize(context.Background(), logger, []byte(`{
			"@id": "/blogs/test",
			"@type": "http://schema.org/Blog",
			"title": "test"
		}`))
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		if resource == nil {
			t.Fatalf("expected resource to be created")
		}
		if resource.GetID() != "/blogs/test" {
			t.Errorf("expected resource id to be '/blogs/test', got %s", resource.GetID())
		}
	})
	t.Run("use the resource projection if it's available", func(t *testing.T) {
		defaultProjection := &ProjectionMock{
			GetByURIFunc: func(ctxt context.Context, logger rest.Log, uri string) (rest.Resource, error) {
				resource := &rest.BasicResource{
					Metadata: rest.ResourceMetadata{
						ID:         "/blogs/test",
						SequenceNo: 2,
						Type:       "http://schema.org/Blog",
						Version:    2,
						UserID:     "",
						AccountID:  "",
					},
				}
				return resource, nil
			},
			GetEventHandlersFunc: func() []rest.EventHandlerConfig {
				return nil
			},
		}
		eventStore := &EventStoreMock{
			AddSubscriberFunc: func(config rest.EventHandlerConfig) error {
				return nil
			},
		}
		params := rest.ResourceRepositoryParams{
			EventStore:        eventStore,
			DefaultProjection: defaultProjection,
			Config:            schema,
		}
		result, err := rest.NewResourceRepository(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		resourceRepository := result.Repository
		resource, err := resourceRepository.Initialize(context.Background(), logger, []byte(`{
			"@id": "/blogs/test",
			"@type": "http://schema.org/Blog",
			"title": "New Title"
		}`))
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		if resource == nil {
			t.Fatalf("expected resource to be created")
		}
		if resource.GetID() != "/blogs/test" {
			t.Errorf("expected resource id to be '/blogs/test', got %s", resource.GetID())
		}

		if resource.GetSequenceNo() != 3 {
			t.Errorf("expected sequence number to be 3, got %d", resource.GetSequenceNo())
		}
		if basicResource, ok := resource.(*rest.BasicResource); !ok || basicResource.GetString("title") != "New Title" {
			t.Errorf("expected title to be 'New Title', got %s", basicResource.GetString("title"))
		}
	})
}

func TestResourceRepository_Persist(t *testing.T) {

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
	t.Run("should trigger Blog create event", func(t *testing.T) {
		createBlogHandlerHit := false

		resource, err := new(rest.BasicResource).FromBytes(schema, []byte(`{
    	"@id": "/blogs/test",
		"@type": "http://schema.org/Blog",
		"title": "test"
}`))
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}

		eventDispatcher := &EventStoreMock{
			AddSubscriberFunc: func(config rest.EventHandlerConfig) error {
				return nil
			},
			PersistFunc: func(ctxt context.Context, logger rest.Log, resources []rest.Resource) []error {
				createBlogHandlerHit = true
				return nil
			},
		}
		params := rest.ResourceRepositoryParams{
			EventStore: eventDispatcher,
		}
		result, err := rest.NewResourceRepository(params)
		if err != nil {
			t.Fatalf("expected no error, got %s", err)
		}
		resourceRepository := result.Repository
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
