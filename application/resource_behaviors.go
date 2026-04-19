// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync/atomic"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// isNilInterface returns true if v is a nil interface value or an interface
// holding a typed-nil pointer/map/slice/chan/func. This catches the common Go
// pitfall where `var p *T = nil; var i I = p` produces an interface that
// compares != nil but still panics when dereferenced.
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// ResourceWriter is the subset of ResourceService that behaviors need to
// create, update, and delete other resources from inside a hook. It
// intentionally omits queries — use BehaviorServices.Resources for reads.
//
// Write methods go through the full ResourceService pipeline: schema
// validation, JSON-LD graph assembly, triple extraction, event recording, and
// UnitOfWork commit, including nested behavior dispatch on the affected
// resources.
type ResourceWriter interface {
	Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error)
	Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
	Delete(ctx context.Context, cmd DeleteResourceCommand) error
}

// lazyResourceWriter is a ResourceWriter proxy that is constructed before the
// real ResourceService exists and populated via SetTarget once the Fx
// container has wired ResourceService. It breaks the Fx construction cycle
// ResourceService -> ResourceBehaviorRegistry -> ResourceService (the inner
// step runs through BehaviorServices.Writer, which the registry provider
// populates with this proxy).
//
// Behaviors receive this proxy at factory time and close over it; by the time
// any hook runs (during a request), Fx has invoked SetTarget so the proxy
// forwards cleanly to the real service. Calls before wiring return a clear
// error instead of panicking.
//
// Because behaviors write through this proxy, each forwarded call enters
// resourceService.Create/Update/Delete, which apply a recursion-depth guard
// (see maxBehaviorRecursionDepth in resource_service.go) so runaway cascades
// fail fast instead of blowing the stack.
type lazyResourceWriter struct {
	// svc is set exactly once by SetTarget and then read concurrently by every
	// behavior hook. The atomic store/load pair gives a happens-before edge
	// independent of Fx ordering, so there is no data race even if a future
	// refactor reorders construction.
	svc atomic.Pointer[ResourceWriter]
}

func newLazyResourceWriter() *lazyResourceWriter { return &lazyResourceWriter{} }

// SetTarget installs the real ResourceWriter. It must be called exactly once,
// before any behavior hook can fire — in production that ordering is enforced
// by registering WireResourceWriter as an Fx invoke that runs after
// ProvideResourceService but before any invoke or lifecycle hook that can
// trigger a resource write (see application/module.go for the current order).
// Returns an error on nil input or double-set so misuse fails startup rather
// than silently installing a broken target.
func (l *lazyResourceWriter) SetTarget(svc ResourceWriter) error {
	if isNilInterface(svc) {
		return fmt.Errorf("lazyResourceWriter.SetTarget: nil ResourceWriter")
	}
	if svc == ResourceWriter(l) {
		return fmt.Errorf("lazyResourceWriter.SetTarget: refusing to target self (would infinite-loop)")
	}
	if !l.svc.CompareAndSwap(nil, &svc) {
		return fmt.Errorf("lazyResourceWriter.SetTarget: already wired")
	}
	return nil
}

// target returns the installed ResourceWriter or an error if SetTarget has
// not run yet. The error phrasing points at the likely cause — a behavior
// fired before Fx wiring completed.
func (l *lazyResourceWriter) target(op string) (ResourceWriter, error) {
	p := l.svc.Load()
	if p == nil {
		return nil, fmt.Errorf(
			"ResourceWriter.%s called before wiring; behavior invoked during startup?", op,
		)
	}
	return *p, nil
}

func (l *lazyResourceWriter) Create(
	ctx context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	svc, err := l.target("Create")
	if err != nil {
		return nil, err
	}
	return svc.Create(ctx, cmd)
}

func (l *lazyResourceWriter) Update(
	ctx context.Context, cmd UpdateResourceCommand,
) (*entities.Resource, error) {
	svc, err := l.target("Update")
	if err != nil {
		return nil, err
	}
	return svc.Update(ctx, cmd)
}

func (l *lazyResourceWriter) Delete(ctx context.Context, cmd DeleteResourceCommand) error {
	svc, err := l.target("Delete")
	if err != nil {
		return err
	}
	return svc.Delete(ctx, cmd)
}

