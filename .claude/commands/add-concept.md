# Add Concept: $ARGUMENTS

You are adding a new domain concept to the WeOS project. This skill creates a **full vertical slice** — from ontology research through entity, events, GORM model, repository, service, API handler, CLI command, DI wiring, tests, and NuxtJS admin UI.

---

## Phase 1: Discovery & Ontology

### Step 1: Parse the Concept Name

The concept name is: **$ARGUMENTS**

If `$ARGUMENTS` is empty or vague (e.g., "thing", "stuff"), ask the user:
> "What domain concept would you like to add? Examples: Location, Event, Ministry, Product, Service, Donation"

### Step 2: Ask About Domain Context

Ask the user:
> "What kind of organization is this for? (e.g., church, restaurant, nonprofit, school, small business) This helps me pick the right ontology and properties."

### Step 3: Explore Existing Project State

Read these files to understand the current project:

- `go.mod` — module path is `weos`
- `domain/entities/*.go` — existing entities
- `domain/entities/*_events.go` — existing predicates
- `pkg/identity/identity.go` — existing type constants and URN constructors. Website: `identity.NewWebsite(slug)`, Page: `identity.NewPage(websiteSlug, pageSlug)`, Section: `identity.NewSection(websiteSlug, pageSlug)`. Child URNs embed the parent's slug for scoping. For standalone entities not scoped to a website, a simpler format like `urn:<type>:<ksuid>` may be appropriate
- `application/module.go` — current DI registrations
- `infrastructure/database/gorm/provider.go` — current AutoMigrate models

### Step 4: Research Ontologies

Use this decision tree to find the best ontology source:

```
1. What domain am I modeling?
   |
   +- Financial/Accounting -> REA Ontology, FIBO
   +- Healthcare -> HL7 FHIR, ICD, SNOMED CT
   +- Supply Chain/Logistics -> GS1, UNSPSC
   +- Legal/Contracts -> LKIF, LegalRuleML
   +- Education -> CEDS, IEEE LOM
   +- Science/Research -> PROV-O, Dublin Core
   +- E-Commerce/Products -> Schema.org, GoodRelations
   +- Social/People -> FOAF, Schema.org
   +- Geography/Location -> GeoSPARQL, Schema.org
   +- Publishing/Media -> Dublin Core, Schema.org
   +- Other -> Web search: "[domain] ontology" or "[domain] vocabulary"

2. Does the concept represent a relationship or provenance?
   +- Yes -> Check PROV-O predicates first
   +- No -> Continue to domain-specific ontology

3. Is the concept a general thing (person, org, event, place)?
   +- Yes -> Check Schema.org
   +- No -> Use domain-specific ontology

4. No ontology match found?
   +- Use a custom name, but document WHY no ontology term fits
```

### Step 5: Present Proposed Model

Present the model as tables for user approval:

**Entity:**
| Field | Type | Ontology Source | Constraint |
|-------|------|----------------|------------|
| name | string | schema:name | required |
| ... | ... | ... | ... |

**Relationships (if any):**
| Subject | Predicate | Object | Event Name |
|---------|-----------|--------|------------|
| {Concept} | belongsTo | OtherEntity | {Concept}OtherEntityLinked |

**Lifecycle Events:**
| Entity | Event | Description |
|--------|-------|-------------|
| {Concept} | Created | Initial creation |
| {Concept} | Updated | Field value change |
| {Concept} | Deleted | Soft delete |

### Step 6: Get User Approval

Ask:
> "Here's the proposed model for **{Concept}**. Would you like to:
> 1. Approve and proceed
> 2. Modify (tell me what to change)
> 3. Pick a different ontology fit"

**Do NOT proceed to Phase 2 until the user approves.**

---

## Phase 2: Go Backend Generation

Once approved, spin up **5 parallel agents** for code generation. After all agents complete, do the sequential DI wiring step.

Use the placeholder `{concept}` for lowercase name, `{Concept}` for PascalCase, `{concepts}` for lowercase plural throughout.

### Agent 1 — Domain Layer

Create these files:

