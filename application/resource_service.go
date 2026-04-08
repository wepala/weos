package application

import (
	"context"
	"encoding/json"
	"fmt"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/pkg/identity"
	"weos/pkg/jsonld"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.uber.org/fx"
)

type ResourceService interface {
	Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error)
	GetByID(ctx context.Context, id string) (*entities.Resource, error)
	List(ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[*entities.Resource], error)
	ListFlat(ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[map[string]any], error)
	ListByField(ctx context.Context, typeSlug, fieldName, fieldValue string) (
		repositories.PaginatedResponse[*entities.Resource], error)
	ListWithFilters(ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
		cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[*entities.Resource], error)
	ListFlatWithFilters(ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
		cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[map[string]any], error)
	Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
	Delete(ctx context.Context, cmd DeleteResourceCommand) error
}

type resourceService struct {
	repo             repositories.ResourceRepository
	typeRepo         repositories.ResourceTypeRepository
	tripleRepo       repositories.TripleRepository
	permRepo         repositories.ResourcePermissionRepository
	accountRepo      authrepos.AccountRepository
	eventStore       domain.EventStore
	dispatcher       *domain.EventDispatcher
	logger           entities.Logger
	behaviors        ResourceBehaviorRegistry
	behaviorMeta     BehaviorMetaRegistry
	behaviorSettings repositories.BehaviorSettingsRepository
}

// maxBehaviorRecursionDepth caps how deep a behavior cascade can go. Behaviors
// can legitimately create/update/delete other resources (via
// BehaviorServices.Writer), and those cross-resource writes run the target's
// own behaviors, so cascades are expected (e.g. course-instance creates
// education-events, which create attendance-records). A small depth limit
// catches accidental cycles fast instead of blowing the stack or hanging on
// exponential fan-out.
const maxBehaviorRecursionDepth = 8

type behaviorDepthKey struct{}

// enterResourceCall increments the cascade depth counter in ctx and returns
// the updated context. It fails if the depth would exceed
// maxBehaviorRecursionDepth — an indicator of a cycle or runaway cascade.
// On failure it returns the original (unchanged) ctx alongside the error so a
// caller that forgets to guard the error still gets a usable context.
//
// Because context values are immutable, sibling writes from the same hook
// each see the parent's depth N (not N+1 and N+2) — only true nesting
// inflates the counter, which is the intended accounting.
func enterResourceCall(ctx context.Context) (context.Context, error) {
	depth, _ := ctx.Value(behaviorDepthKey{}).(int)
	nextDepth := depth + 1
	if depth >= maxBehaviorRecursionDepth {
		return ctx, fmt.Errorf(
			"resource behavior recursion depth reached max %d; attempted next depth %d would exceed it (likely cycle)",
			maxBehaviorRecursionDepth, nextDepth,
		)
	}
	return context.WithValue(ctx, behaviorDepthKey{}, nextDepth), nil
}

func (s *resourceService) behaviorFor(ctx context.Context, rt *entities.ResourceType) entities.ResourceBehavior {
	if rt == nil {
		return entities.DefaultBehavior{}
	}

	enabledSet := s.resolveEnabledBehaviors(ctx, rt.Slug())

	var chain []entities.ResourceBehavior
	visited := map[string]bool{rt.Slug(): true}
	current := rt

	for current != nil {
		if b, ok := s.behaviors[current.Slug()]; ok {
			if s.isBehaviorEnabled(current.Slug(), enabledSet) {
				chain = append(chain, b)
			}
		}
		parentSlug := jsonld.SubClassOf(current.Context())
		if parentSlug == "" || visited[parentSlug] {
			break
		}
		visited[parentSlug] = true
		parentRT, err := s.typeRepo.FindBySlug(ctx, parentSlug)
		if err != nil {
			s.logger.Warn(ctx, "parent type not found for behavior inheritance",
				"child", current.Slug(), "parent", parentSlug)
			break
		}
		current = parentRT
	}

	switch len(chain) {
	case 0:
		return entities.DefaultBehavior{}
	case 1:
		return chain[0]
	default:
		return entities.NewCompositeBehavior(chain)
	}
}

