package mcp

import (
	"context"

	"weos/application"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PresetListInput struct{}

type PresetListOutput struct {
	Presets []PresetSummary `json:"presets"`
}

type PresetSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Types       []string `json:"types"`
}

type PresetInstallInput struct {
	Name   string `json:"name" jsonschema:"preset name to install (core, auth, website, events, tasks, knowledge)"`
	Update bool   `json:"update,omitempty" jsonschema:"update existing resource types with preset definitions instead of skipping"`
}

type PresetInstallOutput struct {
	Created []string `json:"created"`
	Updated []string `json:"updated,omitempty"`
	Skipped []string `json:"skipped"`
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
			out.Presets = append(out.Presets, PresetSummary{
				Name:        d.Name,
				Description: d.Description,
				Types:       slugs,
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
			return nil, PresetInstallOutput{}, err
		}
		return nil, PresetInstallOutput{
			Created: result.Created,
			Updated: result.Updated,
			Skipped: result.Skipped,
		}, nil
	})
}
