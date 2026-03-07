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

var websiteCmd = &cobra.Command{
	Use:   "website",
	Short: "Manage websites",
}

var websiteCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new website",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		name, _ := cmd.Flags().GetString("name")
		url, _ := cmd.Flags().GetString("url")
		slug, _ := cmd.Flags().GetString("slug")
		entity, err := deps.WebsiteService.Create(
			cmd.Context(),
			application.CreateWebsiteCommand{Name: name, URL: url, Slug: slug},
		)
		if err != nil {
			return fmt.Errorf("failed to create website: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created website: %s\n", entity.GetID())
		return nil
	},
}

var websiteGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a website by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		entity, err := deps.WebsiteService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("website not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"name":        entity.Name(),
			"slug":        entity.Slug(),
			"url":         entity.URL(),
			"description": entity.Description(),
			"language":    entity.Language(),
			"status":      entity.Status(),
		}, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var websiteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all websites",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.WebsiteService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list websites: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var websiteDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a website",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		err = deps.WebsiteService.Delete(
			cmd.Context(),
			application.DeleteWebsiteCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete website: %w", err)
		}
		fmt.Fprintln(os.Stdout, "Website deleted successfully")
		return nil
	},
}

func init() {
	websiteCreateCmd.Flags().String("name", "", "Name of the website")
	_ = websiteCreateCmd.MarkFlagRequired("name")
	websiteCreateCmd.Flags().String("url", "", "URL of the website")
	_ = websiteCreateCmd.MarkFlagRequired("url")
	websiteCreateCmd.Flags().String("slug", "", "Slug for the website (auto-generated from name if omitted)")

	websiteListCmd.Flags().Int("limit", 20, "Number of items per page")
	websiteListCmd.Flags().String("cursor", "", "Pagination cursor")

	websiteCmd.AddCommand(
		websiteCreateCmd, websiteGetCmd,
		websiteListCmd, websiteDeleteCmd,
	)
	rootCmd.AddCommand(websiteCmd)
}
