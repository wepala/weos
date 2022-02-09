package model_test

import (
	"context"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"os"
	"testing"
	"time"
)

func TestEventRepository_ReplayEvents(t *testing.T) {

	ctx := context.Background()

	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	err = api.Initialize(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	schemaName := "Blog"

	factories := api.GetEntityFactories()
	newContext := context.WithValue(ctx, weoscontext.ENTITY_FACTORY, factories[schemaName])

	mockPayload1 := map[string]interface{}{"weos_id": "12345", "sequence_no": int64(1), "title": "Test Blog", "url": "testing.com"}
	entity1 := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "12345",
			},
			SequenceNo: int64(0),
		},
		Property: mockPayload1,
	}
	event1 := model.NewEntityEvent("create", entity1, "12345", mockPayload1)
	entity1.NewChange(event1)

	mockPayload2 := map[string]interface{}{"weos_id": "123456", "sequence_no": int64(1), "title": "Test Blog1", "url": "testing1.com"}
	entity2 := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "123456",
			},
			SequenceNo: int64(0),
		},
		Property: mockPayload2,
	}
	event2 := model.NewEntityEvent("create", entity2, "123456", mockPayload2)
	entity2.NewChange(event2)

	repo, err := api.GetEventStore("Default")
	if err != nil {
		t.Fatal(err)
	}

	eventRepo := repo.(*model.EventRepositoryGorm)

	eventRepo.Persist(newContext, entity1)
	eventRepo.Persist(newContext, entity2)

	t.Run("replay events - drop tables", func(t *testing.T) {
		err = eventRepo.DB.Migrator().DropTable("Blog")
		if err != nil {
			t.Fatal(err)
		}

		total, successful, failed, err := eventRepo.ReplayEvents(ctx, time.Time{}, factories)
		if err != nil {
			t.Fatal(err)
		}

		if total != 2 {
			t.Fatalf("expected total events to be %d, got %d", 2, total)
		}

		if successful != 2 {
			t.Fatalf("expected successful events to be %d, got %d", 2, successful)
		}

		if failed != 0 {
			t.Fatalf("expected failed events to be %d, got %d", 0, failed)
		}
	})
	os.Remove("test.db")
}

func TestEventRepository_ReplayEvents2(t *testing.T) {

	ctx := context.Background()

	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	err = api.Initialize(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	schemaName := "Blog"

	factories := api.GetEntityFactories()
	newContext := context.WithValue(ctx, weoscontext.ENTITY_FACTORY, factories[schemaName])

	mockPayload1 := map[string]interface{}{"weos_id": "12345", "sequence_no": int64(1), "title": "Test Blog", "url": "testing.com"}
	entity1 := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "12345",
			},
			SequenceNo: int64(0),
		},
		Property: mockPayload1,
	}
	event1 := model.NewEntityEvent("create", entity1, "12345", mockPayload1)
	entity1.NewChange(event1)

	mockPayload2 := map[string]interface{}{"weos_id": "123456", "sequence_no": int64(1), "title": "Test Blog1", "url": "testing1.com"}
	entity2 := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "123456",
			},
			SequenceNo: int64(0),
		},
		Property: mockPayload2,
	}
	event2 := model.NewEntityEvent("create", entity2, "123456", mockPayload2)
	entity2.NewChange(event2)

	mockPayload3 := map[string]interface{}{"weos_id": "1234567", "sequence_no": int64(1), "title": "Test Blog2", "url": "testing2.com"}
	entity3 := &model.ContentEntity{
		AggregateRoot: model.AggregateRoot{
			BasicEntity: model.BasicEntity{
				ID: "1234567",
			},
			SequenceNo: int64(0),
		},
		Property: mockPayload3,
	}
	event3 := model.NewEntityEvent("create", entity3, "1234567", mockPayload3)
	entity3.NewChange(event3)

	repo, err := api.GetEventStore("Default")
	if err != nil {
		t.Fatal(err)
	}

	eventRepo := repo.(*model.EventRepositoryGorm)

	eventRepo.Persist(newContext, entity1)
	eventRepo.Persist(newContext, entity2)
	eventRepo.Persist(newContext, entity3)

	t.Run("replay events - existing data", func(t *testing.T) {

		total, successful, failed, err := eventRepo.ReplayEvents(ctx, time.Time{}, factories)
		if err != nil {
			t.Fatal(err)
		}

		if total != 3 {
			t.Fatalf("expected total events to be %d, got %d", 3, total)
		}

		if successful != 0 {
			t.Fatalf("expected successful events to be %d, got %d", 0, successful)
		}

		if failed != 3 {
			t.Fatalf("expected failed events to be %d, got %d", 3, failed)
		}
	})
	os.Remove("test.db")
}
