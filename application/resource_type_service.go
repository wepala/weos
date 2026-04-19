package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/jsonld"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// slugPattern enforces kebab-case identifiers: lowercase alphanumeric
// segments separated by single hyphens, max 64 characters. This prevents
// malformed slugs from reaching route registration or SQL DDL.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxSlugLen = 64

func validateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug must not be empty: %w", ErrValidation)
	}
	if len(slug) > maxSlugLen {
		return fmt.Errorf("slug must be at most %d characters: %w", maxSlugLen, ErrValidation)
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("slug %q must be lowercase kebab-case (a-z, 0-9, hyphens): %w", slug, ErrValidation)
	}
	if reservedSlugs[slug] {
		return fmt.Errorf("slug %q is reserved: %w", slug, ErrValidation)
	}
	return nil
}

// ErrValidation is returned for client-side validation failures (bad input).
var ErrValidation = errors.New("validation error")

// ErrForbidden is returned when the caller lacks required permissions.
var ErrForbidden = errors.New("forbidden")

var reservedSlugs = map[string]bool{
	"persons":        true,
	"organizations":  true,
	"health":         true,
	"resource-types": true,
	"websites":       true,
	"pages":          true,
	"sections":       true,
	"themes":         true,
	"templates":      true,
	"user":           true,
	"users":          true,
	"role":           true,
	"roles":          true,
	"account":        true,
	"accounts":       true,
	"auth":           true,
	"settings":       true,
	"admin":          true,
	"uploads":        true,
	"mcp":            true,
}

// ReservedResourceTypeSlugs returns the set of slugs that cannot be used as
// resource type identifiers because they conflict with API route prefixes or
// are reserved for dedicated domain entities (auth).
func ReservedResourceTypeSlugs() map[string]bool {
	cp := make(map[string]bool, len(reservedSlugs))
	for k, v := range reservedSlugs {
		cp[k] = v
	}
	return cp
}

// BehaviorInfo describes a behavior's state for a resource type within
// the caller's account context.
type BehaviorInfo struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Manageable  bool   `json:"manageable"`
}

type ResourceTypeService interface {
	Create(ctx context.Context, cmd CreateResourceTypeCommand) (*entities.ResourceType, error)
	GetByID(ctx context.Context, id string) (*entities.ResourceType, error)
	GetBySlug(ctx context.Context, slug string) (*entities.ResourceType, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.ResourceType], error)
	Update(ctx context.Context, cmd UpdateResourceTypeCommand) (*entities.ResourceType, error)
	Delete(ctx context.Context, cmd DeleteResourceTypeCommand) error
	ListPresets() []PresetDefinition
	InstallPreset(ctx context.Context, presetName string, update bool) (*InstallPresetResult, error)
	ListBehaviors(ctx context.Context, typeSlug string) ([]BehaviorInfo, error)
	SetBehaviors(ctx context.Context, typeSlug string, slugs []string) error
}

type resourceTypeService struct {
	repo             repositories.ResourceTypeRepository
	projMgr          repositories.ProjectionManager
	eventStore       domain.EventStore
	dispatcher       *domain.EventDispatcher
	registry         *PresetRegistry
	logger           entities.Logger
	resourceSvc      ResourceService
	behaviors        ResourceBehaviorRegistry
	behaviorMeta     BehaviorMetaRegistry
	behaviorSettings repositories.BehaviorSettingsRepository
	accountRepo      authrepos.AccountRepository
}