// resolveEnabledBehaviors returns the set of enabled behavior slugs for the
// given resource type in the caller's account context. Returns nil when no
// account override exists (use preset defaults).
func (s *resourceService) resolveEnabledBehaviors(ctx context.Context, typeSlug string) map[string]bool {
	accountID := ""
	if ident := auth.AgentFromCtx(ctx); ident != nil {
		accountID = ident.ActiveAccountID
	}
	if accountID == "" {
		return nil // no account context — use defaults
	}
	if s.behaviorSettings == nil {
		return nil // no settings repo configured — use defaults
	}

	slugs, err := s.behaviorSettings.GetByAccountAndType(ctx, accountID, typeSlug)
	if err != nil {
		s.logger.Error(ctx, "failed to load behavior settings, using defaults",
			"account", accountID, "type", typeSlug, "error", err)
		return nil
	}
	if slugs == nil {
		return nil // no override — use defaults
	}

	set := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		set[slug] = true
	}
	return set
}

// isBehaviorEnabled checks whether a behavior slug should be active.
// Account overrides only apply to manageable behaviors.
// Non-manageable behaviors always use preset defaults.
// When no metadata exists for the slug, the behavior fires (backward compat).
func (s *resourceService) isBehaviorEnabled(slug string, enabledSet map[string]bool) bool {
	meta, ok := s.behaviorMeta[slug]
	if !ok {
		return true // no metadata — legacy behavior, always fire
	}

	if enabledSet != nil && meta.Manageable {
		return enabledSet[slug]
	}
	return meta.Default
}

func ProvideResourceService(params struct {
	fx.In
	Repo             repositories.ResourceRepository
	TypeRepo         repositories.ResourceTypeRepository
	TripleRepo       repositories.TripleRepository
	PermRepo         repositories.ResourcePermissionRepository
	AccountRepo      authrepos.AccountRepository
	EventStore       domain.EventStore
	Dispatcher       *domain.EventDispatcher
	Logger           entities.Logger
	Behaviors        ResourceBehaviorRegistry
	BehaviorMeta     BehaviorMetaRegistry
	BehaviorSettings repositories.BehaviorSettingsRepository
}) ResourceService {
	return &resourceService{
		repo:             params.Repo,
		typeRepo:         params.TypeRepo,
		tripleRepo:       params.TripleRepo,
		permRepo:         params.PermRepo,
		accountRepo:      params.AccountRepo,
		eventStore:       params.EventStore,
		dispatcher:       params.Dispatcher,
		logger:           params.Logger,
		behaviors:        params.Behaviors,
		behaviorMeta:     params.BehaviorMeta,
		behaviorSettings: params.BehaviorSettings,
	}
}

func (s *resourceService) Create(
	ctx context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	ctx, err := enterResourceCall(ctx)
	if err != nil {
		return nil, err
	}
	rt, err := s.typeRepo.FindBySlug(ctx, cmd.TypeSlug)
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", cmd.TypeSlug, err)
	}
	if jsonld.IsAbstract(rt.Context()) {
		return nil, fmt.Errorf("cannot create resource of abstract type %q: use a concrete subtype instead", cmd.TypeSlug)
	}

	behavior := s.behaviorFor(ctx, rt)

	data, err := behavior.BeforeCreate(ctx, cmd.Data, rt)
	if err != nil {
		return nil, fmt.Errorf("behavior BeforeCreate rejected: %w", err)
	}

	if err := validateAgainstSchema(rt.Schema(), data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	var createdBy, accountID string
	if ident := auth.AgentFromCtx(ctx); ident != nil {
		createdBy = ident.AgentID
		accountID = ident.ActiveAccountID
	}

	entityID := identity.NewResource(cmd.TypeSlug)
	refProps := ExtractReferenceProperties(rt.Schema(), rt.Context())

	// Strip reference properties from the data — resources are atomic.
	// References are recorded as Triple events on the entity for atomic UoW commit.
	strippedData, refs, err := ExtractAndStripReferences(data, refProps)
	if err != nil {
		return nil, fmt.Errorf("failed to strip references: %w", err)
	}

	graphData, err := BuildResourceGraph(strippedData, nil, entityID, rt.Name(), rt.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to build resource graph: %w", err)
	}

	entity, err := new(entities.Resource).With(entityID, cmd.TypeSlug, graphData, createdBy, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Record triple events on the entity so they commit in the same UoW.
	for _, ref := range refs {
		tripleEvent := entities.TripleCreated{}.With(entityID, ref.Predicate, ref.Object)
		if err := entity.RecordEvent(tripleEvent, tripleEvent.EventType()); err != nil {
			return nil, fmt.Errorf("failed to record triple event: %w", err)
		}
	}

	published := entities.ResourcePublished{}.With(cmd.TypeSlug)
	if err := entity.RecordEvent(published, published.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record resource published event: %w", err)
	}

	if err := behavior.BeforeCreateCommit(ctx, entity); err != nil {
		return nil, fmt.Errorf("behavior BeforeCreateCommit rejected: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource: %w", err)
	}

	if err := behavior.AfterCreate(ctx, entity); err != nil {
		s.logger.Error(ctx, "behavior AfterCreate failed", "id", entity.GetID(), "error", err)
	}

	s.logger.Info(ctx, "resource created", "id", entity.GetID(), "type", cmd.TypeSlug)
	return entity, nil
}

func (s *resourceService) buildVisibilityScope(ctx context.Context) *repositories.VisibilityScope {
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil
	}
	return &repositories.VisibilityScope{
		AgentID:   identity.AgentID,
		AccountID: identity.ActiveAccountID,
		IsAdmin:   false, // per-user scoping: lists always filter by creator + permissions
	}
}

