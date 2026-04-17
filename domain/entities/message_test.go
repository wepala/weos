package entities_test

import (
	"context"
	"testing"

	"github.com/wepala/weos/domain/entities"
)

func TestContextWithMessages_AccumulatesMessages(t *testing.T) {
	t.Parallel()
	ctx := entities.ContextWithMessages(context.Background())

	entities.AddMessage(ctx, entities.Message{Type: "info", Text: "hello"})
	entities.AddMessage(ctx, entities.Message{Type: "warning", Text: "watch out", Field: "name"})

	msgs := entities.GetMessages(ctx)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Type != "info" || msgs[0].Text != "hello" {
		t.Fatalf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].Field != "name" {
		t.Fatalf("expected field 'name', got %q", msgs[1].Field)
	}
}

func TestGetMessages_ReturnsNilWhenEmpty(t *testing.T) {
	t.Parallel()
	ctx := entities.ContextWithMessages(context.Background())

	msgs := entities.GetMessages(ctx)
	if msgs != nil {
		t.Fatalf("expected nil for empty messages, got %v", msgs)
	}
}

func TestAddMessage_NoOpWithoutAccumulator(t *testing.T) {
	t.Parallel()
	ctx := context.Background() // no accumulator

	// Should not panic.
	entities.AddMessage(ctx, entities.Message{Type: "error", Text: "ignored"})

	msgs := entities.GetMessages(ctx)
	if msgs != nil {
		t.Fatalf("expected nil without accumulator, got %v", msgs)
	}
}

func TestGetMessages_ReturnsNilWithoutAccumulator(t *testing.T) {
	t.Parallel()
	msgs := entities.GetMessages(context.Background())
	if msgs != nil {
		t.Fatalf("expected nil without accumulator, got %v", msgs)
	}
}

func TestMessage_CodeAndFieldAreOptional(t *testing.T) {
	t.Parallel()
	ctx := entities.ContextWithMessages(context.Background())

	entities.AddMessage(ctx, entities.Message{Type: "success", Text: "done"})
	entities.AddMessage(ctx, entities.Message{
		Type: "error", Text: "bad input", Field: "email", Code: "INVALID_EMAIL",
	})

	msgs := entities.GetMessages(ctx)
	if msgs[0].Field != "" || msgs[0].Code != "" {
		t.Fatalf("expected empty field/code, got field=%q code=%q", msgs[0].Field, msgs[0].Code)
	}
	if msgs[1].Code != "INVALID_EMAIL" {
		t.Fatalf("expected code INVALID_EMAIL, got %q", msgs[1].Code)
	}
}
