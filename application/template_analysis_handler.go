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

package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"weos/application/agents"
	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"

	adkagent "google.golang.org/adk/agent"
)

// TemplateAnalysisHandler listens for Template.Created events and uses an LLM
// agent to extract structural metadata from the template's HTML file.
type TemplateAnalysisHandler struct {
	agent        adkagent.Agent
	helper       *agents.TemplateExtractionHelper
	templateRepo repositories.TemplateRepository
	logger       entities.Logger
	storagePath  string
}

// NewTemplateAnalysisHandler creates a new handler.
// agent may be nil when no LLM is configured — the handler becomes a no-op.
func NewTemplateAnalysisHandler(
	agent adkagent.Agent,
	templateRepo repositories.TemplateRepository,
	logger entities.Logger,
	storagePath string,
) domain.EventHandler[any] {
	handler := &TemplateAnalysisHandler{
		agent:        agent,
		helper:       agents.NewTemplateExtractionHelper(logger),
		templateRepo: templateRepo,
		logger:       logger,
		storagePath:  storagePath,
	}
	return handler.Handle
}

// Handle processes Template.Created events.
func (h *TemplateAnalysisHandler) Handle(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if h.agent == nil {
		return nil
	}

	templateID := envelope.AggregateID
	if templateID == "" {
		return nil
	}

	tmpl, err := h.templateRepo.FindByID(ctx, templateID)
	if err != nil {
		h.logger.Warn(ctx, "template analysis: failed to load template",
			"templateID", templateID, "error", err)
		return nil
	}

	if tmpl.Description() != "" {
		return nil
	}

	htmlPath := filepath.Join(h.storagePath, tmpl.FilePath())
	htmlBytes, err := os.ReadFile(htmlPath)
	if err != nil {
		h.logger.Warn(ctx, "template analysis: failed to read HTML file",
			"templateID", templateID, "path", htmlPath, "error", err)
		return nil
	}

	if len(htmlBytes) == 0 {
		return nil
	}

	analysis, err := h.helper.AnalyzeTemplate(ctx, h.agent, string(htmlBytes))
	if err != nil {
		h.logger.Warn(ctx, "template analysis: agent failed",
			"templateID", templateID, "error", err)
		return nil
	}

	if err := h.updateTemplate(ctx, tmpl, analysis); err != nil {
		h.logger.Warn(ctx, "template analysis: failed to update template",
			"templateID", templateID, "error", err)
		return nil
	}

	sectionCount := len(analysis.Sections)
	slotCount := 0
	for _, s := range analysis.Sections {
		slotCount += len(s.ContentSlots)
	}
	h.logger.Info(ctx, "template analysis completed",
		"templateID", templateID,
		"pageType", analysis.PageType,
		"sections", sectionCount,
		"contentSlots", slotCount,
	)

	return nil
}

func (h *TemplateAnalysisHandler) updateTemplate(
	ctx context.Context, tmpl *entities.Template, analysis *agents.TemplateAnalysis,
) error {
	desc := analysis.Description
	if desc == "" {
		desc = fmt.Sprintf("%s template", analysis.PageType)
	}

	event := entities.TemplateUpdated{}.With(
		tmpl.Name(), tmpl.Slug(), desc, tmpl.FilePath(), tmpl.Status(),
	)
	if err := tmpl.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return fmt.Errorf("failed to record TemplateUpdated event: %w", err)
	}

	if err := h.templateRepo.Update(ctx, tmpl, ""); err != nil {
		return fmt.Errorf("failed to update template in repository: %w", err)
	}

	return nil
}
