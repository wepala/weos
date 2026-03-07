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

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Manage pages",
}

var pageCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new page",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		name, _ := cmd.Flags().GetString("name")
		slug, _ := cmd.Flags().GetString("slug")
		websiteID, _ := cmd.Flags().GetString("website-id")
		entity, err := deps.PageService.Create(
			cmd.Context(),
			application.CreatePageCommand{
				WebsiteID: websiteID, Name: name, Slug: slug,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create page: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created page: %s\n", entity.GetID())
		return nil
	},
}

var pageGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a page by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		entity, err := deps.PageService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("page not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"name":        entity.Name(),
			"slug":        entity.Slug(),
			"description": entity.Description(),
			"template":    entity.Template(),
			"position":    entity.Position(),
			"status":      entity.Status(),
		}, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var pageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all pages",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.PageService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list pages: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var pageDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		err = deps.PageService.Delete(
			cmd.Context(),
			application.DeletePageCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete page: %w", err)
		}
		fmt.Fprintln(os.Stdout, "Page deleted successfully")
		return nil
	},
}

func init() {
	pageCreateCmd.Flags().String("name", "", "Name of the page")
	_ = pageCreateCmd.MarkFlagRequired("name")
	pageCreateCmd.Flags().String("slug", "", "URL slug of the page (auto-generated from name if omitted)")
	pageCreateCmd.Flags().String("website-id", "", "Parent website ID")
	_ = pageCreateCmd.MarkFlagRequired("website-id")

	pageListCmd.Flags().Int("limit", 20, "Number of items per page")
	pageListCmd.Flags().String("cursor", "", "Pagination cursor")

	pageCmd.AddCommand(
		pageCreateCmd, pageGetCmd, pageListCmd, pageDeleteCmd,
	)
	rootCmd.AddCommand(pageCmd)
}