func ProvideResourceTypeService(params struct {
	fx.In
	Repo             repositories.ResourceTypeRepository
	ProjMgr          repositories.ProjectionManager
	EventStore       domain.EventStore
	Dispatcher       *domain.EventDispatcher
	Registry         *PresetRegistry
	Logger           entities.Logger
	ResourceSvc      ResourceService
	Behaviors        ResourceBehaviorRegistry
	BehaviorMeta     BehaviorMetaRegistry
	BehaviorSettings repositories.BehaviorSettingsRepository
	AccountRepo      authrepos.AccountRepository
}) ResourceTypeService {
	return &resourceTypeService{
		repo:             params.Repo,
		projMgr:          params.ProjMgr,
		eventStore:       params.EventStore,
		dispatcher:       params.Dispatcher,
		registry:         params.Registry,
		logger:           params.Logger,
		resourceSvc:      params.ResourceSvc,
		behaviors:        params.Behaviors,
		behaviorMeta:     params.BehaviorMeta,
		behaviorSettings: params.BehaviorSettings,
		accountRepo:      params.AccountRepo,
	}
}

func (s *resourceTypeService) Create(
	ctx context.Context, cmd CreateResourceTypeCommand,
) (*entities.ResourceType, error) {
	if err := validateSlug(cmd.Slug); err != nil {
		return nil, err
	}
	entity, err := new(entities.ResourceType).With(
		cmd.Name, cmd.Slug, cmd.Description, cmd.Context, cmd.Schema,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource type: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource type: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource type: %w", err)
	}

	s.logger.Info(ctx, "resource type created", "id", entity.GetID())
	return entity, nil
}

func (s *resourceTypeService) GetByID(
	ctx context.Context, id string,
) (*entities.ResourceType, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *resourceTypeService) GetBySlug(
	ctx context.Context, slug string,
) (*entities.ResourceType, error) {
	return s.repo.FindBySlug(ctx, slug)
}

func (s *resourceTypeService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *resourceTypeService) Update(
	ctx context.Context, cmd UpdateResourceTypeCommand,
) (*entities.ResourceType, error) {
	if err := validateSlug(cmd.Slug); err != nil {
		return nil, err
	}
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Update(
		cmd.Name, cmd.Slug, cmd.Description, cmd.Status, cmd.Context, cmd.Schema,
	); err != nil {
		return nil, fmt.Errorf("failed to update resource type: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource type: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource type update: %w", err)
	}

	s.logger.Info(ctx, "resource type updated", "id", entity.GetID())
	return entity, nil
}

func (s *resourceTypeService) Delete(
	ctx context.Context, cmd DeleteResourceTypeCommand,
) error {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}
	if err := entity.MarkDeleted(); err != nil {
		return fmt.Errorf("failed to mark resource type deleted: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return fmt.Errorf("failed to track resource type: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit resource type deletion: %w", err)
	}

	s.logger.Info(ctx, "resource type deleted", "id", cmd.ID)
	return nil
}

func (s *resourceTypeService) ListPresets() []PresetDefinition {
	return s.registry.List()
}

func (s *resourceTypeService) InstallPreset(
	ctx context.Context, presetName string, update bool,
) (*InstallPresetResult, error) {
	preset, ok := s.registry.Get(presetName)
	if !ok {
		return nil, fmt.Errorf("unknown preset %q", presetName)
	}
	result := &InstallPresetResult{}
	for _, pt := range preset.Types {
		existing, err := s.GetBySlug(ctx, pt.Slug)
		switch {
		case err == nil:
			if !update {
				result.Skipped = append(result.Skipped, pt.Slug)
				continue
			}
			_, uErr := s.Update(ctx, UpdateResourceTypeCommand{
				ID:          existing.GetID(),
				Name:        pt.Name,
				Slug:        pt.Slug,
				Description: pt.Description,
				Status:      existing.Status(),
				Context:     pt.Context,
				Schema:      pt.Schema,
			})
			if uErr != nil {
				return result, fmt.Errorf("failed to update resource type %q: %w", pt.Slug, uErr)
			}
			result.Updated = append(result.Updated, pt.Slug)
		case errors.Is(err, repositories.ErrNotFound):
			_, cErr := s.Create(ctx, CreateResourceTypeCommand{
				Name: pt.Name, Slug: pt.Slug, Description: pt.Description,
				Context: pt.Context, Schema: pt.Schema,
			})
			if cErr != nil {
				return result, fmt.Errorf("failed to create resource type %q: %w", pt.Slug, cErr)
			}
			result.Created = append(result.Created, pt.Slug)
			s.seedFixtures(ctx, pt, result)
		default:
			return result, fmt.Errorf("failed to look up resource type %q: %w", pt.Slug, err)
		}
	}
	return result, nil
}