func (s *resourceService) checkInstanceAccess(
	ctx context.Context, entity *entities.Resource, action string,
) error {
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil // system context (CLI/MCP) — allow
	}
	// Admin/owner bypass: only if the caller is admin/owner in the RESOURCE's account
	if entity.AccountID() != "" {
		role, _ := s.accountRepo.FindMemberRole(ctx, entity.AccountID(), identity.AgentID)
		if role == "admin" || role == "owner" {
			return nil
		}
	}
	// Creator access
	if entity.CreatedBy() == identity.AgentID {
		return nil
	}
	// Explicit permission grant
	if has, _ := s.permRepo.HasPermission(ctx, entity.GetID(), identity.AgentID, action); has {
		return nil
	}
	// Backward compatibility: pre-migration resources with no owner
	if entity.CreatedBy() == "" {
		return nil
	}
	return entities.ErrAccessDenied
}

func (s *resourceService) GetByID(
	ctx context.Context, id string,
) (*entities.Resource, error) {
	entity, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkInstanceAccess(ctx, entity, "read"); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *resourceService) List(
	ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return s.repo.FindAllByType(ctx, typeSlug, cursor, limit, sort, s.buildVisibilityScope(ctx))
}

func (s *resourceService) ListByField(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	items, err := s.repo.FindAllByTypeAndField(ctx, typeSlug, fieldName, fieldValue)
	if err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{}, err
	}
	return repositories.PaginatedResponse[*entities.Resource]{
		Data:    items,
		HasMore: false,
	}, nil
}

func (s *resourceService) ListFlat(
	ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	return s.repo.FindAllByTypeFlat(ctx, typeSlug, cursor, limit, sort, s.buildVisibilityScope(ctx))
}

func (s *resourceService) ListFlatWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	scope := s.buildVisibilityScope(ctx)
	return s.repo.FindAllByTypeFlatWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

func (s *resourceService) ListWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	scope := s.buildVisibilityScope(ctx)
	return s.repo.FindAllByTypeWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

func (s *resourceService) Update(
	ctx context.Context, cmd UpdateResourceCommand,
) (*entities.Resource, error) {
	ctx, err := enterResourceCall(ctx)
	if err != nil {
		return nil, err
	}
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	if err := s.checkInstanceAccess(ctx, entity, "modify"); err != nil {
		return nil, err
	}

	rt, err := s.typeRepo.FindBySlug(ctx, entity.TypeSlug())
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", entity.TypeSlug(), err)
	}

	behavior := s.behaviorFor(ctx, rt)

	data, err := behavior.BeforeUpdate(ctx, entity, cmd.Data, rt)
	if err != nil {
		return nil, fmt.Errorf("behavior BeforeUpdate rejected: %w", err)
	}

	if err := validateAgainstSchema(rt.Schema(), data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	refProps := ExtractReferenceProperties(rt.Schema(), rt.Context())

	// Strip reference properties — resources are atomic.
	strippedData, newRefs, err := ExtractAndStripReferences(data, refProps)
	if err != nil {
		return nil, fmt.Errorf("failed to strip references: %w", err)
	}

	graphData, err := BuildResourceGraph(strippedData, nil, entity.GetID(), rt.Name(), rt.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to build resource graph: %w", err)
	}

	if err := entity.Update(graphData); err != nil {
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}

	if err := s.reconcileTriples(ctx, entity, refProps, newRefs); err != nil {
		return nil, err
	}

	published := entities.ResourcePublished{}.With(entity.TypeSlug())
	if err := entity.RecordEvent(published, published.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record resource published event: %w", err)
	}

	if err := behavior.BeforeUpdateCommit(ctx, entity); err != nil {
		return nil, fmt.Errorf("behavior BeforeUpdateCommit rejected: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource update: %w", err)
	}

	if err := behavior.AfterUpdate(ctx, entity); err != nil {
		s.logger.Error(ctx, "behavior AfterUpdate failed", "id", entity.GetID(), "error", err)
	}

	s.logger.Info(ctx, "resource updated", "id", entity.GetID())
	return entity, nil
}

