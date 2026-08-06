package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/jsonld"
	"github.com/wepala/weos/v3/pkg/utils"

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
	ReconcilePresetSchemas(ctx context.Context, presetName string) (*ReconcilePresetResult, error)
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
	linkActivator    *LinkActivator
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
	LinkActivator    *LinkActivator `optional:"true"`
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
		linkActivator:    params.LinkActivator,
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
			// Skip the Update call when nothing actually changed — keeps repeated
			// `--update` runs (and eventually restart-time auto-installs) from
			// emitting a ResourceTypeUpdated event per type on every boot.
			if presetMatchesResourceType(existing, pt) {
				result.Unchanged = append(result.Unchanged, pt.Slug)
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
	// Reconcile any link definitions whose endpoints are now both present. This
	// runs unconditionally because a newly installed type in *this* preset may
	// also complete a link declared by a *different* preset (e.g. installing
	// `education` can activate a finance→education link declared by a third
	// integration package).
	//
	// A reconcile error doesn't roll back the install — the resource types are
	// already persisted — but it does mean link-declared FK columns are
	// missing. Record it on the result so API/CLI callers can surface the
	// partial success instead of claiming the install is fully green.
	if s.linkActivator != nil {
		if rErr := s.linkActivator.Reconcile(ctx); rErr != nil {
			s.logger.Error(ctx, "link reconciliation after InstallPreset failed",
				"preset", presetName, "error", rErr)
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("link reconciliation failed: %s", rErr.Error()))
		}
	}
	return result, nil
}

// ReconcilePresetSchemas merges additive schema changes from a preset's code
// definition into the resource types already stored in this database (issue
// #379).
//
// This exists because InstallPreset can't do the job at startup. With
// update=false it skips an existing type entirely, so a preset that gains a
// property in a new build never reaches an already-provisioned database — the
// stored schema stays stale, EnsureTable derives the old column set from it,
// and every write to the new field is silently dropped by dropMissingColumns.
// With update=true it overwrites Name, Description, Context and Schema from
// code on every boot, which would silently discard any operator customization
// of a built-in type. Neither is acceptable unattended, so startup gets this
// third, narrower behavior instead.
//
// Only the schema is touched, and only additively (see reconcileAdditiveSchema
// for the merge rules). Name, Description, Context and Status are read back
// from the stored type and passed through unchanged. The explicit
// `resource-type preset install --update` path keeps its full-overwrite
// semantics — there an operator asked for it.
//
// A type whose preset schema diverged non-additively is logged and recorded in
// Refused, never rewritten; a type with no delta emits no event, which is what
// keeps repeated restarts from appending a ResourceTypeUpdated per type per
// boot.
//
// Per-type failures never abort the pass. One unparseable stored schema or one
// transient write error must not leave every LATER type in the preset
// unreconciled and silently dropping writes — which is the very defect this
// exists to end — so failures are collected in Failed and the loop continues.
func (s *resourceTypeService) ReconcilePresetSchemas(
	ctx context.Context, presetName string,
) (*ReconcilePresetResult, error) {
	preset, ok := s.registry.Get(presetName)
	if !ok {
		return nil, fmt.Errorf("unknown preset %q", presetName)
	}
	result := &ReconcilePresetResult{}
	for _, pt := range preset.Types {
		// A preset type with no schema projects only base columns, so every data
		// field is dropped on write. That is almost always a packaging mistake
		// and never a healthy no-op, so it gets its own category instead of
		// being folded into Unchanged where nothing would ever report it.
		if len(pt.Schema) == 0 {
			result.NoSchema = append(result.NoSchema, pt.Slug)
			continue
		}
		existing, err := s.GetBySlug(ctx, pt.Slug)
		if err != nil {
			// Not installed here: InstallPreset's create path owns that case.
			if errors.Is(err, repositories.ErrNotFound) {
				continue
			}
			s.recordReconcileFailure(ctx, result, presetName, pt.Slug,
				fmt.Errorf("failed to look up resource type: %w", err))
			continue
		}
		s.reconcileOneType(ctx, result, presetName, pt, existing)
	}
	return result, nil
}

// reconcileOneType applies the additive merge to a single installed type,
// recording the outcome on result. It never returns an error: every outcome is
// a category on the result so one type can't mask another.
func (s *resourceTypeService) reconcileOneType(
	ctx context.Context, result *ReconcilePresetResult,
	presetName string, pt PresetResourceType, existing *entities.ResourceType,
) {
	rec, err := reconcileAdditiveSchema(existing.Schema(), pt.Schema)
	if err != nil {
		s.recordReconcileFailure(ctx, result, presetName, pt.Slug,
			fmt.Errorf("failed to reconcile schema: %w", err))
		return
	}
	if len(rec.Conflicts) > 0 {
		// Held, not applied — and the rest of the merge still proceeds, so one
		// changed property definition can't block every additive property beside
		// it. Report which properties are stuck at their stored definition.
		s.logger.Warn(ctx,
			"preset property definitions diverge non-additively; holding them at their stored definition",
			"preset", presetName, "slug", pt.Slug, "heldProperties", rec.Conflicts)
		if result.Refused == nil {
			result.Refused = make(map[string][]string)
		}
		result.Refused[pt.Slug] = rec.Conflicts
	}
	if !rec.Changed {
		result.Unchanged = append(result.Unchanged, pt.Slug)
		return
	}
	if _, err := s.Update(ctx, UpdateResourceTypeCommand{
		ID:          existing.GetID(),
		Name:        existing.Name(),
		Slug:        existing.Slug(),
		Description: existing.Description(),
		Status:      existing.Status(),
		Context:     existing.Context(),
		Schema:      rec.Schema,
	}); err != nil {
		s.recordReconcileFailure(ctx, result, presetName, pt.Slug, err)
		return
	}
	// A successful Update is NOT evidence the column exists. The column is added
	// by the ResourceType.Updated handler, and SimpleUnitOfWork.Commit discards
	// dispatch errors as non-fatal (eventual-consistency model), so EnsureTable
	// can fail while Update still returns nil. Reporting "reconciled" on that
	// basis would announce success over exactly the silent drop this change
	// exists to end, so confirm the columns actually landed.
	if missing := s.missingColumns(pt.Slug, rec.Added); len(missing) > 0 {
		s.recordReconcileFailure(ctx, result, presetName, pt.Slug,
			fmt.Errorf("stored schema updated but projection columns are still missing: %v", missing))
		return
	}
	s.logger.Info(ctx, "reconciled preset schema into existing resource type",
		"preset", presetName, "slug", pt.Slug, "addedProperties", rec.Added)
	result.Updated = append(result.Updated, pt.Slug)
}

// missingColumns returns the subset of property names with no matching
// projection column, using the same camelCase→snake_case mapping the projection
// writer uses to decide which keys to keep.
func (s *resourceTypeService) missingColumns(slug string, properties []string) []string {
	if s.projMgr == nil {
		return nil
	}
	var missing []string
	for _, prop := range properties {
		if !s.projMgr.HasColumn(slug, utils.CamelToSnake(prop)) {
			missing = append(missing, prop)
		}
	}
	return missing
}

func (s *resourceTypeService) recordReconcileFailure(
	ctx context.Context, result *ReconcilePresetResult, presetName, slug string, err error,
) {
	s.logger.Error(ctx, "failed to reconcile preset schema into resource type",
		"preset", presetName, "slug", slug, "error", err)
	if result.Failed == nil {
		result.Failed = make(map[string]string)
	}
	result.Failed[slug] = err.Error()
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
