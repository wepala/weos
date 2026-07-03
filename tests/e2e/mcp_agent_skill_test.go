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

// TestMCPAgentSkills runs the declarative agent-skill acceptance scenarios
// (epic #397, story #400): the agents preset ships the example skill, and
// skill definitions are schema-validated resources.
func TestMCPAgentSkills(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "mcp-agent-skills",
		ScenarioInitializer: initAgentSkillScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/agent_skill_definitions.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("agent-skill acceptance scenarios failed")
	}
}

// agentSkillWorld extends the shared MCP world with preset installation and
// listing steps.
type agentSkillWorld struct {
	*mcpWorld
}

func initAgentSkillScenario(sc *godog.ScenarioContext) {
	w := &agentSkillWorld{mcpWorld: &mcpWorld{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^I call resource_list for type "([^"]*)"$`, w.iCallResourceList)
	sc.Step(`^the listing includes a resource named "([^"]*)"$`, w.listingIncludesName)
	sc.Step(`^I call resource_create for type "([^"]*)" with the data:$`, w.iCallResourceCreate)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the returned resource has a "([^"]*)" URN identifier$`, w.returnedResourceHasURN)
	sc.Step(`^the error names the missing property "([^"]*)"$`, w.errorNamesMissingProperty)
	sc.Step(`^the error names the invalid property "([^"]*)"$`, w.errorNamesInvalidProperty)
}

func (w *agentSkillWorld) presetIsInstalled(ctx context.Context, name string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted")
	}
	if _, err := w.rts.InstallPreset(ctx, name, true); err != nil {
		return fmt.Errorf("failed to install %q preset: %w", name, err)
	}
	return nil
}

func (w *agentSkillWorld) iCallResourceList(ctx context.Context, typeSlug string) error {
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name:      "resource_list",
		Arguments: json.RawMessage(fmt.Sprintf(`{"type_slug":%q,"limit":50}`, typeSlug)),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

func (w *agentSkillWorld) listingIncludesName(name string) error {
	if err := w.theCallSucceeds(); err != nil {
		return err
	}
	if !strings.Contains(w.lastText, fmt.Sprintf(`"name":%q`, name)) {
		return fmt.Errorf("expected listing to include a resource named %q, got: %s", name, w.lastText)
	}
	return nil
}

// errorNamesInvalidProperty accepts either schema-validation phrasing (enum
// violation naming the property) or the SDK's protocol-level message, as long
// as the property is named.
func (w *agentSkillWorld) errorNamesInvalidProperty(prop string) error {
	msg := strings.ToLower(w.errMessage())
	if !strings.Contains(msg, strings.ToLower(prop)) {
		return fmt.Errorf("expected the error to name the invalid property %q, got: %s", prop, w.errMessage())
	}
	return nil
}
