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

var sectionCmd = &cobra.Command{
	Use:   "section",
	Short: "Manage sections",
}

var sectionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new section",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		name, _ := cmd.Flags().GetString("name")
		slot, _ := cmd.Flags().GetString("slot")
		pageID, _ := cmd.Flags().GetString("page-id")
		entity, err := deps.SectionService.Create(
			cmd.Context(),
			application.CreateSectionCommand{
				PageID: pageID, Name: name, Slot: slot,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create section: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created section: %s\n", entity.GetID())
		return nil
	},
}

var sectionGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a section by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		entity, err := deps.SectionService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("section not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"name":        entity.Name(),
			"slot":        entity.Slot(),
			"entity_type": entity.EntityType(),
			"content":     entity.Content(),
			"position":    entity.Position(),
		}, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var sectionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sections",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.SectionService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list sections: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var sectionDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a section",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		err = deps.SectionService.Delete(
			cmd.Context(),
			application.DeleteSectionCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete section: %w", err)
		}
		fmt.Fprintln(os.Stdout, "Section deleted successfully")
		return nil
	},
}

func init() {
	sectionCreateCmd.Flags().String("name", "", "Name of the section")
	_ = sectionCreateCmd.MarkFlagRequired("name")
	sectionCreateCmd.Flags().String("slot", "",
		"Slot identifier (e.g. hero.headline)")
	_ = sectionCreateCmd.MarkFlagRequired("slot")
	sectionCreateCmd.Flags().String("page-id", "", "Parent page ID")
	_ = sectionCreateCmd.MarkFlagRequired("page-id")

	sectionListCmd.Flags().Int("limit", 20, "Number of items per page")
	sectionListCmd.Flags().String("cursor", "", "Pagination cursor")

	sectionCmd.AddCommand(
		sectionCreateCmd, sectionGetCmd,
		sectionListCmd, sectionDeleteCmd,
	)
	rootCmd.AddCommand(sectionCmd)
}
