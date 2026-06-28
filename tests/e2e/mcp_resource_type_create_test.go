package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPResourceTypeCreate exercises the resource_type_create MCP tool against a
// freshly booted application with a clean SQLite database. It is the regression
// guard for issue #382: an object JSON Schema (and a string/object JSON-LD
// context) must round-trip through the tool, which previously failed at the MCP
// input-schema layer because the fields were typed json.RawMessage.
func TestMCPResourceTypeCreate(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "mcp-resource-type-create",
		ScenarioInitializer: initResourceTypeCreateScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/resource_type_create.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("resource_type_create acceptance scenarios failed")
	}
}

func initResourceTypeCreateScenario(sc *godog.ScenarioContext) {
	w := &mcpWorld{}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the JSON-LD context:$`, w.theJSONLDContext)
	sc.Step(`^the JSON Schema:$`, w.theJSONSchema)
	sc.Step(`^I create the resource type "([^"]*)" over MCP$`, w.iCreateResourceType)
	sc.Step(`^the resource type "([^"]*)" is created$`, w.theResourceTypeIsCreated)
	sc.Step(`^a "([^"]*)" resource can be created with:$`, w.aResourceCanBeCreatedWith)
	sc.Step(`^I update the resource type "([^"]*)" with description "([^"]*)"$`, w.iUpdateResourceTypeDescription)
	sc.Step(`^the resource type "([^"]*)" still requires a "([^"]*)" property$`, w.theResourceTypeStillRequires)
}

func (w *mcpWorld) theJSONLDContext(doc *godog.DocString) error {
	w.pendingContext = doc.Content
	return nil
}

func (w *mcpWorld) theJSONSchema(doc *godog.DocString) error {
	w.pendingSchema = doc.Content
	return nil
}

func (w *mcpWorld) iCreateResourceType(ctx context.Context, slug string) error {
	args := fmt.Sprintf(`{"name":%q,"slug":%q,"description":%q,"context":%s,"schema":%s}`,
		titleCase(slug), slug, "Acceptance type "+slug, w.pendingContext, w.pendingSchema)
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_type_create", Arguments: json.RawMessage(args),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *mcpWorld) theResourceTypeIsCreated(slug string) error {
	if err := w.theCallSucceeds(); err != nil {
		return err
	}
	m, err := w.resultMap()
	if err != nil {
		return err
	}
	if got, _ := m["slug"].(string); got != slug {
		return fmt.Errorf("expected slug %q, got %q", slug, got)
	}
	id, _ := m["id"].(string)
	if want := "urn:type:" + slug; !strings.HasPrefix(id, want) {
		return fmt.Errorf("expected id to start with %q, got %q", want, id)
	}
	return nil
}

func (w *mcpWorld) aResourceCanBeCreatedWith(ctx context.Context, slug string, data *godog.DocString) error {
	args := fmt.Sprintf(`{"type_slug":%q,"data":%s}`, slug, data.Content)
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_create", Arguments: json.RawMessage(args),
	})
	if err != nil {
		return fmt.Errorf("resource_create protocol error: %v", err)
	}
	if res.IsError {
		return fmt.Errorf("expected the new type to be usable, but resource_create failed: %s", textOf(res))
	}
	return nil
}

func (w *mcpWorld) iUpdateResourceTypeDescription(ctx context.Context, _, description string) error {
	m, err := w.resultMap() // the just-created type, carrying its id
	if err != nil {
		return err
	}
	id, _ := m["id"].(string)
	args := fmt.Sprintf(`{"id":%q,"description":%q}`, id, description)
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_type_update", Arguments: json.RawMessage(args),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *mcpWorld) theResourceTypeStillRequires(_, prop string) error {
	if err := w.theCallSucceeds(); err != nil {
		return err
	}
	m, err := w.resultMap()
	if err != nil {
		return err
	}
	schema, ok := m["schema"].(map[string]any)
	if !ok {
		return fmt.Errorf("update cleared the schema (issue #382 review): %s", w.lastText)
	}
	required, _ := schema["required"].([]any)
	for _, r := range required {
		if s, _ := r.(string); s == prop {
			return nil
		}
	}
	return fmt.Errorf("expected schema to still require %q after update; got: %s", prop, w.lastText)
}