// seedFixtures creates resources from the preset type's fixture data.
// Fixtures require a schema on the resource type for validation.
// Failures are logged but do not prevent the rest of the preset from installing.
// Built-in fixtures seeded at startup (via ensureBuiltInResourceTypes) use a
// background context and have no owner — they are intentionally global/public.
func (s *resourceTypeService) seedFixtures(
	ctx context.Context, pt PresetResourceType, result *InstallPresetResult,
) {
	if len(pt.Fixtures) == 0 {
		return
	}
	if len(pt.Schema) == 0 {
		s.logger.Error(ctx, "cannot seed fixtures without a schema", "slug", pt.Slug)
		return
	}
	if result.Seeded == nil {
		result.Seeded = make(map[string]int)
	}
	count := 0
	for i, fixture := range pt.Fixtures {
		// Schema validation is handled by ResourceService.Create.
		_, err := s.resourceSvc.Create(ctx, CreateResourceCommand{
			TypeSlug: pt.Slug,
			Data:     fixture,
		})
		if err != nil {
			s.logger.Error(ctx, "failed to seed fixture",
				"slug", pt.Slug, "index", i, "error", err)
			continue
		}
		count++
	}
	result.Seeded[pt.Slug] = count
	if count > 0 {
		s.logger.Info(ctx, "seeded fixture data", "slug", pt.Slug, "count", count)
	}
}

func (s *resourceTypeService) ListBehaviors(
	ctx context.Context, typeSlug string,
) ([]BehaviorInfo, error) {
	// Verify the resource type exists.
	rt, err := s.repo.FindBySlug(ctx, typeSlug)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, fmt.Errorf("resource type %q not found: %w", typeSlug, err)
		}
		return nil, fmt.Errorf("failed to load resource type %q: %w", typeSlug, err)
	}

	// Load account-level overrides (nil means use preset defaults).
	var overrides []string
	accountID := ""
	if ident := auth.AgentFromCtx(ctx); ident != nil {
		accountID = ident.ActiveAccountID
	}
	if accountID != "" && s.behaviorSettings != nil {
		overrides, err = s.behaviorSettings.GetByAccountAndType(
			ctx, accountID, typeSlug)
		if err != nil {
			return nil, fmt.Errorf("failed to load behavior settings: %w", err)
		}
	}

	// Walk the inheritance chain to collect all behaviors that apply.
	var infos []BehaviorInfo
	visited := map[string]bool{rt.Slug(): true}
	current := rt

	for current != nil {
		slug := current.Slug()
		if meta, ok := s.behaviorMeta[slug]; ok {
			enabled := meta.Default
			if meta.Manageable && overrides != nil {
				enabled = slugInList(slug, overrides)
			}
			infos = append(infos, BehaviorInfo{
				Slug:        slug,
				DisplayName: meta.DisplayName,
				Description: meta.Description,
				Enabled:     enabled,
				Manageable:  meta.Manageable,
			})
		} else if _, hasBehavior := s.behaviors[slug]; hasBehavior {
			infos = append(infos, BehaviorInfo{
				Slug:    slug,
				Enabled: true,
			})
		}
		parentSlug := jsonld.SubClassOf(current.Context())
		if parentSlug == "" || visited[parentSlug] {
			break
		}
		visited[parentSlug] = true
		parentRT, lookupErr := s.repo.FindBySlug(ctx, parentSlug)
		if lookupErr != nil {
			if errors.Is(lookupErr, repositories.ErrNotFound) {
				break
			}
			return nil, fmt.Errorf(
				"failed to load parent resource type %q for %q: %w",
				parentSlug, current.Slug(), lookupErr,
			)
		}
		current = parentRT
	}

	return infos, nil
}