**`domain/entities/{concept}.go`**
```go
package entities

import (
    "context"
    "fmt"
    "time"

    "weos/pkg/identity"

    "github.com/akeemphilbert/pericarp/pkg/ddd"
    "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// {Concept} represents the {concept} aggregate root.
// Ontology source: {source}
type {Concept} struct {
    *ddd.BaseEntity
    // Add fields from the approved model (all unexported)
    name      string
    createdAt time.Time
}

// With initializes the {Concept} with the given parameters.
// Calling convention: new({Concept}).With(...)
func (e *{Concept}) With(name string) (*{Concept}, error) {
    // 1. Validate inputs
    if name == "" {
        return nil, fmt.Errorf("name cannot be empty")
    }

    // 2. Generate identity
    // For website children: identity.NewPage(websiteSlug, slug) or identity.NewSection(websiteSlug, pageSlug)
    // For standalone entities: use a simple URN like "urn:" + entityType + ":" + ksuid.New().String()
    entityID := identity.NewWebsite(slug) // adapt to entity's place in hierarchy

    // 3. Initialize base entity
    e.BaseEntity = ddd.NewBaseEntity(entityID)

    // 4. Set fields
    e.name = name
    e.createdAt = time.Now()

    // 5. Record creation event
    event := new({Concept}Created).With(name)
    if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
        return nil, fmt.Errorf("failed to record {Concept}Created event: %w", err)
    }

    return e, nil
}

// --- Getters ---

func (e *{Concept}) Name() string        { return e.name }
func (e *{Concept}) CreatedAt() time.Time { return e.createdAt }

// --- Restore (no events) ---

func (e *{Concept}) Restore(id string, name string, createdAt time.Time) error {
    if id == "" {
        return fmt.Errorf("id cannot be empty")
    }
    if name == "" {
        return fmt.Errorf("name cannot be empty")
    }
    e.BaseEntity = ddd.NewBaseEntity(id)
    e.name = name
    e.createdAt = createdAt
    return nil
}

// --- ApplyEvent ---

func (e *{Concept}) ApplyEvent(ctx context.Context, envelope domain.EventEnvelope[any]) error {
    if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
        return fmt.Errorf("base entity apply event failed: %w", err)
    }

    switch payload := envelope.Payload.(type) {
    case {Concept}Created:
        e.name = payload.Name
        e.createdAt = payload.Timestamp
        return nil
    // Add cases for Updated, Deleted, and any triple events
    default:
        return fmt.Errorf("unknown event type: %T", envelope.Payload)
    }
}

// Add relationship methods (triple events) if the model has relationships:
// func (e *{Concept}) LinkTo{Other}(ctx context.Context, otherID string, logger Logger) error { ... }
```

**Key rules for entity:**
- Embed `*ddd.BaseEntity` as first field
- All domain fields are **unexported** (lowercase)
- Always include `createdAt time.Time`
- No slice or map fields holding child entities — use triple events instead
- Constructor pattern: `new({Concept}).With(args...)`
- Validate ALL inputs before generating ID
- Record exactly ONE creation event
- `Restore()` does NOT record events
- `ApplyEvent()` calls `BaseEntity.ApplyEvent()` first, handles ALL event types in switch, returns error for unknown types
- For triple events in ApplyEvent: acknowledge with `_ = payload` and return nil
- Relationship methods accept `ctx context.Context` and `logger Logger` parameters

**`domain/entities/{concept}_events.go`**
```go
package entities

import (
    "time"

    "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// --- Predicate constants ---
// const Predicate{X} = "{x}"

// --- Simple events ---

type {Concept}Created struct {
    Name      string
    Timestamp time.Time
}

func (e {Concept}Created) With(name string) {Concept}Created {
    return {Concept}Created{
        Name:      name,
        Timestamp: time.Now(),
    }
}

func (e {Concept}Created) EventType() string {
    return "{Concept}.Created"
}

// Add Updated and Deleted events following the same pattern

// --- Triple events ---
// Embed domain.BasicTripleEvent for relationships:
//
// type {Concept}{Other}Linked struct {
//     domain.BasicTripleEvent
//     Timestamp time.Time
// }
//
// func (e {Concept}{Other}Linked) With(conceptID, otherID string) {Concept}{Other}Linked {
//     return {Concept}{Other}Linked{
//         BasicTripleEvent: domain.BasicTripleEvent{
//             Subject:   conceptID,
//             Predicate: Predicate{X},
//             Object:    otherID,
//         },
//         Timestamp: time.Now(),
//     }
// }

// --- Pattern constants ---

const {Concept}EventPattern = "{Concept}.%"
```

**Key rules for events:**
- Value receiver on `With()` — returns a new instance, never mutates
- Always include `Timestamp time.Time` set to `time.Now()`
- `EventType()` returns `"{Concept}.EventName"` format
- Triple events embed `domain.BasicTripleEvent` as first field
- Predicate constants use camelCase: `writtenBy`, `hasCategory`, `belongsTo`
- Check existing predicates in `domain/entities/*_events.go` before defining new ones
- File layout: predicate constants -> simple events -> triple events -> pattern constants

