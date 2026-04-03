package entities_test

import (
	"context"
	"encoding/json"
	"testing"

	"weos/domain/entities"
)

// Compile-time check: DefaultBehavior implements ResourceBehavior.
var _ entities.ResourceBehavior = entities.DefaultBehavior{}

func TestDefaultBehavior_BeforeCreate_PassesThroughData(t *testing.T) {
	b := entities.DefaultBehavior{}
	input := json.RawMessage(`{"name":"test"}`)

	out, err := b.BeforeCreate(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(input) {
		t.Errorf("expected data pass-through, got %s", out)
	}
}

func TestDefaultBehavior_BeforeUpdate_PassesThroughData(t *testing.T) {
	b := entities.DefaultBehavior{}
	input := json.RawMessage(`{"name":"updated"}`)

	out, err := b.BeforeUpdate(context.Background(), nil, input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(input) {
		t.Errorf("expected data pass-through, got %s", out)
	}
}

func TestDefaultBehavior_AllHooksReturnNil(t *testing.T) {
	b := entities.DefaultBehavior{}
	ctx := context.Background()

	if err := b.BeforeCreateCommit(ctx, nil); err != nil {
		t.Errorf("BeforeCreateCommit: %v", err)
	}
	if err := b.AfterCreate(ctx, nil); err != nil {
		t.Errorf("AfterCreate: %v", err)
	}
	if err := b.BeforeUpdateCommit(ctx, nil); err != nil {
		t.Errorf("BeforeUpdateCommit: %v", err)
	}
	if err := b.AfterUpdate(ctx, nil); err != nil {
		t.Errorf("AfterUpdate: %v", err)
	}
	if err := b.BeforeDelete(ctx, nil); err != nil {
		t.Errorf("BeforeDelete: %v", err)
	}
	if err := b.AfterDelete(ctx, nil); err != nil {
		t.Errorf("AfterDelete: %v", err)
	}
}