func (s *resourceService) Delete(
	ctx context.Context, cmd DeleteResourceCommand,
) error {
	ctx, err := enterResourceCall(ctx)
	if err != nil {
		return err
	}
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}

	if err := s.checkInstanceAccess(ctx, entity, "delete"); err != nil {
		return err
	}

	rt, rtErr := s.typeRepo.FindBySlug(ctx, entity.TypeSlug())
	if rtErr != nil {
		s.logger.Warn(ctx, "resource type not found for delete behavior",
			"type", entity.TypeSlug(), "error", rtErr)
	}
	behavior := s.behaviorFor(ctx, rt)

	if err := behavior.BeforeDelete(ctx, entity); err != nil {
		return fmt.Errorf("behavior BeforeDelete rejected: %w", err)
	}

	if err := entity.MarkDeleted(); err != nil {
		return fmt.Errorf("failed to mark resource deleted: %w", err)
	}

	// Record TripleDeleted events for all existing triples on this resource.
	existing, err := s.tripleRepo.FindBySubject(ctx, entity.GetID())
	if err != nil {
		return fmt.Errorf("failed to load triples for deletion cleanup: %w", err)
	}
	for _, t := range existing {
		ev := entities.TripleDeleted{}.With(entity.GetID(), t.Predicate, t.Object)
		if err := entity.RecordEvent(ev, ev.EventType()); err != nil {
			return fmt.Errorf("failed to record triple deleted event: %w", err)
		}
	}

	published := entities.ResourcePublished{}.With(entity.TypeSlug())
	if err := entity.RecordEvent(published, published.EventType()); err != nil {
		return fmt.Errorf("failed to record resource published event: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit resource deletion: %w", err)
	}

	if err := behavior.AfterDelete(ctx, entity); err != nil {
		s.logger.Error(ctx, "behavior AfterDelete failed", "id", entity.GetID(), "error", err)
	}

	s.logger.Info(ctx, "resource deleted", "id", cmd.ID)
	return nil
}

// reconcileTriples diffs existing triples against new references and records
// TripleCreated/TripleDeleted events on the entity for atomic UoW commit.
func (s *resourceService) reconcileTriples(
	ctx context.Context,
	entity *entities.Resource,
	refProps []ReferencePropertyDef,
	newRefs []repositories.Triple,
) error {
	existing, err := s.tripleRepo.FindBySubject(ctx, entity.GetID())
	if err != nil {
		return fmt.Errorf("failed to load existing triples for reconciliation: %w", err)
	}
	schemaPredicates := make(map[string]bool, len(refProps))
	for _, rp := range refProps {
		schemaPredicates[rp.PredicateIRI] = true
	}

	newSet := make(map[string]bool, len(newRefs))
	for _, ref := range newRefs {
		newSet[ref.Predicate+"|"+ref.Object] = true
	}
	existingSet := make(map[string]bool, len(existing))
	for _, t := range existing {
		existingSet[t.Predicate+"|"+t.Object] = true
	}

	for _, t := range existing {
		if !schemaPredicates[t.Predicate] {
			continue
		}
		if !newSet[t.Predicate+"|"+t.Object] {
			ev := entities.TripleDeleted{}.With(entity.GetID(), t.Predicate, t.Object)
			if err := entity.RecordEvent(ev, ev.EventType()); err != nil {
				return fmt.Errorf("failed to record triple deleted event: %w", err)
			}
		}
	}
	for _, ref := range newRefs {
		if !existingSet[ref.Predicate+"|"+ref.Object] {
			ev := entities.TripleCreated{}.With(entity.GetID(), ref.Predicate, ref.Object)
			if err := entity.RecordEvent(ev, ev.EventType()); err != nil {
				return fmt.Errorf("failed to record triple created event: %w", err)
			}
		}
	}
	return nil
}

func validateAgainstSchema(schema, data json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}

	var schemaDoc any
	if err := json.Unmarshal(schema, &schemaDoc); err != nil {
		return fmt.Errorf("invalid schema JSON: %w", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schemaDoc); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("invalid JSON data: %w", err)
	}
	return sch.Validate(v)
}
