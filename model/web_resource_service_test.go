package model_test

import (
	"github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"testing"
)

func TestWebResourceService_Create(t *testing.T) {

	mockResourceStore := make(map[string]model.Entity)

	repository := &ProjectionMock{
		GetByIDFunc: func(ctxt context.Context, id string) (model.Entity, error) {
			if item, ok := mockResourceStore[id]; ok {
				return item, nil
			}
			return nil, nil
		},
	}
	//mock command dispatcher
	commandDispatcher := &CommandDispatcherMock{
		DispatchFunc: func(ctx context.Context, command *model.Command, eventStore model.EventRepository, projection model.Projection, logger model.Log) error {
			return nil
		},
	}

	service := model.NewWebResourceService("https://weos.cloud", repository)

	t.Run("create collection and item with no schema", func(t *testing.T) {
		payload := `{
	"name": "Sojourner Truth",
	"givenName": "Sojourner",
	"age": 300
}`

		entity, err := service.Create(nil, "/some-user/users", "application/json", []byte(payload), nil)
		if err != nil {
			t.Fatalf("unexpected error creating resource '%s'", err)
		}

		//confirm the appropriate checks were done
		if len(repository.GetByIDCalls()) != 3 {
			t.Errorf("expected %d check for if the web resource already existed, got %d checks", 3, len(repository.GetByIDCalls()))
		}

		//check that the values are what we expect
		var ok bool
		var value interface{}
		if value, ok = entity["name"]; ok {
			if value != "Sojourner Truth" {
				t.Errorf("expected the name to be '%s', got '%s'", "Sojourner Truth", entity["name"])
			}
		}
		if !ok {
			t.Errorf("expected a value for '%s'", "name")
		}

		//check that URI etc is what we expect
		if entity.URI() != "https://weos.cloud/some-user/users/123" {
			t.Errorf("expected the url to be '%s', got '%s'", "https://weos.cloud/some-user/users/123", entity.URI())
		}

		if entity.URN() != "wern:123" {
			t.Errorf("expected the urn to be '%s', got '%s'", "wern:123", entity.URN())
		}

		if entity.Type() != "https://weos.cloud/_schemas/some-user/users" {
			t.Errorf("expected the type to be '%s', got '%s'", "https://weos.cloud/_schemas/some-user/users", entity.URN())
		}

		//confirm that the expected web resources are created
		if len(repository.GetEventHandlerCalls()) != 3 {
			t.Errorf("expected %d web resources to be created, %d were created", 3, len(repository.GetEventHandlerCalls()))
		}

	})

}
