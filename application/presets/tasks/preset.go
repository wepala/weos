// Package tasks provides resource types for task and project management.
package tasks

import "weos/application"

// Register adds the tasks preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "tasks",
		Description: "Task management types: projects and tasks with status, priority, and due dates",
		Types: []application.PresetResourceType{
			application.NewPresetType("Project", "project",
				"A project that groups related tasks",
				`{"@vocab":"https://schema.org/","@type":"Project"}`,
				`{"type":"object","properties":{`+
					`"name":{"type":"string"},`+
					`"description":{"type":"string"},`+
					`"status":{"type":"string"}`+
					`},"required":["name"]}`,
			),
			application.NewPresetType("Task", "task",
				"An actionable item with status, priority, and optional due date",
				`{"@vocab":"https://schema.org/","@type":"Action","project":"https://schema.org/isPartOf"}`,
				`{"type":"object","properties":{`+
					`"name":{"type":"string"},`+
					`"description":{"type":"string"},`+
					`"status":{"type":"string"},`+
					`"priority":{"type":"string"},`+
					`"dueDate":{"type":"string","format":"date"},`+
					`"project":{"type":"string","x-resource-type":"project","x-display-property":"name"}`+
					`},"required":["name"]}`,
			),
		},
	})
}
