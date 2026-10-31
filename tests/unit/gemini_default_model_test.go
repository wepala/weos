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

package unit

import (
	"testing"

	"github.com/wepala/weos/v3/infrastructure/agents"
	"github.com/wepala/weos/v3/internal/config"
)

// wantGeminiDefault is spelled out here rather than read from the config
// constant: the test pins the model name itself, so moving the default is a
// deliberate edit in two places. Google refuses gemini-2.5-flash to keys
// created now with a 404, and no unit test can call the live API to catch that.
const wantGeminiDefault = "gemini-3.8-flash"

func TestGeminiDefaultModel_ConfigDefault(t *testing.T) {
	t.Setenv("GEMINI_MODEL", "")

	cfg := config.Default()
	cfg.LoadFromEnvironment()

	if cfg.LLM.GeminiModel != wantGeminiDefault {
		t.Fatalf("LLM.GeminiModel = %q, want %q", cfg.LLM.GeminiModel, wantGeminiDefault)
	}
}

func TestGeminiDefaultModel_EnvironmentOverrides(t *testing.T) {
	t.Setenv("GEMINI_MODEL", "gemini-custom-model")

	cfg := config.Default()
	cfg.LoadFromEnvironment()

	if cfg.LLM.GeminiModel != "gemini-custom-model" {
		t.Fatalf("LLM.GeminiModel = %q, want the GEMINI_MODEL value", cfg.LLM.GeminiModel)
	}
}

// A Config built by hand, not through Default, reaches NewADKConfig with no
// model set, so the ADK config needs the same fallback.
func TestGeminiDefaultModel_ADKConfigFallback(t *testing.T) {
	var cfg config.Config
	cfg.LLM.GeminiAPIKey = "test-key"

	adk := agents.NewADKConfig(cfg)
	if adk == nil {
		t.Fatal("NewADKConfig returned nil with an API key set")
	}
	if adk.ModelID != wantGeminiDefault {
		t.Fatalf("ModelID = %q, want %q", adk.ModelID, wantGeminiDefault)
	}
}

func TestGeminiDefaultModel_ADKConfigKeepsExplicitModel(t *testing.T) {
	var cfg config.Config
	cfg.LLM.GeminiAPIKey = "test-key"
	cfg.LLM.GeminiModel = "gemini-custom-model"

	adk := agents.NewADKConfig(cfg)
	if adk == nil {
		t.Fatal("NewADKConfig returned nil with an API key set")
	}
	if adk.ModelID != "gemini-custom-model" {
		t.Fatalf("ModelID = %q, want the configured model", adk.ModelID)
	}
}
