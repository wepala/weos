package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"go.uber.org/fx"
	"golang.org/x/net/context"
)

type ResourceRepositoryParams struct {
	fx.In
	EventStore        EventStore
	Projections       []Projection `group:"projections"`
	DefaultProjection Projection   `name:"defaultProjection" optional:"true"`
	Config            *openapi3.T
}

type ResourceRepositoryResult struct {
	fx.Out
	Repository *ResourceRepository
}

// NewResourceRepository creates a new resource repository and registers all the event handlers
func NewResourceRepository(p ResourceRepositoryParams) (ResourceRepositoryResult, error) {
	repo := &ResourceRepository{
		eventStore:        p.EventStore,
		defaultProjection: p.DefaultProjection,
		projections:       p.Projections,
		config:            p.Config,
	}

	if p.DefaultProjection != nil {
		for _, handler := range p.DefaultProjection.GetEventHandlers() {
			err := p.EventStore.AddSubscriber(handler)
			if err != nil {
				return ResourceRepositoryResult{}, err
			}
		}
	}

	for _, projection := range p.Projections {
		for _, handler := range projection.GetEventHandlers() {
			err := p.EventStore.AddSubscriber(handler)
			if err != nil {
				return ResourceRepositoryResult{}, err
			}
		}
	}
	return ResourceRepositoryResult{
		Repository: repo,
	}, nil
}

type ResourceRepository struct {
	eventStore        EventStore
	defaultProjection Projection
	projections       []Projection
	config            *openapi3.T
}

func (r *ResourceRepository) Initialize(ctxt context.Context, logger Log, payload []byte) (resource Resource, err error) {
	//try to get the resource from the default projection and if it doesn't exist create it
	resource, err = r.defaultProjection.GetByURI(ctxt, logger, "")
	if err != nil {
		logger.Debugf("error encountered getting resource from default projection '%s'", err)
		return nil, err
	}
	if resource == nil {
		resource = new(BasicResource)
	}

	resource, err = resource.FromBytes(r.config, payload)

	return resource, err
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

func (r *ResourceRepository) GetProjections() []Projection {
	return r.projections
}

func (r *ResourceRepository) GetDefaultProjection() Projection {
	return r.defaultProjection
}
