package model_test

import (
	"encoding/json"
	model "github.com/wepala/weos-service/model"
	"golang.org/x/net/context"
	"testing"
)

func TestDomainService_Create(t *testing.T) {

	mockEventRepository := &EventRepositoryMock{
		PersistFunc: func(ctxt context.Context, entity model.AggregateInterface) error {
			return nil
		},
	}

	mockBlog := &Blog{
		Id:          "1",
		Title:       "First blog",
		Description: "Description testing",
	}
	entityType := "Blog"

	reqBytes, err := json.Marshal(mockBlog)
	if err != nil {
		t.Fatalf("error converting content type to bytes %s", err)
	}

	dService := model.NewDomainService(context.Background(), mockEventRepository)
	blog, err := dService.Create(context.Background(), reqBytes, entityType)

	if err != nil {
		t.Fatalf("unexpected error creating content type '%s'", err)
	}
	if blog == nil {
		t.Fatal("expected blog to be returned")
	}
	if blog.ID != mockBlog.Id {
		t.Fatalf("exppected blog id to be %s got %s", mockBlog.Id, blog.ID)
	}
}
