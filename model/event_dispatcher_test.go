package model_test

import (
	"fmt"
	weos "github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"testing"
)

func TestEventDisptacher_Dispatch(t *testing.T) {
	mockEvent := &weos.Event{
		Type:    "TEST_EVENT",
		Payload: nil,
		Meta: weos.EventMeta{
			EntityID:      "some id",
			ApplicationID: "applicationID",
			RootID:        "accountID",
		},
		Version: 1,
	}
	dispatcher := &weos.DefaultEventDisptacher{}
	handlersCalled := 0
	dispatcher.AddSubscriber(func(ctx context.Context, event weos.Event) error {
		handlersCalled += 1
		return nil
	})

	dispatcher.AddSubscriber(func(ctx context.Context, event weos.Event) error {
		handlersCalled += 1
		if event.Type != mockEvent.Type {
			t.Errorf("expected the type to be '%s', got '%s'", mockEvent.Type, event.Type)
			return fmt.Errorf("expected the type to be '%s', got '%s'", mockEvent.Type, event.Type)
		}
		return nil
	})
	dispatcher.Dispatch(context.TODO(), *mockEvent)

	if handlersCalled != 2 {
		t.Errorf("expected %d handler to be called, %d called", 2, handlersCalled)
	}
}
