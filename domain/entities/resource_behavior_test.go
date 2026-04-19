package entities_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
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

// Compile-time check: CompositeBehavior implements ResourceBehavior.
var _ entities.ResourceBehavior = (*entities.CompositeBehavior)(nil)

// trackingBehavior records which hooks were called and can transform data.
type trackingBehavior struct {
	entities.DefaultBehavior
	label       string
	calls       *[]string
	failOn      string // hook name to fail on, e.g. "BeforeCreateCommit"
	transformFn func(json.RawMessage) json.RawMessage
}

func (b *trackingBehavior) record(hook string) {
	*b.calls = append(*b.calls, b.label+"."+hook)
}

func (b *trackingBehavior) maybeErr(hook string) error {
	if b.failOn == hook {
		return errors.New(hook + " error from " + b.label)
	}
	return nil
}

func (b *trackingBehavior) BeforeCreate(
	ctx context.Context, data json.RawMessage, rt *entities.ResourceType,
) (json.RawMessage, error) {
	b.record("BeforeCreate")
	if err := b.maybeErr("BeforeCreate"); err != nil {
		return nil, err
	}
	if b.transformFn != nil {
		data = b.transformFn(data)
	}
	return data, nil
}

func (b *trackingBehavior) BeforeCreateCommit(ctx context.Context, resource *entities.Resource) error {
	b.record("BeforeCreateCommit")
	return b.maybeErr("BeforeCreateCommit")
}

func (b *trackingBehavior) AfterCreate(ctx context.Context, resource *entities.Resource) error {
	b.record("AfterCreate")
	return b.maybeErr("AfterCreate")
}

func (b *trackingBehavior) BeforeUpdate(
	ctx context.Context, existing *entities.Resource, data json.RawMessage, rt *entities.ResourceType,
) (json.RawMessage, error) {
	b.record("BeforeUpdate")
	if err := b.maybeErr("BeforeUpdate"); err != nil {
		return nil, err
	}
	if b.transformFn != nil {
		data = b.transformFn(data)
	}
	return data, nil
}

func (b *trackingBehavior) BeforeUpdateCommit(ctx context.Context, resource *entities.Resource) error {
	b.record("BeforeUpdateCommit")
	return b.maybeErr("BeforeUpdateCommit")
}

func (b *trackingBehavior) AfterUpdate(ctx context.Context, resource *entities.Resource) error {
	b.record("AfterUpdate")
	return b.maybeErr("AfterUpdate")
}

func (b *trackingBehavior) BeforeDelete(ctx context.Context, resource *entities.Resource) error {
	b.record("BeforeDelete")
	return b.maybeErr("BeforeDelete")
}

func (b *trackingBehavior) AfterDelete(ctx context.Context, resource *entities.Resource) error {
	b.record("AfterDelete")
	return b.maybeErr("AfterDelete")
}

func TestCompositeBehavior_BeforeCreate_PipelinesData(t *testing.T) {
	var calls []string
	child := &trackingBehavior{
		label: "child", calls: &calls,
		transformFn: func(data json.RawMessage) json.RawMessage {
			return json.RawMessage(`{"step":"child"}`)
		},
	}
	parent := &trackingBehavior{
		label: "parent", calls: &calls,
		transformFn: func(data json.RawMessage) json.RawMessage {
			// Verify it received child's output
			if string(data) != `{"step":"child"}` {
				t.Errorf("parent received %s, want child output", data)
			}
			return json.RawMessage(`{"step":"parent"}`)
		},
	}

	comp := entities.NewCompositeBehavior([]entities.ResourceBehavior{child, parent})
	out, err := comp.BeforeCreate(context.Background(), json.RawMessage(`{"step":"input"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"step":"parent"}` {
		t.Errorf("got %s, want parent output", out)
	}
	if len(calls) != 2 || calls[0] != "child.BeforeCreate" || calls[1] != "parent.BeforeCreate" {
		t.Errorf("unexpected call order: %v", calls)
	}
}

func TestCompositeBehavior_BeforeCreate_ShortCircuitsOnError(t *testing.T) {
	var calls []string
	child := &trackingBehavior{label: "child", calls: &calls, failOn: "BeforeCreate"}
	parent := &trackingBehavior{label: "parent", calls: &calls}

	comp := entities.NewCompositeBehavior([]entities.ResourceBehavior{child, parent})
	_, err := comp.BeforeCreate(context.Background(), json.RawMessage(`{}`), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(calls) != 1 {
		t.Errorf("parent should not have been called, got calls: %v", calls)
	}
}

func TestCompositeBehavior_BeforeCreateCommit_GateSemantics(t *testing.T) {
	var calls []string
	child := &trackingBehavior{label: "child", calls: &calls}
	parent := &trackingBehavior{label: "parent", calls: &calls, failOn: "BeforeCreateCommit"}

	comp := entities.NewCompositeBehavior([]entities.ResourceBehavior{child, parent})
	err := comp.BeforeCreateCommit(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from parent")
	}
	if len(calls) != 2 {
		t.Errorf("both should be called before error, got: %v", calls)
	}
}

func TestCompositeBehavior_AfterCreate_FiresAll(t *testing.T) {
	var calls []string
	child := &trackingBehavior{label: "child", calls: &calls, failOn: "AfterCreate"}
	parent := &trackingBehavior{label: "parent", calls: &calls}

	comp := entities.NewCompositeBehavior([]entities.ResourceBehavior{child, parent})
	err := comp.AfterCreate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error to be returned")
	}
	// Both should have been called despite child's error
	if len(calls) != 2 {
		t.Errorf("expected both to fire, got: %v", calls)
	}
}

func TestCompositeBehavior_SingleElement_BehavesLikeSingle(t *testing.T) {
	var calls []string
	single := &trackingBehavior{label: "only", calls: &calls}

	comp := entities.NewCompositeBehavior([]entities.ResourceBehavior{single})
	_, err := comp.BeforeCreate(context.Background(), json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 || calls[0] != "only.BeforeCreate" {
		t.Errorf("unexpected calls: %v", calls)
	}
}

func TestCompositeBehavior_BeforeDelete_GateSemantics(t *testing.T) {
	var calls []string
	child := &trackingBehavior{label: "child", calls: &calls, failOn: "BeforeDelete"}
	parent := &trackingBehavior{label: "parent", calls: &calls}

	comp := entities.NewCompositeBehavior([]entities.ResourceBehavior{child, parent})
	err := comp.BeforeDelete(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from child")
	}
	// Child failed so parent should NOT be called
	if len(calls) != 1 {
		t.Errorf("expected only child called, got: %v", calls)
	}
}

func TestCompositeBehavior_AfterDelete_FiresAll(t *testing.T) {
	var calls []string
	child := &trackingBehavior{label: "child", calls: &calls, failOn: "AfterDelete"}
	parent := &trackingBehavior{label: "parent", calls: &calls}

	comp := entities.NewCompositeBehavior([]entities.ResourceBehavior{child, parent})
	err := comp.AfterDelete(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(calls) != 2 {
		t.Errorf("both should fire, got: %v", calls)
	}
}
