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
	"os"

	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"
	gormdb "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session/database"
)

// ProvideOrchestrator is the fx provider for the in-app agent. Without a
// configured LLM key it returns an unconfigured orchestrator whose Converse
// reports ErrNotConfigured — the rest of the app is unaffected (the same
// graceful no-op contract as memory consolidation).
//
// Conversation sessions persist in the instance's own database (ADK's
// GORM-backed session service over the same DSN), so multi-turn
// conversations survive process restarts.
func ProvideOrchestrator(cfg config.Config, logger entities.Logger) (*appagents.Orchestrator, error) {
	m, err := provideModel(cfg, logger)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return appagents.NewOrchestrator(nil, nil, logger), nil
	}

	// The agent session store is high-churn (an event appended per turn
	// step) and independent of the resource graph. On SQLite it shares the
	// single-writer lock with the resource store and the background
	// subscribers, so under load — a conversation turn writing while memory
	// consolidation writes a fact — the two contend. AGENT_SESSION_DSN
	// points the session store at its own database to remove that
	// contention; it defaults to the main DSN (one file, unchanged
	// behavior). Postgres has no single-writer limit, so this only matters
	// for SQLite deployments.
	sessionDSN := cfg.DatabaseDSN
	if v := os.Getenv("AGENT_SESSION_DSN"); v != "" { //nolint:forbidigo // SQLite-only session split, see comment above
		sessionDSN = v
	}
	sessions, err := database.NewSessionService(gormdb.DialectorForDSN(sessionDSN))
	if err != nil {
		return nil, fmt.Errorf("create agent session service: %w", err)
	}
	if err := database.AutoMigrate(sessions); err != nil {
		return nil, fmt.Errorf("migrate agent session tables: %w", err)
	}

	return appagents.NewOrchestrator(m, sessions, logger), nil
}

// provideModel picks the agent's LLM: the scripted test model when
// WEOS_AGENT_SCRIPT is set (deterministic turns, no key — see #420), else
// Gemini when configured, else nil (agent unconfigured).
func provideModel(cfg config.Config, logger entities.Logger) (model.LLM, error) {
	if script := os.Getenv(AgentScriptEnv); script != "" { //nolint:forbidigo // test-model switch, see #420
		m, err := NewScriptedModel(script)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", AgentScriptEnv, err)
		}
		logger.Warn(context.Background(),
			"agent runs on a SCRIPTED model (test harness) — never use this in production",
			"script", script)
		return m, nil
	}
	adk := NewADKConfig(cfg)
	if adk == nil {
		return nil, nil
	}
	m, err := adk.CreateGeminiModel(context.Background())
	if err != nil {
		return nil, fmt.Errorf("create agent model: %w", err)
	}
	return m, nil
}
