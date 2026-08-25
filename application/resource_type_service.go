package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

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
	HeldContextTerms(ctx context.Context, presetName, typeSlug string) ([]HeldTerm, error)
	AdoptContextTerms(ctx context.Context, presetName, typeSlug string, terms []string) (AdoptResult, error)
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
	linkRegistry     *LinkRegistry
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
	LinkRegistry     *LinkRegistry  `optional:"true"`
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
		linkRegistry:     params.LinkRegistry,
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
	// A type created here belongs to no preset, so the boot sweep never sees
	// it — and hand-authoring several ID fields onto one relation is exactly
	// how the shape gets built (issue #515). Report it now, while the operator
	// is looking at what they just defined.
	ReportAmbiguousReferenceShape(ctx, s.logger, cmd.Slug, cmd.Schema, cmd.Context, s.linksFor(cmd.Slug))

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
	// An edit can introduce the shape just as a create can — repointing one
	// property's term onto another's predicate is enough.
	ReportAmbiguousReferenceShape(ctx, s.logger, cmd.Slug, cmd.Schema, cmd.Context, s.linksFor(cmd.Slug))

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
// for the merge rules). Schema and `@context` are BOTH merged, and only
// additively (reconcileAdditiveSchema and reconcileAdditiveContext); each side
// falls back to the stored value when its own merge is a no-op, so a
// context-only change never rewrites the schema. Name, Description and Status
// are read back from the stored type and passed through unchanged. The explicit
// `resource-type preset install --update` path keeps its full-overwrite
// semantics — there an operator asked for it.
//
// A type whose preset schema or context diverged non-additively is logged and
// recorded in Refused or RefusedContext, never rewritten; a type with no delta emits no event, which is what
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
		existing, err := s.GetBySlug(ctx, pt.Slug)
		if err != nil {
			// Not installed here: InstallPreset's create path owns that case.
			// Checked BEFORE the schema check so a preset that isn't installed in
			// this database stays completely silent — the reconcile now runs for
			// every registered preset, so warning about types nobody installed
			// would be noise on every boot.
			if errors.Is(err, repositories.ErrNotFound) {
				continue
			}
			s.recordReconcileFailure(ctx, result, presetName, pt.Slug,
				fmt.Errorf("failed to look up resource type: %w", err))
			continue
		}
		// An INSTALLED type whose preset declares no schema projects only base
		// columns, so every data field is dropped on write. That is almost
		// always a packaging mistake and never a healthy no-op, so it gets its
		// own category instead of being folded into Unchanged where nothing
		// would ever report it.
		if len(pt.Schema) == 0 {
			result.NoSchema = append(result.NoSchema, pt.Slug)
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
	schemaRec, err := reconcileAdditiveSchema(existing.Schema(), pt.Schema)
	if err != nil {
		s.recordReconcileFailure(ctx, result, presetName, pt.Slug,
			fmt.Errorf("failed to reconcile schema: %w", err))
		return
	}
	// The context is merged by the same additive rules as the schema (issue
	// #510). Merging only the schema is what left a reference property with a
	// column, a declaration, and nowhere for its predicate to resolve — so its
	// writes went on being dropped while the reconcile reported success.
	contextRec, err := reconcileAdditiveContext(existing.Context(), pt.Context, existing.Schema())
	if err != nil {
		// A context this reconcile cannot parse must not take the schema merge
		// down with it (issue #513). JSON-LD permits an array and a remote-IRI
		// string, nothing validates the shape on the write path, and blocking
		// here left the type's new property without its projection column —
		// reintroducing, for that type, the silent drop #379 closed. Report it
		// and carry on with the stored context untouched.
		s.logger.Error(ctx, "cannot reconcile this type's @context; merging its schema only",
			"preset", presetName, "slug", pt.Slug, "error", err)
		if result.RefusedContext == nil {
			result.RefusedContext = make(map[string][]string)
		}
		if result.UnparseableContext == nil {
			result.UnparseableContext = make(map[string]string)
		}
		result.UnparseableContext[pt.Slug] = err.Error()
		contextRec = contextReconciliation{}
	}
	s.recordHeldDefinitions(ctx, result, presetName, pt.Slug, schemaRec, contextRec)
	// A redefined prefix the stored class expands through moves the class;
	// the sweep skips it, so the remedy must name it (issues #518, #521).
	for _, m := range conflictMoves(existing.Context(), pt.Context, existing.Schema(), contextRec.Conflicts) {
		if m.Property == "@type" && m.Term != "@type" {
			if result.ClassMovers == nil {
				result.ClassMovers = make(map[string][]string)
			}
			result.ClassMovers[pt.Slug] = append(result.ClassMovers[pt.Slug], m.Term)
		}
	}

	if !schemaRec.Changed && !contextRec.Changed {
		// Nothing to merge is not the same as nothing wrong. A reference with
		// no covering context term keeps dropping its writes on every boot, and
		// this is the branch a steady-state boot takes — so checking only after
		// an Update meant the alarm sounded once, on whichever boot happened to
		// change something, and then went quiet forever over ongoing loss.
		//
		// This is where it differs from missingColumns, which is checked only
		// after an Update: a missing column is transient and the next Update
		// retries it, whereas an uncovered reference is permanent and
		// guarantees there will be no next Update.
		if dropped := referencePropertiesWithoutContextEntry(
			existing.Schema(), existing.Context()); len(dropped) > 0 {
			s.recordReconcileFailure(ctx, result, presetName, pt.Slug,
				uncoveredReferencesError(dropped, pt.Schema))
			return
		}
		result.Unchanged = append(result.Unchanged, pt.Slug)
		return
	}

	// Each side falls back to what is stored when its own merge is a no-op, so
	// a context-only change never rewrites the schema and vice versa.
	schema := existing.Schema()
	if schemaRec.Changed {
		schema = schemaRec.Schema
	}
	ldContext := existing.Context()
	if contextRec.Changed {
		ldContext = contextRec.Context
	}

	if _, err := s.Update(ctx, UpdateResourceTypeCommand{
		ID:          existing.GetID(),
		Name:        existing.Name(),
		Slug:        existing.Slug(),
		Description: existing.Description(),
		Status:      existing.Status(),
		Context:     ldContext,
		Schema:      schema,
	}); err != nil {
		s.recordReconcileFailure(ctx, result, presetName, pt.Slug, err)
		return
	}
	if !s.confirmWritesLand(ctx, result, presetName, pt, schema, ldContext, schemaRec.Added) {
		return
	}
	s.logger.Info(ctx, "reconciled preset schema and context into existing resource type",
		"preset", presetName, "slug", pt.Slug,
		"addedProperties", schemaRec.Added, "addedContextTerms", contextRec.Added)
	result.Updated = append(result.Updated, pt.Slug)
}

