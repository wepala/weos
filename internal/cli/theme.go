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
	"path/filepath"

	"weos/application"

	"github.com/spf13/cobra"
)

var themeUploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload a theme from a zip file",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		filePath, _ := cmd.Flags().GetString("file")
		storagePath, _ := cmd.Flags().GetString("storage-path")
		nameOverride, _ := cmd.Flags().GetString("name")

		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer f.Close()

		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat file: %w", err)
		}

		result, err := deps.ThemeService.Upload(
			cmd.Context(),
			application.UploadThemeCommand{
				ZipReader:   f,
				ZipSize:     fi.Size(),
				StoragePath: storagePath,
				Name:        nameOverride,
				FileName:    filepath.Base(filePath),
			},
		)
		if err != nil {
			return fmt.Errorf("failed to upload theme: %w", err)
		}

		fmt.Fprintf(os.Stdout, "Theme uploaded: %s (%s)\n",
			result.Theme.Name(), result.Theme.GetID())
		fmt.Fprintf(os.Stdout, "Templates created: %d\n",
			len(result.Templates))
		for _, t := range result.Templates {
			fmt.Fprintf(os.Stdout, "  - %s (%s)\n", t.Name(), t.GetID())
		}
		return nil
	},
}

var themeCmd = &cobra.Command{
	Use:   "theme",
	Short: "Manage themes",
}

var themeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new theme",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		name, _ := cmd.Flags().GetString("name")
		slug, _ := cmd.Flags().GetString("slug")
		entity, err := deps.ThemeService.Create(
			cmd.Context(),
			application.CreateThemeCommand{Name: name, Slug: slug},
		)
		if err != nil {
			return fmt.Errorf("failed to create theme: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created theme: %s\n", entity.GetID())
		return nil
	},
}

var themeGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a theme by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		entity, err := deps.ThemeService.GetByID(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("theme not found: %w", err)
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"id":            entity.GetID(),
			"name":          entity.Name(),
			"slug":          entity.Slug(),
			"description":   entity.Description(),
			"version":       entity.Version(),
			"thumbnail_url": entity.ThumbnailURL(),
			"status":        entity.Status(),
		}, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var themeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all themes",
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		result, err := deps.ThemeService.List(cmd.Context(), cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list themes: %w", err)
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var themeDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a theme",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		err = deps.ThemeService.Delete(
			cmd.Context(),
			application.DeleteThemeCommand{ID: args[0]},
		)
		if err != nil {
			return fmt.Errorf("failed to delete theme: %w", err)
		}
		fmt.Fprintln(os.Stdout, "Theme deleted successfully")
		return nil
	},
}

func init() {
	themeCreateCmd.Flags().String("name", "", "Name of the theme")
	_ = themeCreateCmd.MarkFlagRequired("name")
	themeCreateCmd.Flags().String("slug", "", "URL-safe slug for the theme")
	_ = themeCreateCmd.MarkFlagRequired("slug")

	themeListCmd.Flags().Int("limit", 20, "Number of items per page")
	themeListCmd.Flags().String("cursor", "", "Pagination cursor")

	themeUploadCmd.Flags().String("file", "", "Path to the theme zip file")
	_ = themeUploadCmd.MarkFlagRequired("file")
	themeUploadCmd.Flags().String("storage-path", "themes", "Directory to extract theme files")
	themeUploadCmd.Flags().String("name", "", "Override the theme name")

	themeCmd.AddCommand(
		themeCreateCmd, themeGetCmd,
		themeListCmd, themeDeleteCmd,
		themeUploadCmd,
	)
	rootCmd.AddCommand(themeCmd)
}
