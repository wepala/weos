package model_test

import (
	weos "github.com/wepala/weos/model"
	"testing"
)

func TestAggregateRoot_DefaultReducer(t *testing.T) {
	type BaseAggregate struct {
		weos.AggregateRoot
		Title string `json:"title"`
	}

	mockEvent, err := weos.NewBasicEvent("Event", "", "BaseAggregate", &struct {
		Title string `json:"title"`
	}{Title: "Test"})
	if err != nil {
		t.Fatalf("error creating mock event '%s'", err)
	}
	baseAggregate := &BaseAggregate{}
	baseAggregate = weos.DefaultReducer(baseAggregate, mockEvent, nil).(*BaseAggregate)
	if baseAggregate.Title != "Test" {
		t.Errorf("expected aggregate title to be '%s', got '%s'", "Test", baseAggregate.Title)
	}
}

func TestAggregateRoot_NewAggregateFromEvents(t *testing.T) {
	type BaseAggregate struct {
		weos.AggregateRoot
		Title string `json:"title"`
	}

	mockEvent, err := weos.NewBasicEvent("Event", "", "BaseAggregate", &struct {
		Title string `json:"title"`
	}{Title: "Test"})
	if err != nil {
		t.Fatalf("error creating mock event '%s'", err)
	}
	baseAggregate := &BaseAggregate{}
	baseAggregate = weos.NewAggregateFromEvents(baseAggregate, []*weos.Event{mockEvent}).(*BaseAggregate)
	if baseAggregate.Title != "Test" {
		t.Errorf("expected aggregate title to be '%s', got '%s'", "Test", baseAggregate.Title)
	}
}
