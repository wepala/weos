// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"weos/application"

	"github.com/spf13/cobra"
)

var resourceTypeCmd = &cobra.Command{
	Use:     "resource-type",
	Short:   "Manage resource types",
	Aliases: []string{"rt"},
}

var resourceTypeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new resource type",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		name, _ := cmd.Flags().GetString("name")
		slug, _ := cmd.Flags().GetString("slug")
		description, _ := cmd.Flags().GetString("description")
		ctxStr, _ := cmd.Flags().GetString("context")
		schemaStr, _ := cmd.Flags().GetString("schema")
		var ctx json.RawMessage
		if ctxStr != "" {
			ctx = json.RawMessage(ctxStr)
		}
		var schema json.RawMessage
		if schemaStr != "" {
			schema = json.RawMessage(schemaStr)
		}
		entity, err := deps.ResourceTypeService.Create(
			cmd.Context(),
			application.CreateResourceTypeCommand{
				Name: name, Slug: slug, Description: description,
				Context: ctx, Schema: schema,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create resource type: %w", err)
		}
		_, _ = fmt.Fprintf(os.Stdout, "Created resource type: %s\n", entity.GetID())
		return nil
	},
}

var resourceTypeGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a resource type by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		entity, err := deps.ResourceTypeService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("resource type not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]any{
			"id":          entity.GetID(),
			"name":        entity.Name(),
			"slug":        entity.Slug(),
			"description": entity.Description(),
			"context":     jsonOrNil(entity.Context()),
			"schema":      jsonOrNil(entity.Schema()),
			"status":      entity.Status(),
		}, "", "  ")
		_, _ = fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var resourceTypeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all resource types",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.ResourceTypeService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list resource types: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		_, _ = fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var resourceTypeDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a resource type",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		err = deps.ResourceTypeService.Delete(
			cmd.Context(),
			application.DeleteResourceTypeCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete resource type: %w", err)
		}
		_, _ = fmt.Fprintln(os.Stdout, "Resource type deleted successfully")
		return nil
	},
}

func jsonOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func init() {
	resourceTypeCreateCmd.Flags().String("name", "", "Name of the resource type")
	_ = resourceTypeCreateCmd.MarkFlagRequired("name")
	resourceTypeCreateCmd.Flags().String("slug", "", "URL-safe slug")
	_ = resourceTypeCreateCmd.MarkFlagRequired("slug")
	resourceTypeCreateCmd.Flags().String("description", "", "Resource type description")
	resourceTypeCreateCmd.Flags().String("context", "", "JSON-LD context (JSON string)")
	resourceTypeCreateCmd.Flags().String("schema", "", "JSON Schema for validation (JSON string)")

	resourceTypeListCmd.Flags().Int("limit", 20, "Number of items per page")
	resourceTypeListCmd.Flags().String("cursor", "", "Pagination cursor")

	resourceTypeCmd.AddCommand(
		resourceTypeCreateCmd, resourceTypeGetCmd,
		resourceTypeListCmd, resourceTypeDeleteCmd,
	)
	rootCmd.AddCommand(resourceTypeCmd)
}
