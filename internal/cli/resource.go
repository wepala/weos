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

	"github.com/wepala/weos/application"
	"github.com/wepala/weos/domain/repositories"

	"github.com/spf13/cobra"
)

var resourceCmd = &cobra.Command{
	Use:     "resource",
	Short:   "Manage resources",
	Aliases: []string{"res"},
}

var resourceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new resource",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		typeSlug, _ := cmd.Flags().GetString("type")
		dataStr, _ := cmd.Flags().GetString("data")
		entity, err := deps.ResourceService.Create(
			cmd.Context(),
			application.CreateResourceCommand{
				TypeSlug: typeSlug,
				Data:     json.RawMessage(dataStr),
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create resource: %w", err)
		}
		_, _ = fmt.Fprintf(os.Stdout, "Created resource: %s\n", entity.GetID())
		return nil
	},
}

var resourceGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a resource by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		entity, err := deps.ResourceService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("resource not found: %w", err)
		}
		data, _ := json.MarshalIndent(json.RawMessage(entity.Data()), "", "  ")
		_, _ = fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var resourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List resources of a given type",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		typeSlug, _ := cmd.Flags().GetString("type")
		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.ResourceService.List(cmd.Context(), typeSlug, cursor, limit,
			repositories.SortOptions{})
		if err != nil {
			return fmt.Errorf("failed to list resources: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		_, _ = fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var resourceDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a resource",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		err = deps.ResourceService.Delete(
			cmd.Context(),
			application.DeleteResourceCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete resource: %w", err)
		}
		_, _ = fmt.Fprintln(os.Stdout, "Resource deleted successfully")
		return nil
	},
}

func init() {
	resourceCreateCmd.Flags().String("type", "", "Resource type slug")
	_ = resourceCreateCmd.MarkFlagRequired("type")
	resourceCreateCmd.Flags().String("data", "", "Resource data (JSON string)")
	_ = resourceCreateCmd.MarkFlagRequired("data")

	resourceListCmd.Flags().String("type", "", "Resource type slug")
	_ = resourceListCmd.MarkFlagRequired("type")
	resourceListCmd.Flags().Int("limit", 20, "Number of items per page")
	resourceListCmd.Flags().String("cursor", "", "Pagination cursor")

	resourceCmd.AddCommand(
		resourceCreateCmd, resourceGetCmd,
		resourceListCmd, resourceDeleteCmd,
	)
	rootCmd.AddCommand(resourceCmd)
}
