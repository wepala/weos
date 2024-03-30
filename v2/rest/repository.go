package rest

import (
	"go.uber.org/fx"
	"golang.org/x/net/context"
)

type ResourceRepositoryParams struct {
	fx.In
	EventStore EventStore
	Handlers   []EventHandlerConfig `group:"projections"`
}

type ResourceRepositoryResult struct {
	fx.Out
	Repository ResourceRepository
}

// NewResourceRepository creates a new resource repository and registers all the event handlers
func NewResourceRepository(p ResourceRepositoryParams) (ResourceRepositoryResult, error) {
	repo := ResourceRepository{
		eventStore: p.EventStore,
	}
	for _, handler := range p.Handlers {
		err := p.EventStore.AddSubscriber(handler)
		if err != nil {
			return ResourceRepositoryResult{}, err
		}
	}
	return ResourceRepositoryResult{
		Repository: repo,
	}, nil
}

type ResourceRepository struct {
	eventStore EventStore
}

func (r *ResourceRepository) Persist(ctxt context.Context, logger Log, resources []Resource) []error {
	var errs []error
	for _, resource := range resources {
		r.eventStore.Persist(ctxt, logger, resource.GetNewChanges())
	}
	return errs
}

func (r *ResourceRepository) Remove(ctxt context.Context, logger Log, resources []Resource) error {
	//TODO implement me
	panic("implement me")
}
