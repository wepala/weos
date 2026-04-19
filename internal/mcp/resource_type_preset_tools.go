package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/wepala/weos/v3/application"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PresetListInput struct{}

type PresetListOutput struct {
	Presets []PresetSummary `json:"presets"`
}

type PresetSummary struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Types       []string             `json:"types"`
	Behaviors   []PresetBehaviorMeta `json:"behaviors,omitempty"`
}

type PresetBehaviorMeta struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
	Manageable  bool   `json:"manageable"`
}

type PresetInstallInput struct {
	Name   string `json:"name" jsonschema:"preset name to install (core, website, events, tasks, knowledge, ecommerce, meal-planning)"`
	Update bool   `json:"update,omitempty" jsonschema:"update existing resource types with preset definitions instead of skipping"`
}

type PresetInstallOutput struct {
	Created []string       `json:"created"`
	Updated []string       `json:"updated,omitempty"`
	Skipped []string       `json:"skipped"`
	Seeded  map[string]int `json:"seeded,omitempty"`
}

func registerResourceTypePresetTools(server *mcp.Server, svc application.ResourceTypeService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_preset_list",
		Description: "List available resource type presets and their included types.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, _ PresetListInput,
	) (*mcp.CallToolResult, PresetListOutput, error) {
		defs := svc.ListPresets()
		out := PresetListOutput{Presets: make([]PresetSummary, 0, len(defs))}
		for _, d := range defs {
			slugs := make([]string, len(d.Types))
			for i, t := range d.Types {
				slugs[i] = t.Slug
			}
			var behaviors []PresetBehaviorMeta
			for _, m := range d.BehaviorMeta {
				behaviors = append(behaviors, PresetBehaviorMeta{
					Slug:        m.Slug,
					DisplayName: m.DisplayName,
					Description: m.Description,
					Default:     m.Default,
					Manageable:  m.Manageable,
				})
			}
			sort.Slice(behaviors, func(i, j int) bool {
				return behaviors[i].Slug < behaviors[j].Slug
			})
			out.Presets = append(out.Presets, PresetSummary{
				Name:        d.Name,
				Description: d.Description,
				Types:       slugs,
				Behaviors:   behaviors,
			})
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_preset_install",
		Description: "Install a resource type preset. By default skips types that already exist; set update=true to sync them with the preset definition.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input PresetInstallInput,
	) (*mcp.CallToolResult, PresetInstallOutput, error) {
		result, err := svc.InstallPreset(ctx, input.Name, input.Update)
		if err != nil {
			if result == nil {
				return nil, PresetInstallOutput{}, err
			}
			return nil, PresetInstallOutput{
				Created: result.Created,
				Updated: result.Updated,
				Skipped: result.Skipped,
				Seeded:  result.Seeded,
			}, err
		}
		if result == nil {
			return nil, PresetInstallOutput{}, fmt.Errorf("install preset %q returned nil result", input.Name)
		}
		return nil, PresetInstallOutput{
			Created: result.Created,
			Updated: result.Updated,
			Skipped: result.Skipped,
			Seeded:  result.Seeded,
		}, nil
	})

	registerBehaviorTools(server, svc)
}

type BehaviorListInput struct {
	TypeSlug string `json:"type_slug" jsonschema:"resource type slug to list behaviors for"`
}

type BehaviorListOutput struct {
	Behaviors []application.BehaviorInfo `json:"behaviors"`
}

type BehaviorSetInput struct {
	TypeSlug string   `json:"type_slug" jsonschema:"resource type slug"`
	Slugs    []string `json:"slugs" jsonschema:"behavior slugs to enable"`
}

type BehaviorSetOutput struct {
	Success bool `json:"success"`
}

func registerBehaviorTools(server *mcp.Server, svc application.ResourceTypeService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_behavior_list",
		Description: "List available behaviors for a resource type with their enabled state in the current account.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input BehaviorListInput,
	) (*mcp.CallToolResult, BehaviorListOutput, error) {
		behaviors, err := svc.ListBehaviors(ctx, input.TypeSlug)
		if err != nil {
			return nil, BehaviorListOutput{}, err
		}
		return nil, BehaviorListOutput{Behaviors: behaviors}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resource_type_behavior_set",
		Description: "Set which manageable behaviors are enabled for a resource type in the current account.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input BehaviorSetInput,
	) (*mcp.CallToolResult, BehaviorSetOutput, error) {
		slugs := input.Slugs
		if slugs == nil {
			slugs = []string{}
		}
		if err := svc.SetBehaviors(ctx, input.TypeSlug, slugs); err != nil {
			return nil, BehaviorSetOutput{}, err
		}
		return nil, BehaviorSetOutput{Success: true}, nil
	})
}