**`domain/repositories/{concept}_repository.go`**
```go
package repositories

import "context"
import "weos/domain/entities"

type {Concept}Repository interface {
    Save(ctx context.Context, entity *entities.{Concept}) error
    FindByID(ctx context.Context, id string) (*entities.{Concept}, error)
    FindAll(ctx context.Context, cursor string, limit int) (PaginatedResponse[*entities.{Concept}], error)
    Update(ctx context.Context, entity *entities.{Concept}) error
    Delete(ctx context.Context, id string) error
}
```

### Agent 2 — Infrastructure Layer

**`infrastructure/models/{concept}.go`**
```go
package models

import (
    "time"
    "weos/domain/entities"
)

type {Concept} struct {
    ID        string `gorm:"primaryKey"`
    Name      string `gorm:"not null"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt *time.Time `gorm:"index"`
}

func (m {Concept}) TableName() string {
    return "{concepts}"
}

func (m *{Concept}) To{Concept}() (*entities.{Concept}, error) {
    e := &entities.{Concept}{}
    if err := e.Restore(m.ID, m.Name, m.CreatedAt); err != nil {
        return nil, err
    }
    return e, nil
}

func From{Concept}(e *entities.{Concept}) *{Concept} {
    return &{Concept}{
        ID:        e.GetID(),
        Name:      e.Name(),
        CreatedAt: e.CreatedAt(),
    }
}
```

**`infrastructure/database/gorm/{concept}_repository.go`**
```go
package gorm

import (
    "context"
    "fmt"

    "weos/domain/entities"
    "weos/domain/repositories"
    "weos/infrastructure/models"

    "go.uber.org/fx"
    "gorm.io/gorm"
)

type {Concept}Repository struct {
    db *gorm.DB
}

// Provider result struct
type {Concept}RepositoryResult struct {
    fx.Out
    Repository repositories.{Concept}Repository
}

func Provide{Concept}Repository(params struct {
    fx.In
    DB *gorm.DB
}) ({Concept}RepositoryResult, error) {
    return {Concept}RepositoryResult{
        Repository: &{Concept}Repository{db: params.DB},
    }, nil
}

func (r *{Concept}Repository) Save(ctx context.Context, entity *entities.{Concept}) error {
    model := models.From{Concept}(entity)
    if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
        return fmt.Errorf("failed to save {concept}: %w", err)
    }
    return nil
}

func (r *{Concept}Repository) FindByID(ctx context.Context, id string) (*entities.{Concept}, error) {
    var model models.{Concept}
    if err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&model).Error; err != nil {
        return nil, fmt.Errorf("failed to find {concept}: %w", err)
    }
    return model.To{Concept}()
}

func (r *{Concept}Repository) FindAll(
    ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.{Concept}], error) {
    if limit <= 0 {
        limit = 20
    }

    query := r.db.WithContext(ctx).Where("deleted_at IS NULL")
    if cursor != "" {
        query = query.Where("id > ?", cursor)
    }

    var dbModels []models.{Concept}
    if err := query.Order("id ASC").Limit(limit + 1).Find(&dbModels).Error; err != nil {
        return repositories.PaginatedResponse[*entities.{Concept}]{},
            fmt.Errorf("failed to list {concepts}: %w", err)
    }

    hasMore := len(dbModels) > limit
    if hasMore {
        dbModels = dbModels[:limit]
    }

    result := make([]*entities.{Concept}, 0, len(dbModels))
    var nextCursor string
    for _, m := range dbModels {
        e, err := m.To{Concept}()
        if err != nil {
            return repositories.PaginatedResponse[*entities.{Concept}]{}, err
        }
        result = append(result, e)
        nextCursor = m.ID
    }

    if !hasMore {
        nextCursor = ""
    }

    return repositories.PaginatedResponse[*entities.{Concept}]{
        Data:    result,
        Cursor:  nextCursor,
        Limit:   limit,
        HasMore: hasMore,
    }, nil
}

func (r *{Concept}Repository) Update(ctx context.Context, entity *entities.{Concept}) error {
    model := models.From{Concept}(entity)
    if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
        return fmt.Errorf("failed to update {concept}: %w", err)
    }
    return nil
}

func (r *{Concept}Repository) Delete(ctx context.Context, id string) error {
    now := time.Now()
    if err := r.db.WithContext(ctx).Model(&models.{Concept}{}).
        Where("id = ?", id).Update("deleted_at", &now).Error; err != nil {
        return fmt.Errorf("failed to delete {concept}: %w", err)
    }
    return nil
}
```

### Agent 3 — Application Layer

**`application/{concept}_commands.go`**
```go
package application

