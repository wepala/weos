package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPToolAnnotations runs the tool-annotation acceptance scenarios
// (epic #397, story #399): every tool advertises whether it mutates the
// instance, which is what gates writes behind user confirmation in the
// in-app agent.
func TestMCPToolAnnotations(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "mcp-tool-annotations",
		ScenarioInitializer: initToolAnnotationScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/tool_annotations.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("tool-annotation acceptance scenarios failed")
	}
}

// toolAnnotationWorld extends the shared MCP world with a tools/list snapshot.
type toolAnnotationWorld struct {
	*mcpWorld
	tools map[string]*mcp.Tool
}

func initToolAnnotationScenario(sc *godog.ScenarioContext) {
	w := &toolAnnotationWorld{mcpWorld: &mcpWorld{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^I list the server's tools$`, w.iListTools)
	sc.Step(`^the tool "([^"]*)" is advertised as read-only$`, w.toolIsReadOnly)
	sc.Step(`^the tool "([^"]*)" is advertised as mutating$`, w.toolIsMutating)
	sc.Step(`^the tool "([^"]*)" is advertised as destructive$`, w.toolIsDestructive)
	sc.Step(`^every tool declares whether it is read-only or mutating$`, w.everyToolIsAnnotated)
}

func (w *toolAnnotationWorld) iListTools(ctx context.Context) error {
	res, err := w.client.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	w.tools = make(map[string]*mcp.Tool, len(res.Tools))
	for _, t := range res.Tools {
		w.tools[t.Name] = t
	}
	return nil
}

func (w *toolAnnotationWorld) tool(name string) (*mcp.Tool, error) {
	t, ok := w.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q is not advertised", name)
	}
	if t.Annotations == nil {
		return nil, fmt.Errorf("tool %q has no annotations", name)
	}
	return t, nil
}

func (w *toolAnnotationWorld) toolIsReadOnly(name string) error {
	t, err := w.tool(name)
	if err != nil {
		return err
	}
	if !t.Annotations.ReadOnlyHint {
		return fmt.Errorf("tool %q is not advertised as read-only", name)
	}
	return nil
}

func (w *toolAnnotationWorld) toolIsMutating(name string) error {
	t, err := w.tool(name)
	if err != nil {
		return err
	}
	if t.Annotations.ReadOnlyHint {
		return fmt.Errorf("tool %q is advertised as read-only, expected mutating", name)
	}
	return nil
}

func (w *toolAnnotationWorld) toolIsDestructive(name string) error {
	if err := w.toolIsMutating(name); err != nil {
		return err
	}
	t := w.tools[name]
	if t.Annotations.DestructiveHint == nil || !*t.Annotations.DestructiveHint {
		return fmt.Errorf("tool %q is not advertised as destructive", name)
	}
	return nil
}

func (w *toolAnnotationWorld) everyToolIsAnnotated() error {
	if len(w.tools) == 0 {
		return fmt.Errorf("no tools advertised")
	}
	for name, t := range w.tools {
		a := t.Annotations
		if a == nil {
			return fmt.Errorf("tool %q declares no annotations", name)
		}
		// The hints the epic's confirmation gating depends on must be
		// explicit, not defaulted: closed-world on every tool, and a
		// destructive-vs-additive declaration on every mutating tool.
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			return fmt.Errorf("tool %q must explicitly declare a closed world (OpenWorldHint=false)", name)
		}
		if !a.ReadOnlyHint && a.DestructiveHint == nil {
			return fmt.Errorf("mutating tool %q must declare DestructiveHint explicitly", name)
		}
	}
	return nil
}
