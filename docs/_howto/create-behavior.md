---
title: Create a Behavior
parent: How-to Guides
layout: default
nav_order: 9
---

# Create a Behavior

This guide walks through creating a custom behavior that attaches domain logic to a resource type. We will build a behavior for a "blog-post" type that auto-generates a URL slug from the title.

## Prerequisites

- A working WeOS development environment (`make build` succeeds)
- A preset that defines the resource type you want to add behavior to (see [Creating a Preset]({% link _tutorials/creating-a-preset.md %}))

> **Note:** This guide uses the module import path `weos/...`, which matches the current `go.mod` (`module weos`). If you are following older examples that show `github.com/wepala/weos/...`, use the import path that matches your checkout and `go.mod`.

## 1. Create the Behavior Struct

In your preset package (e.g. `application/presets/blog/`), create a struct that embeds `entities.DefaultBehavior` and overrides the hooks you need. All imports use the module path from `go.mod` (currently `weos`):

```go
package blog

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "weos/domain/entities"
)

type blogPostBehavior struct {
    entities.DefaultBehavior
}
```

Embedding `DefaultBehavior` means every hook you do not override is a no-op pass-through.

## 2. Implement the Hooks

Override only the hooks your behavior requires. For auto-generating a URL slug:

```go
func (b *blogPostBehavior) BeforeCreate(
    ctx context.Context, data json.RawMessage, rt *entities.ResourceType,
) (json.RawMessage, error) {
    return injectSlug(data)
}

func (b *blogPostBehavior) BeforeUpdate(
    ctx context.Context, _ *entities.Resource, data json.RawMessage, rt *entities.ResourceType,
) (json.RawMessage, error) {
    return injectSlug(data)
}

func injectSlug(data json.RawMessage) (json.RawMessage, error) {
    var m map[string]any
    if err := json.Unmarshal(data, &m); err != nil {
        return nil, fmt.Errorf("invalid blog post data: %w", err)
    }
    if title, ok := m["title"].(string); ok && m["urlSlug"] == nil {
        m["urlSlug"] = strings.ToLower(strings.ReplaceAll(title, " ", "-"))
    }
    return json.Marshal(m)
}
```

### Hook Selection Guide

| When you need to... | Use this hook |
|---------------------|---------------|
| Transform or enrich input data | `BeforeCreate` / `BeforeUpdate` |
| Validate business rules before commit | `BeforeCreateCommit` / `BeforeUpdateCommit` |
| Prevent deletion based on a condition | `BeforeDelete` |
| Trigger side effects after persistence | `AfterCreate` / `AfterUpdate` / `AfterDelete` |

## 3. Register the Behavior in the Preset

In your preset's `Register` function, include the behavior in the `Behaviors` map keyed by the resource type slug:

```go
package blog

import (
    "weos/application"
    "weos/domain/entities"
)

func Register(registry *application.PresetRegistry) {
    registry.MustAdd(application.PresetDefinition{
        Name:        "blog",
        Description: "Blog content types",
        Types: []application.PresetResourceType{
            application.NewPresetType("BlogPost", "blog-post",
                "A blog post",
                `{"@vocab": "https://schema.org/"}`,
                `{
                    "type": "object",
                    "properties": {
                        "title":   {"type": "string"},
                        "urlSlug": {"type": "string"},
                        "body":    {"type": "string"}
                    },
                    "required": ["title"]
                }`,
            ),
        },
        Behaviors: map[string]application.BehaviorFactory{
            "blog-post": application.StaticBehavior(&blogPostBehavior{}),
        },
    })
}
```

The slug key (`"blog-post"`) must match the resource type's slug exactly.

`Behaviors` is a map of **factory functions**, not pre-built instances. Factories are
invoked at startup, after the dependency injection container is wired, so a behavior
can close over real repositories and loggers. For a behavior with no service
dependencies (like the one above), `application.StaticBehavior` wraps a plain instance.

## 3a. Inject Application Services

If your behavior needs a repository or logger, write a real factory instead of
`StaticBehavior`. The factory receives a populated `application.BehaviorServices`:

| Field | Type | Use for |
|---|---|---|
| `Resources` | `repositories.ResourceRepository` | reading other resources (queries, lookups) |
| `Triples` | `repositories.TripleRepository` | reading relationships between resources |
| `ResourceTypes` | `repositories.ResourceTypeRepository` | loading a type's schema/context (e.g. to call `application.EdgeValue`) |
| `Logger` | `entities.Logger` | structured logging |
| `Writer` | `application.ResourceWriter` | **creating, updating, or deleting other resources** through the full service pipeline |

