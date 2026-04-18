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

package agents

import (
	"context"
	"fmt"

	"github.com/wepala/weos/v3/internal/config"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

// ADKConfig holds configuration for Google ADK.
type ADKConfig struct {
	APIKey  string
	ModelID string
}

// NewADKConfig creates a new ADK configuration from the base config.
// Returns nil if no API key is configured.
func NewADKConfig(cfg config.Config) *ADKConfig {
	apiKey := cfg.LLM.GeminiAPIKey
	if apiKey == "" {
		return nil
	}

	modelID := cfg.LLM.GeminiModel
	if modelID == "" {
		modelID = "gemini-2.0-flash"
	}

	return &ADKConfig{
		APIKey:  apiKey,
		ModelID: modelID,
	}
}

// CreateGeminiModel creates a Gemini model instance using the ADK config.
func (c *ADKConfig) CreateGeminiModel(ctx context.Context) (model.LLM, error) {
	m, err := gemini.NewModel(ctx, c.ModelID, &genai.ClientConfig{
		APIKey: c.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model: %w", err)
	}

	return m, nil
}

// ProvideADKConfig is the Fx provider for ADKConfig. Returns nil when no API key is set.
func ProvideADKConfig(cfg config.Config) *ADKConfig {
	return NewADKConfig(cfg)
}
