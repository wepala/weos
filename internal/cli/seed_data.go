package cli

import (
	"encoding/json"
	"fmt"
	"os"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
)

type seedUserDef struct {
	Name           string
	Email          string
	Provider       string
	ProviderUserID string
}

var seedUsers = []seedUserDef{
	{
		Name:           "Admin User",
		Email:          "admin@weos.dev",
		Provider:       "dev",
		ProviderUserID: "dev-admin-001",
	},
	{
		Name:           "Regular User",
		Email:          "member@weos.dev",
		Provider:       "dev",
		ProviderUserID: "dev-member-001",
	},
}

func userInfoFromDef(def seedUserDef) authapp.UserInfo {
	return authapp.UserInfo{
		ProviderUserID: def.ProviderUserID,
		Email:          def.Email,
		DisplayName:    def.Name,
		Provider:       def.Provider,
	}
}

type seedManifestUser struct {
	AgentID   string `json:"agentId"`
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
}

type seedManifest struct {
	Users     map[string]seedManifestUser `json:"users"`
	Presets   []string                    `json:"presets"`
	Resources map[string][]string         `json:"resources"`
}

const seedManifestPath = ".dev-seed.json"

func writeSeedManifest(manifest seedManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal seed manifest: %w", err)
	}
	if err := os.WriteFile(seedManifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write seed manifest: %w", err)
	}
	return nil
}
