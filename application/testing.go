package application

import (
	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// SortOptions re-exports for integration test convenience.
type SortOptions = repositories.SortOptions

// SubscribeResourceTypeHandlers exports the subscription for tests.
func SubscribeResourceTypeHandlers(
	d *domain.EventDispatcher,
	repo repositories.ResourceTypeRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	return subscribeResourceTypeHandlers(d, repo, projMgr, logger)
}

// SubscribeResourceHandlers exports the subscription for tests.
func SubscribeResourceHandlers(
	d *domain.EventDispatcher,
	repo repositories.ResourceRepository,
	logger entities.Logger,
) error {
	return subscribeResourceHandlers(d, repo, logger)
}

// SubscribeTripleHandlers exports the subscription for tests.
func SubscribeTripleHandlers(
	d *domain.EventDispatcher,
	tripleRepo repositories.TripleRepository,
	tripleSvc TripleService,
	resourceRepo repositories.ResourceRepository,
	rtRepo repositories.ResourceTypeRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	return subscribeTripleHandlers(d, tripleRepo, tripleSvc, resourceRepo, rtRepo, projMgr, logger)
}

// NewResourceServiceForTest creates a ResourceService without fx wiring.
// Pass nil for behaviors to use DefaultBehavior for all types.
func NewResourceServiceForTest(
	repo repositories.ResourceRepository,
	typeRepo repositories.ResourceTypeRepository,
	eventStore domain.EventStore,
	dispatcher *domain.EventDispatcher,
	logger entities.Logger,
	behaviors ResourceBehaviorRegistry,
) ResourceService {
	if behaviors == nil {
		behaviors = make(ResourceBehaviorRegistry)
	}
	return &resourceService{
		repo:       repo,
		typeRepo:   typeRepo,
		eventStore: eventStore,
		dispatcher: dispatcher,
		logger:     logger,
		behaviors:  behaviors,
	}
}