func (s *resourceTypeService) SetBehaviors(
	ctx context.Context, typeSlug string, slugs []string,
) error {
	if err := s.requireAdmin(ctx); err != nil {
		return err
	}

	if s.behaviorSettings == nil {
		return fmt.Errorf("behavior settings not available")
	}

	applicable, err := s.applicableBehaviorSlugs(ctx, typeSlug)
	if err != nil {
		return err
	}

	if slugs == nil {
		slugs = []string{}
	}
	slugs = dedup(slugs)

	for _, slug := range slugs {
		if !applicable[slug] {
			return fmt.Errorf(
				"behavior %q does not apply to type %q: %w",
				slug, typeSlug, ErrValidation)
		}
		meta, ok := s.behaviorMeta[slug]
		if !ok {
			return fmt.Errorf(
				"behavior %q is not user-manageable: %w", slug, ErrValidation)
		}
		if !meta.Manageable {
			return fmt.Errorf(
				"behavior %q is not user-manageable: %w", slug, ErrValidation)
		}
	}

	accountID := ""
	if ident := auth.AgentFromCtx(ctx); ident != nil {
		accountID = ident.ActiveAccountID
	}
	if accountID == "" {
		return fmt.Errorf("account context required to set behaviors: %w", ErrForbidden)
	}

	return s.behaviorSettings.SaveByAccountAndType(
		ctx, accountID, typeSlug, slugs)
}

func (s *resourceTypeService) requireAdmin(ctx context.Context) error {
	ident := auth.AgentFromCtx(ctx)
	if ident == nil {
		return fmt.Errorf("authentication required: %w", ErrForbidden)
	}
	if ident.ActiveAccountID == "" {
		return fmt.Errorf("account context required: %w", ErrForbidden)
	}
	if s.accountRepo == nil {
		return fmt.Errorf("authorization not configured: %w", ErrForbidden)
	}
	role, err := s.accountRepo.FindMemberRole(
		ctx, ident.ActiveAccountID, ident.AgentID)
	if err != nil {
		return fmt.Errorf("failed to check admin status: %w", err)
	}
	if role != authentities.RoleAdmin && role != authentities.RoleOwner {
		return fmt.Errorf("admin role required: %w", ErrForbidden)
	}
	return nil
}

func (s *resourceTypeService) applicableBehaviorSlugs(
	ctx context.Context, typeSlug string,
) (map[string]bool, error) {
	rt, err := s.repo.FindBySlug(ctx, typeSlug)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, fmt.Errorf(
				"resource type %q not found: %w", typeSlug, err)
		}
		return nil, fmt.Errorf(
			"failed to look up resource type %q: %w", typeSlug, err)
	}

	allowed := make(map[string]bool)
	visited := map[string]bool{rt.Slug(): true}
	current := rt

	for current != nil {
		slug := current.Slug()
		if _, ok := s.behaviorMeta[slug]; ok {
			allowed[slug] = true
		} else if _, ok := s.behaviors[slug]; ok {
			allowed[slug] = true
		}
		parentSlug := jsonld.SubClassOf(current.Context())
		if parentSlug == "" || visited[parentSlug] {
			break
		}
		visited[parentSlug] = true
		parentRT, lookupErr := s.repo.FindBySlug(ctx, parentSlug)
		if lookupErr != nil {
			if errors.Is(lookupErr, repositories.ErrNotFound) {
				break
			}
			return nil, fmt.Errorf(
				"failed to look up parent type %q for %q: %w",
				parentSlug, typeSlug, lookupErr)
		}
		current = parentRT
	}

	return allowed, nil
}

func slugInList(slug string, list []string) bool {
	for _, s := range list {
		if s == slug {
			return true
		}
	}
	return false
}

func dedup(slugs []string) []string {
	seen := make(map[string]bool, len(slugs))
	out := make([]string, 0, len(slugs))
	for _, s := range slugs {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