// BehaviorServices bundles application services that ResourceBehavior factories
// may depend on. All fields are required when constructed by
// ProvideResourceBehaviorRegistry; tests that build BehaviorServices directly
// must supply real or fake implementations for any field their behavior touches.
type BehaviorServices struct {
	Resources     repositories.ResourceRepository
	Triples       repositories.TripleRepository
	ResourceTypes repositories.ResourceTypeRepository
	Logger        entities.Logger
	// Writer lets behaviors create, update, or delete other resources through
	// the full ResourceService pipeline. See lazyResourceWriter for the
	// cycle-breaking rationale behind how this is wired.
	Writer ResourceWriter
}

// BehaviorFactory constructs a ResourceBehavior given the available application
// services. Factories are invoked once at startup, after the Fx container is
// wired, allowing behaviors to close over real repositories and loggers.
// A factory must not return nil — that is treated as a programmer error and
// fails startup.
type BehaviorFactory func(services BehaviorServices) entities.ResourceBehavior

// StaticBehavior wraps a pre-constructed ResourceBehavior in a BehaviorFactory.
// Use this for behaviors that have no service dependencies, so presets can
// declare them inline without writing a factory function. Panics if b is nil.
func StaticBehavior(b entities.ResourceBehavior) BehaviorFactory {
	if b == nil {
		panic("application.StaticBehavior: behavior must not be nil")
	}
	return func(BehaviorServices) entities.ResourceBehavior { return b }
}

// ResourceBehaviorRegistry maps resource type slugs to their custom behaviors.
// Types without a registered behavior use DefaultBehavior (no-op).
type ResourceBehaviorRegistry map[string]entities.ResourceBehavior

// ProvideResourceBehaviorRegistry builds the behavior registry from all
// registered presets, invoking each factory with the supplied services. Fails
// startup if any injected dependency is nil or any factory returns nil. The
// writer parameter is an unwired *lazyResourceWriter — see that type for the
// cycle-breaking rationale.
func ProvideResourceBehaviorRegistry(
	registry *PresetRegistry,
	resources repositories.ResourceRepository,
	triples repositories.TripleRepository,
	resourceTypes repositories.ResourceTypeRepository,
	logger entities.Logger,
	writer *lazyResourceWriter,
) (ResourceBehaviorRegistry, error) {
	if registry == nil {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil PresetRegistry")
	}
	if isNilInterface(resources) {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil Resources")
	}
	if isNilInterface(triples) {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil Triples")
	}
	if isNilInterface(resourceTypes) {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil ResourceTypes")
	}
	if isNilInterface(logger) {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil Logger")
	}
	if writer == nil {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil lazyResourceWriter")
	}
	services := BehaviorServices{
		Resources:     resources,
		Triples:       triples,
		ResourceTypes: resourceTypes,
		Logger:        logger,
		Writer:        writer,
	}
	behaviors, err := registry.Behaviors(services)
	if err != nil {
		return nil, err
	}
	slugs := make([]string, 0, len(behaviors))
	for slug := range behaviors {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	logger.Info(context.Background(), "resource behaviors registered",
		"count", len(slugs), "slugs", slugs)
	return behaviors, nil
}

// WireResourceWriter installs the real ResourceService into the lazy writer
// proxy after both have been constructed by Fx.
//
// Fx guarantees that lazyResourceWriter and ResourceService are available
// before this invoke runs (parameter-level dependency ordering), but it does
// NOT impose ordering relative to other fx.Invoke calls that also depend on
// ResourceService. Any startup invoke that may call ResourceService.Create,
// Update, or Delete — and thus trigger behavior hooks that use
// BehaviorServices.Writer — MUST be registered after WireResourceWriter in
// application/module.go. Today the only such invoke is
// ensureBuiltInResourceTypes, and the module registers WireResourceWriter
// first; adding a new hook-triggering invoke earlier would silently regress
// this contract.
//
// Returns an error on nil svc, self-target, or double-set so Fx aborts
// startup loudly instead of silently installing a broken proxy.
func WireResourceWriter(writer *lazyResourceWriter, svc ResourceService) error {
	if writer == nil {
		return fmt.Errorf("WireResourceWriter: nil lazyResourceWriter")
	}
	if err := writer.SetTarget(svc); err != nil {
		return fmt.Errorf("WireResourceWriter: %w", err)
	}
	return nil
}

// BehaviorMetaRegistry maps resource type slugs to their behavior metadata.
// Used by services to expose available behaviors and enforce manageability.
type BehaviorMetaRegistry map[string]entities.BehaviorMeta

// ProvideBehaviorMetaRegistry builds the metadata registry from all presets.
func ProvideBehaviorMetaRegistry(registry *PresetRegistry) BehaviorMetaRegistry {
	return registry.BehaviorsMeta()
}