type Create{Concept}Command struct {
    Name string
    // Add fields from approved model
}

type Update{Concept}Command struct {
    ID   string
    Name string
    // Add fields from approved model
}

type Delete{Concept}Command struct {
    ID string
}
```

**`application/{concept}_service.go`**
```go
package application

import (
    "context"
    "fmt"

    "weos/domain/entities"
    "weos/domain/repositories"

    "go.uber.org/fx"
)

type {Concept}Service interface {
    Create(ctx context.Context, cmd Create{Concept}Command) (*entities.{Concept}, error)
    GetByID(ctx context.Context, id string) (*entities.{Concept}, error)
    List(ctx context.Context, cursor string, limit int) (
        repositories.PaginatedResponse[*entities.{Concept}], error)
    Update(ctx context.Context, cmd Update{Concept}Command) (*entities.{Concept}, error)
    Delete(ctx context.Context, cmd Delete{Concept}Command) error
}

type {concept}Service struct {
    repo   repositories.{Concept}Repository
    logger entities.Logger
}

// Fx provider
func Provide{Concept}Service(params struct {
    fx.In
    Repo   repositories.{Concept}Repository
    Logger entities.Logger
}) {Concept}Service {
    return &{concept}Service{
        repo:   params.Repo,
        logger: params.Logger,
    }
}

func (s *{concept}Service) Create(ctx context.Context, cmd Create{Concept}Command) (*entities.{Concept}, error) {
    entity, err := new(entities.{Concept}).With(cmd.Name)
    if err != nil {
        return nil, fmt.Errorf("failed to create {concept}: %w", err)
    }
    if err := s.repo.Save(ctx, entity); err != nil {
        return nil, err
    }
    s.logger.Info(ctx, "{concept} created", "id", entity.GetID())
    return entity, nil
}

func (s *{concept}Service) GetByID(ctx context.Context, id string) (*entities.{Concept}, error) {
    return s.repo.FindByID(ctx, id)
}

func (s *{concept}Service) List(ctx context.Context, cursor string, limit int) (
    repositories.PaginatedResponse[*entities.{Concept}], error,
) {
    return s.repo.FindAll(ctx, cursor, limit)
}

func (s *{concept}Service) Update(ctx context.Context, cmd Update{Concept}Command) (*entities.{Concept}, error) {
    entity, err := s.repo.FindByID(ctx, cmd.ID)
    if err != nil {
        return nil, err
    }
    // Apply updates to entity fields here
    // (generate Update method on entity or update fields directly based on model)
    if err := s.repo.Update(ctx, entity); err != nil {
        return nil, err
    }
    s.logger.Info(ctx, "{concept} updated", "id", entity.GetID())
    return entity, nil
}

func (s *{concept}Service) Delete(ctx context.Context, cmd Delete{Concept}Command) error {
    if err := s.repo.Delete(ctx, cmd.ID); err != nil {
        return err
    }
    s.logger.Info(ctx, "{concept} deleted", "id", cmd.ID)
    return nil
}
```

### Agent 4 — API + CLI Layer

**`api/handlers/{concept}_handler.go`**
```go
package handlers

import (
    "net/http"
    "strconv"

    "weos/application"

    "github.com/labstack/echo/v4"
)

type {Concept}Handler struct {
    service application.{Concept}Service
}

func New{Concept}Handler(service application.{Concept}Service) *{Concept}Handler {
    return &{Concept}Handler{service: service}
}

// JSON request/response structs
type Create{Concept}Request struct {
    Name string `json:"name"`
    // Add fields from model
}

type Update{Concept}Request struct {
    Name string `json:"name"`
    // Add fields from model
}

type {Concept}Response struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    CreatedAt string `json:"created_at"`
    // Add fields from model
}

func (h *{Concept}Handler) Create(c echo.Context) error {
    var req Create{Concept}Request
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
    }
    entity, err := h.service.Create(c.Request().Context(), application.Create{Concept}Command{
        Name: req.Name,
    })
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
    }
    return c.JSON(http.StatusCreated, to{Concept}Response(entity))
}

func (h *{Concept}Handler) Get(c echo.Context) error {
    id := c.Param("id")
    entity, err := h.service.GetByID(c.Request().Context(), id)
    if err != nil {
        return c.JSON(http.StatusNotFound, map[string]string{"error": "{concept} not found"})
    }
    return c.JSON(http.StatusOK, to{Concept}Response(entity))
}