// recordHeldDefinitions logs and records every definition held at its stored
// form, keeping schema properties and context terms in separate categories:
// they read the same on a boot line but need different operator fixes.
func (s *resourceTypeService) recordHeldDefinitions(
	ctx context.Context, result *ReconcilePresetResult, presetName, slug string,
	schemaRec schemaReconciliation, contextRec contextReconciliation,
) {
	if len(schemaRec.Conflicts) > 0 {
		// Held, not applied — and the rest of the merge still proceeds, so one
		// changed property definition can't block every additive property beside
		// it. Report which properties are stuck at their stored definition.
		s.logger.Warn(ctx,
			"preset property definitions diverge non-additively; holding them at their stored definition",
			"preset", presetName, "slug", slug, "heldProperties", schemaRec.Conflicts)
		if result.Refused == nil {
			result.Refused = make(map[string][]string)
		}
		result.Refused[slug] = schemaRec.Conflicts
	}
	if len(contextRec.Conflicts) > 0 {
		s.logger.Warn(ctx,
			"preset context terms diverge; holding them at their stored definition",
			"preset", presetName, "slug", slug, "heldContextTerms", contextRec.Conflicts)
		s.addRefusedContext(result, slug, contextRec.Conflicts...)
	}
	for _, moved := range contextRec.Moves {
		// Not a divergence — the stored context never had this term. Merging it
		// would repoint a predicate that already has edges, so it is held and
		// both IRIs are named: the operator needs a data migration, not a
		// choice between definitions.
		s.logger.Warn(ctx,
			"preset context term would repoint a predicate that already has data; holding it",
			"preset", presetName, "slug", slug, "term", moved.Term, "property", moved.Property,
			"storedIRI", moved.StoredIRI, "presetIRI", moved.PresetIRI)
		if result.Repointed == nil {
			result.Repointed = make(map[string][]string)
		}
		// One held TERM per report line, however many properties it moves.
		result.Repointed[slug] = appendMissing(result.Repointed[slug], moved.Term)
		if moved.Property == "@type" && moved.Term != "@type" {
			if result.ClassMovers == nil {
				result.ClassMovers = make(map[string][]string)
			}
			result.ClassMovers[slug] = appendMissing(result.ClassMovers[slug], moved.Term)
		}
	}
}

