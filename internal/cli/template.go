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

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage templates",
}

var templateCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new template",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		themeID, _ := cmd.Flags().GetString("theme-id")
		name, _ := cmd.Flags().GetString("name")
		slug, _ := cmd.Flags().GetString("slug")
		entity, err := deps.TemplateService.Create(
			cmd.Context(),
			application.CreateTemplateCommand{
				ThemeID: themeID,
				Name:    name,
				Slug:    slug,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create template: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created template: %s\n", entity.GetID())
		return nil
	},
}

var templateGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a template by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		entity, err := deps.TemplateService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("template not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"name":        entity.Name(),
			"slug":        entity.Slug(),
			"description": entity.Description(),
			"file_path":   entity.FilePath(),
			"status":      entity.Status(),
		}, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		themeID, _ := cmd.Flags().GetString("theme-id")

		if themeID != "" {
			result, err := deps.TemplateService.ListByThemeID(
				cmd.Context(), themeID, cursor, limit)
			if err != nil {
				return fmt.Errorf("failed to list templates: %w", err)
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		result, err := deps.TemplateService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list templates: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var templateDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		err = deps.TemplateService.Delete(
			cmd.Context(),
			application.DeleteTemplateCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete template: %w", err)
		}
		fmt.Fprintln(os.Stdout, "Template deleted successfully")
		return nil
	},
}

func init() {
	templateCreateCmd.Flags().String("theme-id", "", "ID of the parent theme")
	_ = templateCreateCmd.MarkFlagRequired("theme-id")
	templateCreateCmd.Flags().String("name", "", "Name of the template")
	_ = templateCreateCmd.MarkFlagRequired("name")
	templateCreateCmd.Flags().String("slug", "", "URL-safe slug for the template")
	_ = templateCreateCmd.MarkFlagRequired("slug")

	templateListCmd.Flags().Int("limit", 20, "Number of items per page")
	templateListCmd.Flags().String("cursor", "", "Pagination cursor")
	templateListCmd.Flags().String("theme-id", "", "Filter by theme ID")

	templateCmd.AddCommand(
		templateCreateCmd, templateGetCmd,
		templateListCmd, templateDeleteCmd,
	)
	rootCmd.AddCommand(templateCmd)
}