func (h *{Concept}Handler) List(c echo.Context) error {
    cursor := c.QueryParam("cursor")
    limit, _ := strconv.Atoi(c.QueryParam("limit"))
    if limit <= 0 {
        limit = 20
    }
    result, err := h.service.List(c.Request().Context(), cursor, limit)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
    }
    items := make([]_{Concept}Response, 0, len(result.Data))
    for _, e := range result.Data {
        items = append(items, to{Concept}Response(e))
    }
    return c.JSON(http.StatusOK, map[string]interface{}{
        "data":     items,
        "cursor":   result.Cursor,
        "has_more": result.HasMore,
    })
}

func (h *{Concept}Handler) Update(c echo.Context) error {
    id := c.Param("id")
    var req Update{Concept}Request
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
    }
    entity, err := h.service.Update(c.Request().Context(), application.Update{Concept}Command{
        ID:   id,
        Name: req.Name,
    })
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
    }
    return c.JSON(http.StatusOK, to{Concept}Response(entity))
}

func (h *{Concept}Handler) Delete(c echo.Context) error {
    id := c.Param("id")
    if err := h.service.Delete(c.Request().Context(), application.Delete{Concept}Command{ID: id}); err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
    }
    return c.NoContent(http.StatusNoContent)
}

// Helper to convert entity to response
func to{Concept}Response(e *entities.{Concept}) {Concept}Response {
    return {Concept}Response{
        ID:        e.GetID(),
        Name:      e.Name(),
        CreatedAt: e.CreatedAt().Format(time.RFC3339),
    }
}
```

**`internal/cli/{concept}.go`**
```go
package cli

import (
    "encoding/json"
    "fmt"
    "os"

    "weos/application"

    "github.com/spf13/cobra"
)

var {concept}Cmd = &cobra.Command{
    Use:   "{concept}",
    Short: "Manage {concepts}",
}

var {concept}CreateCmd = &cobra.Command{
    Use:   "create",
    Short: "Create a new {concept}",
    RunE: func(cmd *cobra.Command, args []string) error {
        deps, err := StartContainer(GetConfig())
        if err != nil {
            return err
        }
        defer deps.Shutdown()

        name, _ := cmd.Flags().GetString("name")
        entity, err := deps.{Concept}Service.Create(cmd.Context(), application.Create{Concept}Command{
            Name: name,
        })
        if err != nil {
            return fmt.Errorf("failed to create {concept}: %w", err)
        }
        fmt.Fprintf(os.Stdout, "Created {concept}: %s\n", entity.GetID())
        return nil
    },
}

var {concept}GetCmd = &cobra.Command{
    Use:   "get [id]",
    Short: "Get a {concept} by ID",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        deps, err := StartContainer(GetConfig())
        if err != nil {
            return err
        }
        defer deps.Shutdown()

        entity, err := deps.{Concept}Service.GetByID(cmd.Context(), args[0])
        if err != nil {
            return fmt.Errorf("{concept} not found: %w", err)
        }
        data, _ := json.MarshalIndent(entity, "", "  ")
        fmt.Fprintln(os.Stdout, string(data))
        return nil
    },
}

var {concept}ListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all {concepts}",
    RunE: func(cmd *cobra.Command, args []string) error {
        deps, err := StartContainer(GetConfig())
        if err != nil {
            return err
        }
        defer deps.Shutdown()

        limit, _ := cmd.Flags().GetInt("limit")
        cursor, _ := cmd.Flags().GetString("cursor")
        result, err := deps.{Concept}Service.List(cmd.Context(), cursor, limit)
        if err != nil {
            return fmt.Errorf("failed to list {concepts}: %w", err)
        }
        data, _ := json.MarshalIndent(result, "", "  ")
        fmt.Fprintln(os.Stdout, string(data))
        return nil
    },
}

var {concept}DeleteCmd = &cobra.Command{
    Use:   "delete [id]",
    Short: "Delete a {concept}",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        deps, err := StartContainer(GetConfig())
        if err != nil {
            return err
        }
        defer deps.Shutdown()

        err = deps.{Concept}Service.Delete(cmd.Context(), application.Delete{Concept}Command{ID: args[0]})
        if err != nil {
            return fmt.Errorf("failed to delete {concept}: %w", err)
        }
        fmt.Fprintln(os.Stdout, "{Concept} deleted successfully")
        return nil
    },
}