// confirmWritesLand verifies, after a successful Update, that writes to the
// reconciled type's properties can actually land. It reports whether the type
// may be counted as Updated.
//
// A successful Update is NOT evidence of either half. The column is added by
// the ResourceType.Updated handler, and SimpleUnitOfWork.Commit discards
// dispatch errors as non-fatal (eventual-consistency model), so EnsureTable can
// fail while Update still returns nil. A reference property needs a second
// thing the Update cannot vouch for: an explicit context term to key its edge
// by. Reporting "reconciled" without checking both would announce success over
// exactly the silent drop this exists to end.
//
// Both halves are reported together rather than short-circuiting, because
// Failed carries one reason per slug and an operator fixing a type wants to see
// everything wrong with it at once.
func (s *resourceTypeService) confirmWritesLand(
	ctx context.Context, result *ReconcilePresetResult,
	presetName string, pt PresetResourceType,
	schema, ldContext json.RawMessage, addedProperties []string,
) bool {
	var reasons []string
	if missing := s.missingColumns(pt.Slug, addedProperties); len(missing) > 0 {
		reasons = append(reasons,
			fmt.Sprintf("stored schema updated but projection columns are still missing: %v", missing))
	}
	// A reference the preset's own context never declares cannot be fixed by an
	// additive merge — nothing exists to merge in. That is a preset packaging
	// mistake, and naming it is the only way an operator learns those writes are
	// being dropped.
	if dropped := referencePropertiesWithoutContextEntry(schema, ldContext); len(dropped) > 0 {
		reasons = append(reasons, uncoveredReferencesError(dropped, pt.Schema).Error())
	}
	if len(reasons) == 0 {
		return true
	}
	s.recordReconcileFailure(ctx, result, presetName, pt.Slug, errors.New(strings.Join(reasons, "; ")))
	return false
}

// columnlessProperties are schema property names that deliberately yield no
// projection column of their own, so their absence is correct rather than a
// failed migration. This mirrors schemaToColumns in the gorm projection
// manager, which skips the JSON-LD meta-keys outright and skips any property
// whose snake_case name collides with a standard column. Only `data` is
// enumerated from that second set: every other standard column IS cached as
// present by EnsureTable, so a property named `status` or `createdAt` — both of
// which real presets declare — verifies correctly without special-casing.
var columnlessProperties = map[string]bool{
	"@id":      true,
	"@type":    true,
	"@context": true,
	"data":     true, // lives on the canonical resources row, not the projection
}

