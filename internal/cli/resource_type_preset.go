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
	"fmt"
	"os"
	"strings"

	"weos/application"

	"github.com/spf13/cobra"
)

var presetCmd = &cobra.Command{
	Use:   "preset",
	Short: "Manage resource type presets",
}

var presetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available resource type presets",
	RunE: func(cmd *cobra.Command, args []string) error {
		defs := application.ListPresetDefinitions()
		for _, d := range defs {
			slugs := make([]string, len(d.Types))
			for i, t := range d.Types {
				slugs[i] = t.Slug
			}
			fmt.Fprintf(os.Stdout, "%-12s %s\n", d.Name, d.Description)
			fmt.Fprintf(os.Stdout, "             Types: %s\n\n", strings.Join(slugs, ", "))
		}
		return nil
	},
}

var presetInstallCmd = &cobra.Command{
	Use:   "install [name]",
	Short: "Install a resource type preset",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer deps.Shutdown()

		update, _ := cmd.Flags().GetBool("update")
		result, err := deps.ResourceTypeService.InstallPreset(cmd.Context(), args[0], update)
		if err != nil {
			return fmt.Errorf("failed to install preset: %w", err)
		}
		if len(result.Created) > 0 {
			fmt.Fprintf(os.Stdout, "Created: %s\n", strings.Join(result.Created, ", "))
		}
		if len(result.Updated) > 0 {
			fmt.Fprintf(os.Stdout, "Updated: %s\n", strings.Join(result.Updated, ", "))
		}
		if len(result.Skipped) > 0 {
			fmt.Fprintf(os.Stdout, "Skipped (already exist): %s\n", strings.Join(result.Skipped, ", "))
		}
		if len(result.Created) == 0 && len(result.Updated) == 0 && len(result.Skipped) == 0 {
			fmt.Fprintln(os.Stdout, "Preset has no types to install")
		}
		return nil
	},
}

func init() {
	presetInstallCmd.Flags().Bool("update", false, "Update existing resource types with preset definitions instead of skipping them")
	presetCmd.AddCommand(presetListCmd, presetInstallCmd)
	resourceTypeCmd.AddCommand(presetCmd)
}
