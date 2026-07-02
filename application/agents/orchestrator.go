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
	"errors"
	"fmt"
	"sync"

	"github.com/wepala/weos/v3/domain/entities"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

// OrchestratorAppName scopes ADK sessions for the in-app agent.
const OrchestratorAppName = "weos-agent"

// ErrNotConfigured is returned when no LLM key is configured; the in-app
// agent surface degrades to a clear signal instead of failing obscurely
// (mirrors how #386 consolidation no-ops without a key).
var ErrNotConfigured = errors.New("agent not configured: set the Gemini API key to enable the in-app agent")

// SkillSource supplies the current skill definitions (the application layer's
// SkillRegistry, injected as a function to keep this package free of
// application imports).
type SkillSource func(ctx context.Context) ([]entities.SkillDefinition, error)

// ToolsetFactory opens a tool surface for one conversation context. The
// context carries the caller's identity — service-layer authorization reads
// it — so the orchestrator asks for a fresh toolset per turn rather than
// sharing one across users.
type ToolsetFactory func(ctx context.Context) (tool.Toolset, error)

// orchestratorInstruction is the root coordinator's standing brief. Routing
// is LLM-driven over the skills' descriptions via ADK's transfer mechanism.
const orchestratorInstruction = `You are the coordinator for this WeOS app.
Route each user request to the sub-agent (skill) whose description best matches, by transferring to it.
If no skill fits, answer directly using your read-only tools (memory recall, knowledge-graph search).
Ground every answer in what the tools returned; when you cannot help, say so plainly instead of inventing data.`

// Orchestrator is the in-app agent: a Chat-mode coordinator that routes each
// request to the right skill sub-agent, built fresh from the current skill
// definitions so skill changes apply without a restart. It satisfies the
// application layer's ConversationalAgent interface; ADK types never cross
// this package boundary.
type Orchestrator struct {
	model    model.LLM
	sessions session.Service
	logger   entities.Logger

	mu           sync.RWMutex
	skills       SkillSource
	toolsets     ToolsetFactory
	defaultTools []string
}

// NewOrchestrator assembles the orchestrator. model and sessions may be nil
// when the instance has no LLM configured — Converse then returns
// ErrNotConfigured and the rest of the app is unaffected.
func NewOrchestrator(m model.LLM, sessions session.Service, logger entities.Logger) *Orchestrator {
	return &Orchestrator{model: m, sessions: sessions, logger: logger}
}

// SetSkillSource wires the skill registry (called from the application
// layer's fx wiring).
func (o *Orchestrator) SetSkillSource(s SkillSource) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.skills = s
}

// SetToolsetFactory wires the tool surface once it exists (the serve command
// builds it after the MCP server). defaultTools are the read-only tool names
// the coordinator may use directly when no skill matches.
func (o *Orchestrator) SetToolsetFactory(f ToolsetFactory, defaultTools []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.toolsets = f
	o.defaultTools = defaultTools
}

// Configured reports whether the in-app agent can serve conversations.
func (o *Orchestrator) Configured() bool {
	return o.model != nil && o.sessions != nil
}

// Converse runs one turn of a conversation. conversationID maps to a durable
// ADK session, so multi-turn exchanges — and, with a DB-backed session
// service, restarts — keep their history. ctx must carry the caller's
// identity (it is the context tools execute under) and must be
// request-scoped: the per-turn toolset session lives exactly as long as
// ctx, so a non-cancellable context would leak an in-memory MCP session
// per turn.
func (o *Orchestrator) Converse(ctx context.Context, conversationID, userID, message string) (string, error) {
	if !o.Configured() {
		return "", ErrNotConfigured
	}
	root, err := o.buildRoot(ctx)
	if err != nil {
		return "", err
	}
	if err := o.ensureSession(ctx, userID, conversationID); err != nil {
		return "", err
	}
	r, err := runner.New(runner.Config{
		AppName:        OrchestratorAppName,
		Agent:          root,
		SessionService: o.sessions,
	})
	if err != nil {
		return "", fmt.Errorf("create runner: %w", err)
	}
	return RunUserTurn(ctx, r, userID, conversationID, message)
}

// buildRoot assembles the coordinator with one sub-agent per loaded skill.
// It is rebuilt per turn from the live registry, which is what makes skill
// changes take effect immediately; construction is cheap (no network).
func (o *Orchestrator) buildRoot(ctx context.Context) (agent.Agent, error) {
	o.mu.RLock()
	skills := o.skills
	toolsets := o.toolsets
	defaultTools := o.defaultTools
	o.mu.RUnlock()

	var defs []entities.SkillDefinition
	if skills != nil {
		var err error
		if defs, err = skills(ctx); err != nil {
			return nil, fmt.Errorf("load skills: %w", err)
		}
	}

	var ts tool.Toolset
	if toolsets != nil {
		var err error
		if ts, err = toolsets(ctx); err != nil {
			return nil, fmt.Errorf("open toolset: %w", err)
		}
	}

	subAgents := make([]agent.Agent, 0, len(defs))
	for _, def := range defs {
		if def.Model != "" {
			o.logger.Info(ctx, "agent-skill model override not yet supported; using the default model",
				"skill", def.Name, "model", def.Model)
		}
		sub, err := BuildSkillAgent(def, o.model, ts)
		if err != nil {
			// One bad skill must not take the agent down; the registry
			// already validated, so this is defensive.
			o.logger.Error(ctx, "skipping skill: failed to build agent", "skill", def.Name, "error", err.Error())
			continue
		}
		subAgents = append(subAgents, sub)
	}

	cfg := llmagent.Config{
		Name:        "weos_coordinator",
		Description: "Routes requests to the right skill and answers simple questions directly",
		Model:       o.model,
		Instruction: orchestratorInstruction,
		Mode:        llmagent.ModeChat,
		SubAgents:   subAgents,
	}
	if ts != nil && len(defaultTools) > 0 {
		cfg.Toolsets = []tool.Toolset{tool.FilterToolset(ts, tool.AllowedToolsPredicate(defaultTools))}
	}
	root, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("build coordinator: %w", err)
	}
	return root, nil
}

// ensureSession makes the (userID, conversationID) session exist. Get-then-
// create keeps replays and retries idempotent.
func (o *Orchestrator) ensureSession(ctx context.Context, userID, conversationID string) error {
	_, getErr := o.sessions.Get(ctx, &session.GetRequest{
		AppName:   OrchestratorAppName,
		UserID:    userID,
		SessionID: conversationID,
	})
	if getErr == nil {
		return nil
	}
	if _, createErr := o.sessions.Create(ctx, &session.CreateRequest{
		AppName:   OrchestratorAppName,
		UserID:    userID,
		SessionID: conversationID,
	}); createErr != nil {
		return fmt.Errorf("ensure session %q: get: %v; create: %w", conversationID, getErr, createErr)
	}
	return nil
}

// RunUserTurn feeds one user message through a runner and returns the
// concatenated text of the response events.
func RunUserTurn(ctx context.Context, r *runner.Runner, userID, sessionID, message string) (string, error) {
	msg := &genai.Content{Parts: []*genai.Part{{Text: message}}, Role: genai.RoleUser}
	var out []byte
	for event, err := range r.Run(ctx, userID, sessionID, msg, agent.RunConfig{}) {
		if err != nil {
			return "", fmt.Errorf("run agent: %w", err)
		}
		out = append(out, textFromEvent(event)...)
	}
	return string(out), nil
}