// missingColumns returns the subset of property names with no matching
// projection column, using the same camelCase→snake_case mapping the projection
// writer uses to decide which keys to keep.
//
// Properties that never map to a column are skipped: reporting them would raise
// a false alarm announcing data loss that isn't happening.
func (s *resourceTypeService) missingColumns(slug string, properties []string) []string {
	if s.projMgr == nil {
		return nil
	}
	var missing []string
	for _, prop := range properties {
		if columnlessProperties[prop] {
			continue
		}
		column := utils.CamelToSnake(prop)
		if columnlessProperties[column] {
			continue
		}
		if !s.projMgr.HasColumn(slug, column) {
			missing = append(missing, prop)
		}
	}
	return missing
}

func (s *resourceTypeService) recordReconcileFailure(
	ctx context.Context, result *ReconcilePresetResult, presetName, slug string, err error,
) {
	// Deliberately not "schema": this records every way a reconcile can fail —
	// an unparseable schema OR context, a failed Update, a projection column
	// that never appeared, and a reference property left with no context term.
	// Naming only the schema would send an operator to the wrong half.
	s.logger.Error(ctx, "failed to reconcile preset into resource type",
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

// addRefusedContext records held context terms without clobbering terms already
// recorded for the same slug — divergences and repointings both land here.
func (s *resourceTypeService) addRefusedContext(
	result *ReconcilePresetResult, slug string, terms ...string,
) {
	if len(terms) == 0 {
		return
	}
	if result.RefusedContext == nil {
		result.RefusedContext = make(map[string][]string)
	}
	result.RefusedContext[slug] = append(result.RefusedContext[slug], terms...)
}

// HeldTermKind says why the boot holds a term, because the operator's
// decision differs: an ADDED term needs a data migration before its new IRI
// can be taken; a REDEFINED term needs a choice between the stored
// definition and the preset's.
type HeldTermKind string

const (
	// HeldTermAdded — the stored context never had the term, and adding it
	// would repoint a predicate that already has data (issue #513).
	HeldTermAdded HeldTermKind = "added"
	// HeldTermRedefined — the term exists in both with different
	// definitions; a namespace rename or a corrected prefix (issue #518).
	HeldTermRedefined HeldTermKind = "redefined"
)

// HeldMove is one property whose predicate adopting a held term moves. A
// property term moves itself; a prefix moves every property that expands
// through it; `@type` as the Property means the type's class moves.
type HeldMove struct {
	Property  string `json:"property"`
	StoredIRI string `json:"storedIri"`
	PresetIRI string `json:"presetIri"`
}

// HeldTerm describes one `@context` term the boot reconcile refuses to adopt,
// so an operator can see the decision before making it.
type HeldTerm struct {
	// Term is the context key the preset declares and the boot will not apply.
	Term string `json:"term"`
	// Kind says whether the preset adds the term or redefines it.
	Kind HeldTermKind `json:"kind"`
	// Property names the first thing that would move — usually Term itself,
	// or the stored term that expands through a held prefix. Moves lists all
	// of them.
	Property string `json:"property"`
	// StoredIRI is the IRI existing edges are keyed by (for a prefix or
	// @vocab, the stored namespace).
	StoredIRI string `json:"storedIri"`
	// PresetIRI is what the preset wants the term to resolve to.
	PresetIRI string `json:"presetIri"`
	// Moves is every property (and the class, as "@type") the term moves.
	Moves []HeldMove `json:"moves"`
}

// MovesClass reports whether adopting the term moves the type's RDF class.
func (h HeldTerm) MovesClass() bool {
	for _, m := range h.Moves {
		if m.Property == "@type" {
			return true
		}
	}
	return false
}

// AdoptResult is what AdoptContextTerms did.
type AdoptResult struct {
	// Adopted lists the terms taken, in order.
	Adopted []string `json:"adopted"`
	// ClassMove is set when an adopted term moved the type's RDF class: no
	// alias can follow a class, so existing records need a re-stamp.
	ClassMove *HeldMove `json:"classMove,omitempty"`
	// StillHeld lists the class-moving terms a sweep deliberately left.
	StillHeld []string `json:"stillHeld,omitempty"`
}

// NeedsRestamp reports whether existing records must be re-stamped for the
// adoption to reach them.
func (r AdoptResult) NeedsRestamp() bool { return r.ClassMove != nil }

// HeldContextTerms reports what the boot is refusing to adopt for one type,
// so an operator can see the decision before making it: ADDED terms (Moves)
// and REDEFINED terms (Conflicts), each with everything adopting it moves.
func (s *resourceTypeService) HeldContextTerms(
	ctx context.Context, presetName, typeSlug string,
) ([]HeldTerm, error) {
	pt, existing, err := s.presetTypeAndStored(ctx, presetName, typeSlug)
	if err != nil {
		return nil, err
	}
	rec, err := reconcileAdditiveContext(existing.Context(), pt.Context, existing.Schema())
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile context for %q: %w", typeSlug, err)
	}
	return heldTermsOf(rec, existing.Context(), pt.Context, existing.Schema()), nil
}

// heldTermsOf groups the reconcile's Moves and Conflicts by term.
func heldTermsOf(rec contextReconciliation, stored, preset, schema json.RawMessage) []HeldTerm {
	byTerm := map[string]*HeldTerm{}
	var order []string
	add := func(kind HeldTermKind, m movedPredicate) {
		h := byTerm[m.Term]
		if h == nil {
			h = &HeldTerm{Term: m.Term, Kind: kind, Property: m.Property, StoredIRI: m.StoredIRI, PresetIRI: m.PresetIRI}
			byTerm[m.Term] = h
			order = append(order, m.Term)
		}
		h.Moves = append(h.Moves, HeldMove{Property: m.Property, StoredIRI: m.StoredIRI, PresetIRI: m.PresetIRI})
	}
	for _, m := range rec.Moves {
		add(HeldTermAdded, m)
	}
	conflicts := conflictMoves(stored, preset, schema, rec.Conflicts)
	for _, m := range conflicts {
		add(HeldTermRedefined, m)
	}
	// A redefined prefix or @vocab reports its own definitions, not the
	// first property's: that is what the operator is choosing between.
	storedTerms, _ := splitContext(stored)
	presetTerms, _ := splitContext(preset)
	for _, term := range rec.Conflicts {
		h := byTerm[term]
		if h != nil && len(h.Moves) > 0 && (term == "@vocab" || len(h.Moves) > 1 || h.Moves[0].Property != term) {
			h.StoredIRI, h.PresetIRI = rawTermValue(storedTerms[term]), rawTermValue(presetTerms[term])
		}
	}
	sort.Strings(order)
	out := make([]HeldTerm, 0, len(order))
	for _, term := range order {
		out = append(out, *byTerm[term])
	}
	return out
}

// AdoptContextTerms takes the preset's definition for HELD terms — added or
// redefined — recording the IRI each affected property resolves to today so
// existing edges keep resolving. An empty terms list adopts every held term
// EXCEPT the class-moving ones (`@type`, a prefix the class expands through)
// and `@vocab`, which repoints every untermed property at once: those must
// be named.
//
// A named term the boot never held is REFUSED. Recording an alias for an IRI
// no data was ever written under widens the reverse map for nothing, and a
// mistyped term name would otherwise report success while changing nothing
// the operator meant.
//
// Projection columns are NOT repopulated here: the alias makes existing
// edges resolvable, and a reproject is what rewrites the rows. A moved
// class needs `normalize-edge-keys --restamp` before that reproject.
func (s *resourceTypeService) AdoptContextTerms(
	ctx context.Context, presetName, typeSlug string, terms []string,
) (AdoptResult, error) {
	pt, existing, err := s.presetTypeAndStored(ctx, presetName, typeSlug)
	if err != nil {
		return AdoptResult{}, err
	}
	rec, err := reconcileAdditiveContext(existing.Context(), pt.Context, existing.Schema())
	if err != nil {
		return AdoptResult{}, fmt.Errorf("failed to reconcile context for %q: %w", typeSlug, err)
	}
	held := append(append([]movedPredicate{}, rec.Moves...),
		conflictMoves(existing.Context(), pt.Context, existing.Schema(), rec.Conflicts)...)

	selected, stillHeld, err := selectTermsToAdopt(held, terms, existing.Context(), pt.Context, typeSlug)
	result := AdoptResult{StillHeld: stillHeld}
	if err != nil || len(selected) == 0 {
		return result, err
	}

	adoptedContext, adopted, err := adoptTerms(existing.Context(), pt.Context, selected)
	if err != nil {
		return result, err
	}
	if len(adopted) == 0 {
		return result, nil // already adopted; running this twice changes nothing
	}
	if _, err := s.Update(ctx, UpdateResourceTypeCommand{
		ID:          existing.GetID(),
		Name:        existing.Name(),
		Slug:        existing.Slug(),
		Description: existing.Description(),
		Status:      existing.Status(),
		Context:     adoptedContext,
		Schema:      existing.Schema(),
	}); err != nil {
		return result, fmt.Errorf("failed to store the adopted context for %q: %w", typeSlug, err)
	}
	result.Adopted = adopted
	// The class moved only if a class-moving term was actually APPLIED: a
	// selected term adoptTerms skipped as already equivalent moved nothing,
	// and sending the operator to a re-stamp for it would be wrong.
	applied := map[string]bool{}
	for _, term := range adopted {
		applied[term] = true
	}
	for _, m := range selected {
		if m.Property == "@type" && applied[m.Term] {
			move := HeldMove{Property: "@type", StoredIRI: m.StoredIRI, PresetIRI: m.PresetIRI}
			result.ClassMove = &move
			break
		}
	}
	s.logger.Info(ctx, "adopted preset context terms; previous IRIs recorded as aliases",
		"preset", presetName, "slug", typeSlug, "adopted", adopted)
	return result, nil
}

// selectTermsToAdopt turns the caller's request into the held moves to apply.
//
// An empty request is a sweep: every held term EXCEPT the ones that move
// the class (`@type`, or a prefix the class expands through) and `@vocab`.
// An alias makes an old edge IRI resolve; it cannot do the same for a type's
// RDF class, which is not a predicate — adopting it in a sweep would leave
// resources written before the boot in one class and those after it in
// another, with nothing able to reconcile them. `@vocab` repoints every
// untermed property at once. Both must be named, and the terms a sweep
// left are returned so the operator is told.
//
// A named term that is not held is REFUSED, unless the stored context
// already matches the preset — that is the second run of the same command,
// which must change nothing rather than fail.
func selectTermsToAdopt(
	held []movedPredicate, requested []string, stored, preset json.RawMessage, typeSlug string,
) (selected []movedPredicate, stillHeld []string, err error) {
	movesClass := map[string]bool{}
	for _, m := range held {
		if m.Property == "@type" {
			movesClass[m.Term] = true
		}
	}
	storedTerms, sErr := splitContext(stored)
	presetTerms, pErr := splitContext(preset)
	if len(requested) == 0 {
		blocked := map[string]bool{}
		for _, m := range held {
			if m.Term == "@type" || m.Term == "@vocab" || movesClass[m.Term] {
				blocked[m.Term] = true
			}
		}
		// A term written against a prefix the sweep leaves held cannot be
		// taken either: merged without its prefix it would resolve through
		// @vocab to an IRI nobody meant. It waits for the prefix.
		for changed := true; changed && pErr == nil; {
			changed = false
			for _, m := range held {
				prefix := prefixOf(presetTerms[m.Term])
				_, stored := storedTerms[prefix]
				if !blocked[m.Term] && prefix != "" && blocked[prefix] && !stored {
					blocked[m.Term] = true
					changed = true
				}
			}
		}
		seenHeld := map[string]bool{}
		for _, m := range held {
			if blocked[m.Term] {
				if !seenHeld[m.Term] {
					seenHeld[m.Term] = true
					stillHeld = append(stillHeld, m.Term)
				}
				continue
			}
			selected = append(selected, m)
		}
		sort.Strings(stillHeld)
		return selected, stillHeld, nil
	}

	for _, term := range requested {
		var forTerm []movedPredicate
		for _, m := range held {
			if m.Term == term {
				forTerm = append(forTerm, m)
			}
		}
		if len(forTerm) > 0 {
			selected = append(selected, forTerm...)
			continue
		}
		// Already adopted on an earlier run, or never held at all? The stored
		// context matches the preset either way, so matching alone cannot tell
		// them apart. The adopted-terms record can, and it names the TERM —
		// unlike the aliases, which a prefix records against the properties it
		// moves rather than against itself.
		alreadyAdopted := false
		for _, done := range jsonld.AdoptedTerms(stored) {
			if done == term {
				alreadyAdopted = true
				break
			}
		}
		if alreadyAdopted && sErr == nil && pErr == nil &&
			jsonEquivalent(storedTerms[term], presetTerms[term]) && len(presetTerms[term]) > 0 {
			continue
		}
		return nil, nil, fmt.Errorf(
			"the boot is not holding a %q term for %q, so there is nothing to adopt", term, typeSlug)
	}
	return selected, nil, nil
}

// presetTypeAndStored resolves a preset's declaration of a type alongside what
// is stored for it, which both adoption paths need. The preset is named because
// the IRI being adopted comes from ITS declaration.
func (s *resourceTypeService) presetTypeAndStored(
	ctx context.Context, presetName, typeSlug string,
) (PresetResourceType, *entities.ResourceType, error) {
	preset, ok := s.registry.Get(presetName)
	if !ok {
		return PresetResourceType{}, nil, fmt.Errorf("unknown preset %q", presetName)
	}
	for _, pt := range preset.Types {
		if pt.Slug != typeSlug {
			continue
		}
		existing, err := s.GetBySlug(ctx, typeSlug)
		if err != nil {
			return PresetResourceType{}, nil, fmt.Errorf(
				"failed to load resource type %q: %w", typeSlug, err)
		}
		return pt, existing, nil
	}
	return PresetResourceType{}, nil, fmt.Errorf("preset %q declares no %q type", presetName, typeSlug)
}

// uncoveredReferencesError phrases the dropped-writes report so it names the
// right party.
//
// The check walks the STORED schema, which includes reference properties an
// operator added through the API — the resource-type write path stores a schema
// and context verbatim and derives no term, so such a property is uncovered by
// construction. Calling that "a preset packaging mistake" sends the operator to
// look at a preset they never touched.
func uncoveredReferencesError(dropped []string, presetSchema json.RawMessage) error {
	presetDeclares := map[string]bool{}
	for _, ref := range ExtractReferenceProperties(presetSchema, nil) {
		presetDeclares[ref.PropertyName] = true
	}
	var fromPreset, fromOperator []string
	for _, name := range dropped {
		if presetDeclares[name] {
			fromPreset = append(fromPreset, name)
			continue
		}
		fromOperator = append(fromOperator, name)
	}
	switch {
	case len(fromPreset) > 0 && len(fromOperator) > 0:
		return fmt.Errorf(
			"reference properties have no @context entry, so their writes are dropped — "+
				"declared by the preset: %v; added through the API: %v", fromPreset, fromOperator)
	case len(fromOperator) > 0:
		return fmt.Errorf(
			"reference properties added through the API have no @context entry, so their writes "+
				"are dropped: %v — add a term mapping each to its predicate IRI", fromOperator)
	default:
		return fmt.Errorf(
			"reference properties the preset declares have no @context entry in the preset, so "+
				"their writes are dropped: %v", fromPreset)
	}
}

// linksFor returns the links declared against a type, so the ambiguity check
// sees the same reference set the WRITE path does. A link-declared reference
// is stored and read exactly like a schema-declared one, so two of them — or
// one of each — sharing a predicate and a target type are just as
// indistinguishable, and a schema-only check would never say so.
func (s *resourceTypeService) linksFor(slug string) []PresetLinkDefinition {
	if s.linkRegistry == nil {
		return nil
	}
	return s.linkRegistry.BySource(slug)
}
