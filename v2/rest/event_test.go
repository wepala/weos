package rest_test

import (
	"github.com/wepala/weos/v2/rest"
	"golang.org/x/net/context"
	"testing"
)

func TestDefaultEventDisptacher_AddSubscriber(t *testing.T) {
	t.Run("add subscriber for event type only", func(t *testing.T) {
		eventDispatcher := new(rest.GORMProjection)
		err := eventDispatcher.AddSubscriber(rest.EventHandlerConfig{
			Type: "create",
			Handler: func(ctx context.Context, logger rest.Log, event *rest.Event) error {
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		handlers := eventDispatcher.GetSubscribers("")
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
		eventDispatcher := new(rest.GORMProjection)
		err := eventDispatcher.AddSubscriber(rest.EventHandlerConfig{
			ResourceType: "Article",
			Type:         "create",
			Handler: func(ctx context.Context, logger rest.Log, event *rest.Event) error {
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		handlers := eventDispatcher.GetSubscribers("Article")
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
		eventDispatcher := new(rest.GORMProjection)
		err := eventDispatcher.AddSubscriber(rest.EventHandlerConfig{
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
		eventDispatcher := new(rest.GORMProjection)
		err := eventDispatcher.AddSubscriber(rest.EventHandlerConfig{
			Type: "create",
			Handler: func(ctx context.Context, logger rest.Log, event *rest.Event) error {
				createHandlerHit = true
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		err = eventDispatcher.AddSubscriber(rest.EventHandlerConfig{
			Type:         "create",
			ResourceType: "Article",
			Handler: func(ctx context.Context, logger rest.Log, event *rest.Event) error {
				articleCreateHandlerHit = true
				return nil
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %s", err)
		}
		errors := eventDispatcher.Dispatch(context.Background(), logger, &rest.Event{
			Type: "create",
			Meta: rest.EventMeta{
				ResourceType: "Article",
			},
		})
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
