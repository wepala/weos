package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/wepala/weos/application"

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
		defer func() { _ = deps.Shutdown() }()

		givenName, _ := cmd.Flags().GetString("given-name")
		familyName, _ := cmd.Flags().GetString("family-name")
		email, _ := cmd.Flags().GetString("email")
		data, _ := json.Marshal(map[string]any{
			"givenName":  givenName,
			"familyName": familyName,
			"email":      email,
		})
		entity, err := deps.ResourceService.Create(
			cmd.Context(),
			application.CreateResourceCommand{TypeSlug: "person", Data: data},
		)
		if err != nil {
			return fmt.Errorf("failed to create person: %w", err)
		}
		_, _ = fmt.Fprintf(os.Stdout, "Created person: %s\n", entity.GetID())
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
		defer func() { _ = deps.Shutdown() }()

		entity, err := deps.ResourceService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("person not found: %w", err)
		}
		fields, _ := application.ExtractResourceFields(entity)
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"given_name":  application.StringField(fields, "givenName"),
			"family_name": application.StringField(fields, "familyName"),
			"name":        application.StringField(fields, "name"),
			"email":       application.StringField(fields, "email"),
			"avatar_url":  application.StringField(fields, "avatarURL"),
			"status":      entity.Status(),
		}, "", "  ")
		_, _ = fmt.Fprintln(os.Stdout, string(data))
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
		defer func() { _ = deps.Shutdown() }()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.ResourceService.ListFlat(
			cmd.Context(), "person", cursor, limit, application.SortOptions{},
		)
		if err != nil {
			return fmt.Errorf("failed to list persons: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		_, _ = fmt.Fprintln(os.Stdout, string(data))
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
		defer func() { _ = deps.Shutdown() }()

		err = deps.ResourceService.Delete(
			cmd.Context(),
			application.DeleteResourceCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete person: %w", err)
		}
		_, _ = fmt.Fprintln(os.Stdout, "Person deleted successfully")
		return nil
	},
}

func init() {
	personCreateCmd.Flags().String("given-name", "", "Given name")
	_ = personCreateCmd.MarkFlagRequired("given-name")
	personCreateCmd.Flags().String("family-name", "", "Family name")
	_ = personCreateCmd.MarkFlagRequired("family-name")
	personCreateCmd.Flags().String("email", "", "Email address")

	personListCmd.Flags().Int("limit", 20, "Number of items per page")
	personListCmd.Flags().String("cursor", "", "Pagination cursor")

	personCmd.AddCommand(
		personCreateCmd, personGetCmd,
		personListCmd, personDeleteCmd,
	)
	rootCmd.AddCommand(personCmd)
}
