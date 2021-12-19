package model_test

import (
	"errors"
	weos "github.com/wepala/weos-service/model"
	"testing"
)

func TestBasicEntity_AddError(t *testing.T) {
	entity := &weos.BasicEntity{}
	entity.AddError(errors.New("some error"))
	if len(entity.GetErrors()) != 1 {
		t.Errorf("expected the length of error to be %d, got %d", 1, len(entity.GetErrors()))
	}
}
