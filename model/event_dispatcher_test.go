package model_test

import (
	weos "github.com/wepala/weos-content-service/model"
	"golang.org/x/net/context"
	"testing"
)

func TestEventDisptacher_Dispatch(t *testing.T) {
	mockEvent := &weos.Event{
		Type:    "TEST_EVENT",
		Payload: nil,
		Meta: weos.EventMeta{
			EntityID: "some id",
			Module:   "applicationID",
			RootID:   "accountID",
		},
		Version: 1,
	}
	dispatcher := &weos.EventDisptacher{}
	handlersCalled := 0
	dispatcher.AddSubscriber(func(ctx context.Context, event weos.Event) {
		handlersCalled += 1
	})

	dispatcher.AddSubscriber(func(ctx context.Context, event weos.Event) {
		handlersCalled += 1
		if event.Type != mockEvent.Type {
			t.Errorf("expected the type to be '%s', got '%s'", mockEvent.Type, event.Type)
		}
	})
	dispatcher.Dispatch(context.TODO(), *mockEvent)

	if handlersCalled != 2 {
		t.Errorf("expected %d handler to be called, %d called", 2, handlersCalled)
	}
}
