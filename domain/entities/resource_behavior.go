package entities

import (
	"context"
	"encoding/json"
)

// BehaviorMeta describes a behavior for display and configuration purposes.
// Presets declare metadata alongside their ResourceBehavior implementations
// so that APIs can expose which behaviors exist, their defaults, and whether
// users can toggle them per account.
type BehaviorMeta struct {
	Slug        string // matches the key in PresetDefinition.Behaviors
	DisplayName string // human-readable name shown in UI
	Description string // short explanation of what this behavior does
	Default     bool   // enabled when no account-level override exists
	Manageable  bool   // if true, account admins can toggle on/off
}

// ResourceBehavior allows concrete types to specialize business logic for
// specific resource type slugs. If no behavior is registered for a slug,
// DefaultBehavior (a no-op) is used — mirroring the frontend pattern where
// default screens can be overridden per resource type.
type ResourceBehavior interface {
	// BeforeCreate runs after the ResourceType is loaded but before schema
	// validation. It may transform the input data (e.g. inject defaults) or
	// return an error to reject the operation.
	BeforeCreate(ctx context.Context, data json.RawMessage, rt *ResourceType) (json.RawMessage, error)

	// BeforeCreateCommit runs after the Resource entity is constructed but
	// before the UnitOfWork commit. Return an error to reject.
	BeforeCreateCommit(ctx context.Context, resource *Resource) error

	// AfterCreate runs after a successful commit. Errors are logged but do
	// not fail the operation (the commit has already succeeded).
	AfterCreate(ctx context.Context, resource *Resource) error

	// BeforeUpdate runs before schema validation on an update. It receives
	// the existing resource and may transform the incoming data or reject.
	BeforeUpdate(ctx context.Context, existing *Resource, data json.RawMessage, rt *ResourceType) (json.RawMessage, error)

	// BeforeUpdateCommit runs after entity.Update() but before UoW commit.
	BeforeUpdateCommit(ctx context.Context, resource *Resource) error

	// AfterUpdate runs after a successful update commit.
	AfterUpdate(ctx context.Context, resource *Resource) error

	// BeforeDelete runs before entity.MarkDeleted(). Return an error to reject.
	BeforeDelete(ctx context.Context, resource *Resource) error

	// AfterDelete runs after a successful delete commit.
	AfterDelete(ctx context.Context, resource *Resource) error
}

// DefaultBehavior is a no-op implementation of ResourceBehavior. Concrete
// behaviors embed this struct and override only the hooks they need.
type DefaultBehavior struct{}

func (DefaultBehavior) BeforeCreate(_ context.Context, data json.RawMessage, _ *ResourceType) (json.RawMessage, error) {
	return data, nil
}

func (DefaultBehavior) BeforeCreateCommit(_ context.Context, _ *Resource) error {
	return nil
}

func (DefaultBehavior) AfterCreate(_ context.Context, _ *Resource) error {
	return nil
}

func (DefaultBehavior) BeforeUpdate(_ context.Context, _ *Resource, data json.RawMessage, _ *ResourceType) (json.RawMessage, error) {
	return data, nil
}

func (DefaultBehavior) BeforeUpdateCommit(_ context.Context, _ *Resource) error {
	return nil
}

func (DefaultBehavior) AfterUpdate(_ context.Context, _ *Resource) error {
	return nil
}

func (DefaultBehavior) BeforeDelete(_ context.Context, _ *Resource) error {
	return nil
}

func (DefaultBehavior) AfterDelete(_ context.Context, _ *Resource) error {
	return nil
}

// CompositeBehavior chains multiple ResourceBehavior instances (child first,
// then parents up the rdfs:subClassOf hierarchy). Data-returning hooks pipeline
// outputs; gate hooks short-circuit on first error; after hooks fire all.
type CompositeBehavior struct {
	chain []ResourceBehavior
}

// NewCompositeBehavior creates a composite from an ordered chain of behaviors.
func NewCompositeBehavior(chain []ResourceBehavior) *CompositeBehavior {
	return &CompositeBehavior{chain: chain}
}

func (c *CompositeBehavior) BeforeCreate(
	ctx context.Context, data json.RawMessage, rt *ResourceType,
) (json.RawMessage, error) {
	for _, b := range c.chain {
		var err error
		data, err = b.BeforeCreate(ctx, data, rt)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (c *CompositeBehavior) BeforeCreateCommit(ctx context.Context, resource *Resource) error {
	for _, b := range c.chain {
		if err := b.BeforeCreateCommit(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (c *CompositeBehavior) AfterCreate(ctx context.Context, resource *Resource) error {
	var first error
	for _, b := range c.chain {
		if err := b.AfterCreate(ctx, resource); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (c *CompositeBehavior) BeforeUpdate(
	ctx context.Context, existing *Resource, data json.RawMessage, rt *ResourceType,
) (json.RawMessage, error) {
	for _, b := range c.chain {
		var err error
		data, err = b.BeforeUpdate(ctx, existing, data, rt)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (c *CompositeBehavior) BeforeUpdateCommit(ctx context.Context, resource *Resource) error {
	for _, b := range c.chain {
		if err := b.BeforeUpdateCommit(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (c *CompositeBehavior) AfterUpdate(ctx context.Context, resource *Resource) error {
	var first error
	for _, b := range c.chain {
		if err := b.AfterUpdate(ctx, resource); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (c *CompositeBehavior) BeforeDelete(ctx context.Context, resource *Resource) error {
	for _, b := range c.chain {
		if err := b.BeforeDelete(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (c *CompositeBehavior) AfterDelete(ctx context.Context, resource *Resource) error {
	var first error
	for _, b := range c.chain {
		if err := b.AfterDelete(ctx, resource); err != nil && first == nil {
			first = err
		}
	}
	return first
}
