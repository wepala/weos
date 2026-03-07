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

var personCmd = &cobra.Command{
	Use:   "person",
	Short: "Manage persons",
}

var personCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new person",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		givenName, _ := cmd.Flags().GetString("given-name")
		familyName, _ := cmd.Flags().GetString("family-name")
		email, _ := cmd.Flags().GetString("email")
		entity, err := deps.PersonService.Create(
			cmd.Context(),
			application.CreatePersonCommand{
				GivenName:  givenName,
				FamilyName: familyName,
				Email:      email,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create person: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created person: %s\n", entity.GetID())
		return nil
	},
}

var personGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a person by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		entity, err := deps.PersonService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("person not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"given_name":  entity.GivenName(),
			"family_name": entity.FamilyName(),
			"name":        entity.Name(),
			"email":       entity.Email(),
			"avatar_url":  entity.AvatarURL(),
			"status":      entity.Status(),
		}, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var personListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all persons",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.PersonService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list persons: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var personDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a person",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		err = deps.PersonService.Delete(
			cmd.Context(),
			application.DeletePersonCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete person: %w", err)
		}
		fmt.Fprintln(os.Stdout, "Person deleted successfully")
		return nil
	},
}

func init() {
	personCreateCmd.Flags().String("given-name", "", "Given name")
	_ = personCreateCmd.MarkFlagRequired("given-name")
	personCreateCmd.Flags().String("family-name", "", "Family name")
	_ = personCreateCmd.MarkFlagRequired("family-name")
	personCreateCmd.Flags().String("email", "", "Email address")
	_ = personCreateCmd.MarkFlagRequired("email")

	personListCmd.Flags().Int("limit", 20, "Number of items per page")
	personListCmd.Flags().String("cursor", "", "Pagination cursor")

	personCmd.AddCommand(
		personCreateCmd, personGetCmd,
		personListCmd, personDeleteCmd,
	)
	rootCmd.AddCommand(personCmd)
}
