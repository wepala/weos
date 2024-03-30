package rest

import (
	"fmt"
	"go.uber.org/fx"
	"golang.org/x/net/context"
	"sync"
)

type ResourceRepositoryParams struct {
	fx.In
	Handlers []EventHandlerConfig `group:"projections"`
}

type ResourceRepositoryResult struct {
	fx.Out
	Repository ResourceRepository
}

// NewResourceRepository creates a new resource repository and registers all the event handlers
func NewResourceRepository(p ResourceRepositoryParams) ResourceRepositoryResult {
	repo := ResourceRepository{
		handlers: make(map[string]map[string][]EventHandler),
	}
	for _, handler := range p.Handlers {
		repo.AddSubscriber(handler)
	}
	return ResourceRepositoryResult{
		Repository: repo,
	}
}

type ResourceRepository struct {
	handlers map[string]map[string][]EventHandler
}

func (r *ResourceRepository) AddSubscriber(handler EventHandlerConfig) error {
	if handler.Handler == nil {
		return fmt.Errorf("event handler cannot be nil")
	}
	if r.handlers == nil {
		r.handlers = make(map[string]map[string][]EventHandler)
	}
	if _, ok := r.handlers[handler.ResourceType]; !ok {
		r.handlers[handler.ResourceType] = make(map[string][]EventHandler)
	}
	if _, ok := r.handlers[handler.ResourceType][handler.Type]; !ok {
		r.handlers[handler.ResourceType][handler.Type] = make([]EventHandler, 0)
	}
	r.handlers[handler.ResourceType][handler.Type] = append(r.handlers[handler.ResourceType][handler.Type], handler.Handler)
	return nil
}

// GetSubscribers returns all the event handlers for a specific resource type
func (r *ResourceRepository) GetSubscribers(resourceType string) map[string][]EventHandler {
	if handlers, ok := r.handlers[resourceType]; ok {
		return handlers
	}
	return nil
}

// Dispatch executes all the event handlers for a specific event
func (r *ResourceRepository) Dispatch(ctx context.Context, event Event, logger Log) []error {
	//mutex helps keep state between routines
	var errors []error
	var wg sync.WaitGroup
	if resourceTypeHandlers, ok := r.handlers[event.Meta.ResourceType]; ok {

		if handlers, ok := resourceTypeHandlers[event.Type]; ok {
			//check to see if there were handlers registered for the event type that is not specific to a resource type
			if event.Meta.ResourceType != "" {
				if eventTypeHandlers, ok := r.handlers[""]; ok {
					if ehandlers, ok := eventTypeHandlers[event.Type]; ok {
						handlers = append(handlers, ehandlers...)
					}
				}
			}
			for i := 0; i < len(handlers); i++ {
				handler := handlers[i]
				wg.Add(1)
				go func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Errorf("handler panicked %s", r)
						}
						wg.Done()
					}()

					err := handler(ctx, logger, event)
					if err != nil {
						errors = append(errors, err)
					}

				}()
			}
			wg.Wait()
		}

	}

	return errors
}

func (r *ResourceRepository) Persist(ctxt context.Context, logger Log, resources []Resource) []error {
	var errs []error
	for _, resource := range resources {
		for _, event := range resource.GetNewChanges() {
			terrs := r.Dispatch(ctxt, *event, logger)
			errs = append(errs, terrs...)
		}
	}
	return errs
}

func (r *ResourceRepository) Remove(ctxt context.Context, logger Log, resources []Resource) error {
	//TODO implement me
	panic("implement me")
}
