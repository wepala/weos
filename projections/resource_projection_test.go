package projections_test

import (
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"testing"
)

type TestEntity struct {
	model.AggregateRoot
	Name        string `json:"name"`
	Description string `json:"description"`
}

func TestResource_FromEvent(t *testing.T) {
	testEntity := &TestEntity{}
	payload := make(map[string]interface{})
	payload["name"] = "Test"
	payload["description"] = "Lorem Ipsum"
	event := model.NewEntityEvent("TestEntity", testEntity, "test", payload)

	resource, err := new(projections.Resource).FromEvent(event)
	if err != nil {
		t.Fatalf("unexpected error '%s' creating event", err)
	}

	if resource.Path == event.Meta.EntityPath {
		t.Errorf("expected path to be '%s', got '%s'", event.Meta.EntityPath, resource.Path)
	}
}