func init() {
    {concept}CreateCmd.Flags().String("name", "", "Name of the {concept}")
    _ = {concept}CreateCmd.MarkFlagRequired("name")

    {concept}ListCmd.Flags().Int("limit", 20, "Number of items per page")
    {concept}ListCmd.Flags().String("cursor", "", "Pagination cursor")

    {concept}Cmd.AddCommand({concept}CreateCmd, {concept}GetCmd, {concept}ListCmd, {concept}DeleteCmd)
    rootCmd.AddCommand({concept}Cmd)
}
```

### Agent 5 — Tests

**`domain/entities/{concept}_test.go`**
```go
package entities

import (
    "context"
    "testing"
    "time"

    "github.com/akeemphilbert/pericarp/pkg/ddd"
    "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// ============================================================
// Entity constructor tests
// ============================================================

func Test{Concept}_With(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name    string
        // Add constructor params matching With() signature
        inputName string
        wantErr   bool
        errSubstr string
        validate  func(*testing.T, *{Concept})
    }{
        {
            name:      "happy path - valid {concept}",
            inputName: "Test {Concept}",
            wantErr:   false,
            validate: func(t *testing.T, e *{Concept}) {
                t.Helper()
                if e.GetID() == "" {
                    t.Fatal("expected non-empty ID")
                }
                if e.Name() != "Test {Concept}" {
                    t.Fatalf("expected name %q, got %q", "Test {Concept}", e.Name())
                }
                if e.CreatedAt().IsZero() {
                    t.Fatal("expected non-zero CreatedAt")
                }
            },
        },
        {
            name:      "invalid - empty name",
            inputName: "",
            wantErr:   true,
            errSubstr: "name cannot be empty",
        },
        // Add more validation test cases based on entity fields
    }

    for _, tt := range tests {
        tt := tt
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            entity, err := new({Concept}).With(tt.inputName)

            if tt.wantErr {
                if err == nil {
                    t.Fatal("expected error, got nil")
                }
                if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
                    t.Fatalf("error should contain %q, got: %v", tt.errSubstr, err)
                }
                if entity != nil {
                    t.Fatal("expected nil entity on error")
                }
                return
            }

            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if entity == nil {
                t.Fatal("expected non-nil entity")
            }
            if tt.validate != nil {
                tt.validate(t, entity)
            }
        })
    }
}

// ============================================================
// ApplyEvent tests
// ============================================================

func Test{Concept}_ApplyEvent(t *testing.T) {
    t.Parallel()
    ctx := context.Background()

    setup{Concept} := func(t *testing.T, id string) *{Concept} {
        t.Helper()
        e := &{Concept}{}
        e.BaseEntity = ddd.NewBaseEntity(id)
        return e
    }

    tests := []struct {
        name      string
        setup     func(*testing.T) *{Concept}
        event     domain.EventEnvelope[any]
        wantErr   bool
        errSubstr string
        validate  func(*testing.T, *{Concept})
    }{
        {
            name: "{Concept}Created restores fields",
            setup: func(t *testing.T) *{Concept} {
                return setup{Concept}(t, "test-123")
            },
            event: newTestEnvelope(
                {Concept}Created{
                    Name:      "Test",
                    Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
                },
                "test-123",
                "{Concept}.Created",
                0,
            ),
            wantErr: false,
            validate: func(t *testing.T, e *{Concept}) {
                t.Helper()
                if e.Name() != "Test" {
                    t.Fatalf("expected name %q, got %q", "Test", e.Name())
                }
            },
        },
        // Add triple event test cases if model has relationships
        {
            name: "unknown event type returns error",
            setup: func(t *testing.T) *{Concept} {
                return setup{Concept}(t, "test-789")
            },
            event: newTestEnvelope(
                struct{}{},
                "test-789",
                "UnknownEvent",
                0,
            ),
            wantErr:   true,
            errSubstr: "unknown event type",
        },
    }

    for _, tt := range tests {
        tt := tt
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            entity := tt.setup(t)
            err := entity.ApplyEvent(ctx, tt.event)
            if tt.wantErr {
                if err == nil {
                    t.Fatal("expected error, got nil")
                }
                if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
                    t.Fatalf("error should contain %q, got: %v", tt.errSubstr, err)
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if tt.validate != nil {
                tt.validate(t, entity)
            }
        })
    }
}

// ============================================================
// Event tests
// ============================================================

func Test{Concept}Created_EventType(t *testing.T) {
    t.Parallel()
    e := {Concept}Created{}
    if got := e.EventType(); got != "{Concept}.Created" {
        t.Fatalf("EventType() = %q, want %q", got, "{Concept}.Created")
    }
}

func Test{Concept}Created_With(t *testing.T) {
    t.Parallel()
    e := {Concept}Created{}.With("Test")
    if e.Name != "Test" {
        t.Fatalf("Name = %q, want %q", e.Name, "Test")
    }
    if e.Timestamp.IsZero() {
        t.Fatal("expected non-zero Timestamp")
    }
}

