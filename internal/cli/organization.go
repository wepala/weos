package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"weos/application"

	"github.com/spf13/cobra"
)

var organizationCmd = &cobra.Command{
	Use:     "organization",
	Short:   "Manage organizations",
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
		data, _ := json.Marshal(map[string]any{
			"name": name,
			"slug": slug,
		})
		entity, err := deps.ResourceService.Create(
			cmd.Context(),
			application.CreateResourceCommand{TypeSlug: "organization", Data: data},
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

		entity, err := deps.ResourceService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("organization not found: %w", err)
		}
		fields, _ := application.ExtractResourceFields(entity)
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":          entity.GetID(),
			"name":        application.StringField(fields, "name"),
			"slug":        application.StringField(fields, "slug"),
			"description": application.StringField(fields, "description"),
			"url":         application.StringField(fields, "url"),
			"logo_url":    application.StringField(fields, "logoURL"),
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
		result, err := deps.ResourceService.ListFlat(
			cmd.Context(), "organization", cursor, limit, application.SortOptions{},
		)
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

		err = deps.ResourceService.Delete(
			cmd.Context(),
			application.DeleteResourceCommand{ID: args[0]},
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
