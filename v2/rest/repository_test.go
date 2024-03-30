package rest_test

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"testing"
)

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

		resource, err := new(rest.BasicResource).FromSchema(schema, []byte(`{
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
			DispatchFunc: func(ctx context.Context, event rest.Event, logger rest.Log) []error {
				//TODO check that the event is the correct one
				createBlogHandlerHit = true
				return nil
			},
			PersistFunc: func(ctxt context.Context, logger rest.Log, resources []rest.Resource) []error {
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
