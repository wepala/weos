package model_test

import (
	weos "github.com/wepala/weos/model"
	"testing"
)

type Entity1 struct {
	weos.AggregateRoot
}

func TestGetEntityType(t *testing.T) {
	t.Run("basic struct", func(t *testing.T) {
		entityType1 := weos.GetType(Entity1{})
		if entityType1 != "Entity1" {
			t.Errorf("expected the type to be %s, got '%s'", "Entity1", entityType1)
		}
	})

	t.Run("struct with pointer", func(t *testing.T) {
		entityType2 := weos.GetType(&Entity1{})
		if entityType2 != "Entity1" {
			t.Errorf("expected the type to be %s, got '%s'", "Entity1", entityType2)
		}
	})

}
