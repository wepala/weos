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

	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"
	gormdb "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"

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
	adk := NewADKConfig(cfg)
	if adk == nil {
		return appagents.NewOrchestrator(nil, nil, logger), nil
	}

	m, err := adk.CreateGeminiModel(context.Background())
	if err != nil {
		return nil, fmt.Errorf("create agent model: %w", err)
	}

	sessions, err := database.NewSessionService(gormdb.DialectorForDSN(cfg.DatabaseDSN))
	if err != nil {
		return nil, fmt.Errorf("create agent session service: %w", err)
	}
	if err := database.AutoMigrate(sessions); err != nil {
		return nil, fmt.Errorf("migrate agent session tables: %w", err)
	}

	return appagents.NewOrchestrator(m, sessions, logger), nil
}
