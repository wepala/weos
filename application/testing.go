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
	eventStore domain.EventStore,
	repo repositories.ResourceRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	return subscribeResourceHandlers(d, eventStore, repo, projMgr, logger)
}

// SubscribeTripleHandlers exports the subscription for tests.
func SubscribeTripleHandlers(
	d *domain.EventDispatcher,
	tripleRepo repositories.TripleRepository,
	logger entities.Logger,
) error {
	return subscribeTripleHandlers(d, tripleRepo, logger)
}

// NewResourceServiceForTest creates a ResourceService without fx wiring.
// Pass nil for behaviors to use DefaultBehavior for all types.
func NewResourceServiceForTest(
	repo repositories.ResourceRepository,
	typeRepo repositories.ResourceTypeRepository,
	tripleRepo repositories.TripleRepository,
	eventStore domain.EventStore,
	dispatcher *domain.EventDispatcher,
	logger entities.Logger,
	behaviors ResourceBehaviorRegistry,
) ResourceService {
	if behaviors == nil {
		behaviors = make(ResourceBehaviorRegistry)
	}
	return &resourceService{
		repo:         repo,
		typeRepo:     typeRepo,
		tripleRepo:   tripleRepo,
		eventStore:   eventStore,
		dispatcher:   dispatcher,
		logger:       logger,
		behaviors:    behaviors,
		behaviorMeta: make(BehaviorMetaRegistry),
	}
}

// NewResourceServiceForTestWithSettings creates a ResourceService with
// behavior settings support for tests that need account-scoped behavior config.
func NewResourceServiceForTestWithSettings(
	repo repositories.ResourceRepository,
	typeRepo repositories.ResourceTypeRepository,
	tripleRepo repositories.TripleRepository,
	eventStore domain.EventStore,
	dispatcher *domain.EventDispatcher,
	logger entities.Logger,
	behaviors ResourceBehaviorRegistry,
	behaviorMeta BehaviorMetaRegistry,
	behaviorSettings repositories.BehaviorSettingsRepository,
) ResourceService {
	if behaviors == nil {
		behaviors = make(ResourceBehaviorRegistry)
	}
	if behaviorMeta == nil {
		behaviorMeta = make(BehaviorMetaRegistry)
	}
	return &resourceService{
		repo:             repo,
		typeRepo:         typeRepo,
		tripleRepo:       tripleRepo,
		eventStore:       eventStore,
		dispatcher:       dispatcher,
		logger:           logger,
		behaviors:        behaviors,
		behaviorMeta:     behaviorMeta,
		behaviorSettings: behaviorSettings,
	}
}

// NewResourceTypeServiceForTest creates a ResourceTypeService without fx wiring.
func NewResourceTypeServiceForTest(
	repo repositories.ResourceTypeRepository,
	projMgr repositories.ProjectionManager,
	eventStore domain.EventStore,
	dispatcher *domain.EventDispatcher,
	registry *PresetRegistry,
	logger entities.Logger,
	resourceSvc ResourceService,
) ResourceTypeService {
	return &resourceTypeService{
		repo:        repo,
		projMgr:     projMgr,
		eventStore:  eventStore,
		dispatcher:  dispatcher,
		registry:    registry,
		logger:      logger,
		resourceSvc: resourceSvc,
	}
}
