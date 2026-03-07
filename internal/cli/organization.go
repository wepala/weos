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

var organizationCmd = &cobra.Command{
	Use:   "organization",
	Short: "Manage organizations",
	Aliases: []string{"org"},
}

var organizationCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new organization",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		name, _ := cmd.Flags().GetString("name")
		slug, _ := cmd.Flags().GetString("slug")
		entity, err := deps.OrganizationService.Create(
			cmd.Context(),
			application.CreateOrganizationCommand{Name: name, Slug: slug},
		)
		if err != nil {
			return fmt.Errorf("failed to create organization: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created organization: %s\n", entity.GetID())
		return nil
	},
}

var organizationGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get an organization by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		entity, err := deps.OrganizationService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("organization not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"name":        entity.Name(),
			"slug":        entity.Slug(),
			"description": entity.Description(),
			"url":         entity.URL(),
			"logo_url":    entity.LogoURL(),
			"status":      entity.Status(),
		}, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var organizationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all organizations",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.OrganizationService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list organizations: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var organizationDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete an organization",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		err = deps.OrganizationService.Delete(
			cmd.Context(),
			application.DeleteOrganizationCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete organization: %w", err)
		}
		fmt.Fprintln(os.Stdout, "Organization deleted successfully")
		return nil
	},
}

func init() {
	organizationCreateCmd.Flags().String("name", "", "Name of the organization")
	_ = organizationCreateCmd.MarkFlagRequired("name")
	organizationCreateCmd.Flags().String("slug", "", "URL-safe slug")
	_ = organizationCreateCmd.MarkFlagRequired("slug")

	organizationListCmd.Flags().Int("limit", 20, "Number of items per page")
	organizationListCmd.Flags().String("cursor", "", "Pagination cursor")

	organizationCmd.AddCommand(
		organizationCreateCmd, organizationGetCmd,
		organizationListCmd, organizationDeleteCmd,
	)
	rootCmd.AddCommand(organizationCmd)
}