Example (showing imports):

```go
import (
    "weos/application"
    "weos/domain/entities"
    "weos/domain/repositories"
)

type blogPostBehavior struct {
    entities.DefaultBehavior
    resources repositories.ResourceRepository
    logger    entities.Logger
}

func newBlogPostBehavior(s application.BehaviorServices) entities.ResourceBehavior {
    return &blogPostBehavior{resources: s.Resources, logger: s.Logger}
}

// In Register():
Behaviors: map[string]application.BehaviorFactory{
    "blog-post": newBlogPostBehavior,
},
```

The factory is called once, at startup, by `application.ProvideResourceBehaviorRegistry`.

### Creating or updating other resources

When a hook needs to write *other* resources (not just transform the current
one), use `services.Writer`. It is a `ResourceWriter` that forwards to the real
`ResourceService`, so writes go through schema validation, JSON-LD graph
assembly, triple extraction, event recording, UnitOfWork commit, and nested
behavior dispatch — i.e. creating a resource from a behavior runs that
resource's own behaviors in turn.

Example: a `comment` behavior that bumps a `replyCount` on the parent post:

```go
type commentBehavior struct {
    entities.DefaultBehavior
    writer    application.ResourceWriter
    resources repositories.ResourceRepository
    types     repositories.ResourceTypeRepository
    logger    entities.Logger
}

func newCommentBehavior(s application.BehaviorServices) entities.ResourceBehavior {
    return &commentBehavior{
        writer:    s.Writer,
        resources: s.Resources,
        types:     s.ResourceTypes,
        logger:    s.Logger,
    }
}

func (b *commentBehavior) AfterCreate(ctx context.Context, r *entities.Resource) error {
    commentType, err := b.types.FindBySlug(ctx, r.TypeSlug())
    if err != nil {
        return err
    }
    postID := application.EdgeValue(r.Data(), commentType.Context(), "postId")
    if postID == "" {
        return nil
    }
    post, err := b.resources.FindByID(ctx, postID)
    if err != nil {
        return err
    }
    // Build newData by round-tripping post.Data() through json.Unmarshal /
    // json.Marshal and incrementing the replyCount field. Preserve `@id` and
    // `@context` on the result — Update revalidates the full JSON-LD graph
    // and will reject a payload that has lost them.
    _, err = b.writer.Update(ctx, application.UpdateResourceCommand{
        ID:   post.GetID(),
        Data: newData,
    })
    return err
}
```

Behavior cascades are bounded: `ResourceService` caps the nesting depth
(see `maxBehaviorRecursionDepth` in `application/resource_service.go`) to
catch accidental cycles. If you hit that limit, you probably have a loop —
check that two behaviors aren't triggering each other indefinitely.

## 4. Register the Preset

Add your preset to the `RegisterAll` function in `application/presets/register.go`, which is where all built-in presets are registered. This function is called by `presets.NewDefaultRegistry()`, which is used by the CLI (`internal/cli/di.go`, `internal/cli/serve.go`) and the MCP server:

```go
import "weos/application/presets/blog"

func RegisterAll(registry *application.PresetRegistry) {
    core.Register(registry)
    // ...existing presets...
    blog.Register(registry)  // add your preset here
}
```

## 5. Test the Behavior

Write a unit test that calls the behavior hooks directly:

```go
func TestBlogPostBehavior_InjectsSlug(t *testing.T) {
    b := &blogPostBehavior{}
    input := json.RawMessage(`{"title": "My First Post"}`)

    output, err := b.BeforeCreate(context.Background(), input, nil)
    require.NoError(t, err)

    var m map[string]any
    require.NoError(t, json.Unmarshal(output, &m))
    assert.Equal(t, "my-first-post", m["urlSlug"])
}
```

Run with:

```bash
go test -v -run TestBlogPostBehavior ./application/presets/blog/
```

## Real-World Example

The core preset's `personBehavior` (`application/presets/core/preset.go`) computes a full `name` field by concatenating `givenName` and `familyName`. It overrides `BeforeCreate` and `BeforeUpdate` — the same pattern shown above.

## Further Reading

- [Behaviors (Explanation)]({% link _explanation/behaviors.md %}) — architectural rationale and the Type Object pattern
- [Creating a Preset]({% link _tutorials/creating-a-preset.md %}) — how to bundle resource types and behaviors into a preset