// ============================================================
// Test helpers (only if not already defined in another _test.go)
// ============================================================

// newTestEnvelope creates an EventEnvelope[any] for testing.
func newTestEnvelope(payload any, aggregateID, eventType string, sequenceNo int) domain.EventEnvelope[any] {
    typedEnvelope := domain.NewEventEnvelope(payload, aggregateID, eventType, sequenceNo)
    return domain.EventEnvelope[any]{
        ID:          typedEnvelope.ID,
        AggregateID: typedEnvelope.AggregateID,
        EventType:   typedEnvelope.EventType,
        SequenceNo:  typedEnvelope.SequenceNo,
        Payload:     typedEnvelope.Payload,
    }
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
    for i := 0; i <= len(s)-len(substr); i++ {
        if s[i:i+len(substr)] == substr {
            return true
        }
    }
    return false
}
```

**Important:** If `newTestEnvelope` and `contains` helpers already exist in another `_test.go` file in the same package, do NOT duplicate them. Check first with:
```
Grep: "func newTestEnvelope" in domain/entities/*_test.go
Grep: "func contains" in domain/entities/*_test.go
```

### Sequential Step — DI Wiring (after all agents complete)

After all 5 agents finish, perform these updates sequentially:

1. **`pkg/identity/identity.go`** — If the entity needs a dedicated constructor (e.g., it's a child of website/page), add it. For standalone entities, add a type constant:
   ```go
   const Type{Concept} = "{concept}"
   ```

2. **`infrastructure/database/gorm/provider.go`** — Add model to AutoMigrate:
   ```go
   models := []interface{}{
       &models.{Concept}{},
       // ... existing models
   }
   if err := db.AutoMigrate(models...); err != nil {
       return GormDBResult{}, fmt.Errorf("failed to run auto migrate: %w", err)
   }
   ```

3. **`application/module.go`** — Register providers:
   ```go
   fx.Provide(gorm.Provide{Concept}Repository),
   fx.Provide(Provide{Concept}Service),
   ```

4. **`cmd/api/main.go`** — Add fx.Populate, create handler, register routes:
   ```go
   var {concept}Service application.{Concept}Service
   // In fx.New: fx.Populate(&{concept}Service),
   // After Echo setup:
   {concept}Handler := handlers.New{Concept}Handler({concept}Service)
   api.POST("/{concepts}", {concept}Handler.Create)
   api.GET("/{concepts}", {concept}Handler.List)
   api.GET("/{concepts}/:id", {concept}Handler.Get)
   api.PUT("/{concepts}/:id", {concept}Handler.Update)
   api.DELETE("/{concepts}/:id", {concept}Handler.Delete)
   ```

5. **`internal/cli/di.go`** — Add service to Dependencies:
   ```go
   type Dependencies struct {
       {Concept}Service application.{Concept}Service
       App             *fx.App
   }
   ```
   And update `startContainerWithConfig` to extract and assign the service.

6. **Check EventStore** — If `application/module.go` still has the EventStore provider as a TODO comment, inform the user that event sourcing reconstruction won't work until `ProvideEventStore` is implemented. The projection-based CRUD will work immediately.

---

## Phase 3: NuxtJS Admin UI

### Check if Admin Scaffold Exists

Check if `web/admin/package.json` exists.

**If it does NOT exist — scaffold the full NuxtJS project:**

```
web/admin/
  nuxt.config.ts          -- Nuxt configuration with Ant Design; must include components: [{ path: '~/components', pathPrefix: false }] so components auto-import by filename without directory prefix
  package.json            -- Dependencies: nuxt, @ant-design-vue/nuxt, ant-design-vue
  tsconfig.json           -- TypeScript config extending Nuxt
  app.vue                 -- Root app component
  layouts/default.vue     -- Sidebar layout with a-layout
  plugins/antd.ts         -- Ant Design Vue plugin registration
  composables/useApi.ts   -- Base API client ($fetch wrapper)
  components/atoms/       -- Empty (Ant Design provides atoms)
  components/molecules/   -- Reusable form field molecules
  components/organisms/   -- Tables and forms
  components/templates/   -- Page layout templates
  pages/index.vue         -- Dashboard page
  assets/css/main.css     -- Global styles
```

### Create Entity UI Files (always)

```
web/admin/
  composables/use{Concept}Api.ts
    -- CRUD API calls: create{Concept}, get{Concept}, list{Concepts}, update{Concept}, delete{Concept}
    -- Uses useApi composable, calls /api/{concepts} endpoints

  components/molecules/{Concept}FormField.vue
    -- Ant Design a-form-item with label, input, and error display
    -- Props: label, value, error, field type

  components/organisms/{Concept}Table.vue
    -- a-table with columns derived from entity properties
    -- Pagination using cursor-based API
    -- Action column with View, Edit, and Delete buttons
    -- View button links to /{ concepts }/${ record.id } (view page)
    -- Edit button links to /{ concepts }/${ record.id }/edit (edit page)
    -- Delete button with a-popconfirm confirmation
    -- Actions column width: 250
    -- Uses use{Concept}Api composable

  components/organisms/{Concept}Form.vue
    -- a-form with fields from entity properties
    -- Validation rules matching backend constraints
    -- Submit handler calling create or update API
    -- Uses use{Concept}Api composable

  pages/{concepts}/index.vue
    -- List page composing {Concept}Table
    -- "Create New" button linking to create page
    -- Page title and breadcrumb

  pages/{concepts}/create.vue
    -- Create page composing {Concept}Form
    -- Redirects to list page on success

  pages/{concepts}/[id]/index.vue
    -- Read-only view page showing entity details
    -- Fetches entity via get{Concept}(id) from use{Concept}Api
    -- Uses a-page-header with back button (navigates to list) and Edit button in extra slot
    -- Uses a-descriptions (bordered, single column) to display all fields
    -- Status fields rendered as a-tag with color coding
    -- Edit button links to /{concepts}/${id}/edit

  pages/{concepts}/[id]/edit.vue
    -- Edit page composing {Concept}Form in edit mode
    -- Loads entity by route param ID via get{Concept}(id)
    -- Passes entity as :initial-data and :is-edit="true" to {Concept}Form
    -- Redirects to list page on successful update
```

**Atomic Design mapping:**
- Atoms = Ant Design Vue components (a-button, a-input, a-table, etc.)
- Molecules = `{Concept}FormField` — label + input + error message
- Organisms = `{Concept}Table`, `{Concept}Form` — composed from molecules + atoms
- Templates = `layouts/default.vue` — page layout structure
- Pages = route-level compositions that wire organisms together

**Update navigation** — Add `{concepts}` link to `layouts/default.vue` sidebar menu.

---

## Phase 4: E2E Testing (with safety gate)

**Before running any tests, ASK the user:**

> "I can smoke-test the CRUD endpoints. I'll use a separate dev SQLite database (`weos-dev-test.db`) so your main database is untouched. Safe to proceed?
>
> 1. Yes, use `weos-dev-test.db`
> 2. Yes, use existing DB
> 3. Skip E2E testing"

**Do NOT proceed without explicit user approval.**

If approved:
1. Start the server:
   ```bash
   DATABASE_DSN=weos-dev-test.db go run ./cmd/api &
   ```
2. Wait for health:
   ```bash
   curl -s --retry 5 --retry-delay 2 http://localhost:8080/api/health
   ```
3. Run CRUD sequence:
   ```
   POST /api/{concepts}       -- Create with test data
   GET  /api/{concepts}/:id   -- Verify created entity
   GET  /api/{concepts}       -- List all (should include created entity)
   PUT  /api/{concepts}/:id   -- Update with modified data
   GET  /api/{concepts}/:id   -- Verify update applied
   DELETE /api/{concepts}/:id -- Delete entity
   GET  /api/{concepts}/:id   -- Verify 404 after delete
   ```
4. Kill server process
5. Optionally remove `weos-dev-test.db`
6. Report PASS/FAIL per step

---

## Phase 5: Summary

After all phases complete, provide a summary:

### Files Created
List every new file with a one-line description.

### Files Modified
List every existing file that was updated and what changed.

### Ontology Choices
Explain which ontology was used and why. Cite specific terms and their sources.

### Manual Steps
Remind the user to run:
```bash
go mod tidy
make fmt
make lint
make test
```

If the NuxtJS admin was scaffolded:
```bash
cd web/admin && npm install && npm run dev
```

### Linting Constraints
All generated Go code MUST comply with:
- **Line length:** 120 characters max
- **Function length:** 100 lines / 50 statements max
- **Cyclomatic complexity:** 15 max
- **Duplicate threshold:** 100 tokens
- Split functions that exceed these limits

### Architecture Notes
- Dependencies point inward: API -> Application -> Domain <- Infrastructure
- Never persist entities directly — use the service layer
- Events are immutable — never modify after creation
- Event handlers must be idempotent
- Services own the UnitOfWork lifecycle (when EventStore is implemented)
