package application

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
)

// fakeSkillSource pages agent-skill resources out of a static slice.
type fakeSkillSource struct {
	resources []*entities.Resource
	err       error
	gotCtx    context.Context
}

func (f *fakeSkillSource) List(
	ctx context.Context, typeSlug, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	f.gotCtx = ctx
	if f.err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{}, f.err
	}
	if typeSlug != AgentSkillTypeSlug {
		return repositories.PaginatedResponse[*entities.Resource]{}, fmt.Errorf("unexpected slug %q", typeSlug)
	}
	return repositories.PaginatedResponse[*entities.Resource]{Data: f.resources, HasMore: false}, nil
}

func skillResource(t *testing.T, id string, data string) *entities.Resource {
	t.Helper()
	res, err := new(entities.Resource).With(id, AgentSkillTypeSlug, json.RawMessage(data), "", "")
	if err != nil {
		t.Fatalf("build resource: %v", err)
	}
	return res
}

const goodSkillJSON = `{"schemaVersion":1,"name":"researcher","description":"Finds things",` +
	`"instructions":"Search first.","tools":["kg_search_entities"],"mode":"task"}`

func TestSkillRegistry_ListStripsIdentityKeepsCancellation(t *testing.T) {
	source := &fakeSkillSource{resources: []*entities.Resource{
		skillResource(t, "urn:agent-skill:one", goodSkillJSON),
	}}
	registry := &SkillRegistry{resources: source, logger: noopLogger{}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = auth.ContextWithAgent(ctx, &auth.Identity{AgentID: "u1", ActiveAccountID: "acc1"})

	if _, err := registry.Skills(ctx); err != nil {
		t.Fatalf("Skills: %v", err)
	}
	// Skills are app capabilities: the listing must read unscoped even when
	// the caller is authenticated...
	if got := auth.AgentFromCtx(source.gotCtx); got != nil {
		t.Errorf("listing must be identity-free, saw agent %q", got.AgentID)
	}
	// ...but must still die with the caller — not run on detached from
	// context.Background() while holding the registry mutex.
	cancel()
	if source.gotCtx.Err() == nil {
		t.Error("listing context must inherit the caller's cancellation")
	}
}

func TestParseSkillDefinition_AppliesDefaults(t *testing.T) {
	def, err := ParseSkillDefinition("urn:agent-skill:x", json.RawMessage(
		`{"name":"minimal","description":"d","instructions":"i"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if def.SchemaVersion != entities.SkillSchemaVersion {
		t.Errorf("schemaVersion default = %d, want %d", def.SchemaVersion, entities.SkillSchemaVersion)
	}
	if def.Mode != entities.SkillModeTask {
		t.Errorf("mode default = %q, want %q", def.Mode, entities.SkillModeTask)
	}
	if def.ID != "urn:agent-skill:x" {
		t.Errorf("id = %q", def.ID)
	}
}

func TestSkillRegistry_LoadsValidSkipsInvalid(t *testing.T) {
	source := &fakeSkillSource{resources: []*entities.Resource{
		skillResource(t, "urn:agent-skill:good", goodSkillJSON),
		// Invalid: references a tool that does not exist.
		skillResource(t, "urn:agent-skill:badtool",
			`{"name":"bad_tool","description":"d","instructions":"i","tools":["no_such_tool"]}`),
		// Invalid: unsupported schema version.
		skillResource(t, "urn:agent-skill:future",
			`{"schemaVersion":99,"name":"future","description":"d","instructions":"i"}`),
	}}
	registry := &SkillRegistry{resources: source, logger: noopLogger{}}
	registry.SetKnownTools(func(context.Context) (map[string]bool, error) {
		return map[string]bool{"kg_search_entities": true}, nil
	})

	skills, err := registry.Skills(context.Background())
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "researcher" {
		t.Fatalf("expected only the valid skill to load, got %+v", skills)
	}
}

func TestSkillRegistry_DuplicateNamesKeepFirst(t *testing.T) {
	source := &fakeSkillSource{resources: []*entities.Resource{
		skillResource(t, "urn:agent-skill:a", goodSkillJSON),
		skillResource(t, "urn:agent-skill:b", goodSkillJSON),
	}}
	registry := &SkillRegistry{resources: source, logger: noopLogger{}}

	skills, err := registry.Skills(context.Background())
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if len(skills) != 1 || skills[0].ID != "urn:agent-skill:a" {
		t.Fatalf("expected first duplicate to win, got %+v", skills)
	}
}

func TestSkillRegistry_MarkDirtyReloads(t *testing.T) {
	source := &fakeSkillSource{resources: []*entities.Resource{
		skillResource(t, "urn:agent-skill:one", goodSkillJSON),
	}}
	registry := &SkillRegistry{resources: source, logger: noopLogger{}}

	first, err := registry.Skills(context.Background())
	if err != nil || len(first) != 1 {
		t.Fatalf("first load: %v, %d skills", err, len(first))
	}

	// A new skill appears; without MarkDirty the cache serves stale data.
	source.resources = append(source.resources, skillResource(t, "urn:agent-skill:two",
		`{"name":"second","description":"d","instructions":"i"}`))
	cached, _ := registry.Skills(context.Background())
	if len(cached) != 1 {
		t.Fatalf("expected cached result before MarkDirty, got %d", len(cached))
	}

	registry.MarkDirty()
	reloaded, err := registry.Skills(context.Background())
	if err != nil || len(reloaded) != 2 {
		t.Fatalf("after MarkDirty: %v, %d skills (want 2)", err, len(reloaded))
	}
}
