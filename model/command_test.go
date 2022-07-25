package model_test

import (
	weos "github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"testing"
)

func TestCommandDisptacher_Dispatch(t *testing.T) {
	mockCommand := &weos.Command{
		Type:    "TEST_COMMAND",
		Payload: nil,
		Metadata: weos.CommandMetadata{
			Version: 1,
		},
	}
	mockCommand2 := &weos.Command{
		Type:    "TEST_COMMAND",
		Payload: nil,
		Metadata: weos.CommandMetadata{
			Version:    1,
			EntityType: "Blog",
		},
	}
	dispatcher := &weos.DefaultCommandDispatcher{}
	handlersCalled := 0
	dispatcher.AddSubscriber(mockCommand, func(ctx context.Context, command *weos.Command, container weos.Container, eventRepository weos.EventRepository, projection weos.Projection, logger weos.Log) error {
		handlersCalled += 1
		return nil
	})

	dispatcher.AddSubscriber(&weos.Command{
		Type:     "*",
		Payload:  nil,
		Metadata: weos.CommandMetadata{},
	}, func(ctx context.Context, event *weos.Command, container weos.Container, eventRepository weos.EventRepository, projection weos.Projection, logger weos.Log) error {
		handlersCalled += 1
		if event.Type != mockCommand.Type {
			t.Errorf("expected the type to be '%s', got '%s'", mockCommand.Type, event.Type)
		}
		return nil
	})

	dispatcher.AddSubscriber(mockCommand2, func(ctx context.Context, command *weos.Command, container weos.Container, eventRepository weos.EventRepository, projection weos.Projection, logger weos.Log) error {
		handlersCalled += 1
		return nil
	})
	t.Run("call command for specific type", func(t *testing.T) {
		handlersCalled = 0
		dispatcher.Dispatch(context.TODO(), mockCommand2, nil, nil, nil, nil)
		if handlersCalled != 2 {
			t.Errorf("expected %d handler to be called, %d called", 2, handlersCalled)
		}
	})
	t.Run("should call handler with global handler", func(t *testing.T) {
		handlersCalled = 0
		dispatcher.Dispatch(context.TODO(), mockCommand, nil, nil, nil, nil)
		if handlersCalled != 2 {
			t.Errorf("expected %d handler to be called, %d called", 2, handlersCalled)
		}
	})

}
