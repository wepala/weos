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
	"strings"
	"sync"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/widgets"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
	"gorm.io/gorm"
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

// EpisodeRecorder persists one completed turn as episodic memory (the
// application layer records it as a note resource, which is the type #386
// background consolidation distills facts from). Recording failures must
// never fail the turn — the orchestrator logs and moves on.
type EpisodeRecorder func(ctx context.Context, conversationID, userID, message, reply string) error

// ErrEpisodicMemoryUnavailable is returned by an EpisodeRecorder when the
// note resource type does not exist — i.e. the memory preset is not
// installed. The orchestrator treats it as "episodic memory is off" (one
// info line, no per-turn error noise) rather than a failure.
var ErrEpisodicMemoryUnavailable = errors.New(
	"episodic memory unavailable: install the memory preset to record conversation turns")

// coordinatorBrief is the root coordinator's standing instruction. Routing
// is LLM-driven over the skills' descriptions via ADK's transfer mechanism.
// Tool-dependent guidance is appended only when the tool surface is wired
// (see buildRoot) so a tool-less coordinator is never told to use tools it
// does not have.
const coordinatorBrief = `You are the coordinator for this WeOS app.
Route each user request to the sub-agent (skill) whose description best matches, by transferring to it.
When you cannot help, say so plainly instead of inventing data.`

// coordinatorToolsBrief extends the brief when the read-only toolset exists.
const coordinatorToolsBrief = `
If no skill fits, answer directly using your read-only tools — recall consolidated memory first
(memory_recall, memory_search), then search the knowledge graph. Ground every answer in what the
tools returned.`

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
	recorder     EpisodeRecorder

	memoryOffOnce sync.Once
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

// SetEpisodeRecorder wires turn recording (called from the application
// layer's fx wiring).
func (o *Orchestrator) SetEpisodeRecorder(r EpisodeRecorder) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.recorder = r
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
func (o *Orchestrator) Converse(
	ctx context.Context, conversationID, userID, message string,
) (widgets.Response, error) {
	var out widgets.Response
	err := o.ConverseStream(ctx, conversationID, userID, message, func(e entities.AgentEvent) {
		if e.Type == entities.AgentEventWidgets && e.Widgets != nil {
			out = *e.Widgets
		}
	})
	return out, err
}

// EventSink receives the events of one streamed turn.
type EventSink func(entities.AgentEvent)

// ConverseStream runs one turn, emitting text deltas as the model produces
// them, an input_requested event when a mutating tool awaits the user's
// confirmation, and finally the validated widgets plus done.
func (o *Orchestrator) ConverseStream(
	ctx context.Context, conversationID, userID, message string, emit EventSink,
) error {
	content := &genai.Content{Parts: []*genai.Part{{Text: message}}, Role: genai.RoleUser}
	return o.runTurn(ctx, conversationID, userID, content, message, emit)
}

// ResumeConfirmation answers a pending tool confirmation (per the ADK
// tool-confirmation protocol) and streams the rest of the turn. The pending
// call lives in the durable session, so a confirmation survives refreshes
// and restarts. A non-nil payload rides the confirmation response into
// ADK's ToolConfirmation.Payload — the toolset substitutes it for the
// call's arguments on approval (approve-with-edits) — and is durable for
// the same reason the confirmation is.
func (o *Orchestrator) ResumeConfirmation(
	ctx context.Context, conversationID, userID, callID string, confirmed bool,
	payload map[string]any, emit EventSink,
) error {
	response := map[string]any{"confirmed": confirmed}
	if payload != nil {
		response["payload"] = payload
	}
	content := &genai.Content{
		Role: genai.RoleUser,
		Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			Name:     toolconfirmation.FunctionCallName,
			ID:       callID,
			Response: response,
		}}},
	}
	episode := "[approved a pending action]"
	switch {
	case confirmed && payload != nil:
		episode = "[approved a pending action with edits]"
	case !confirmed:
		episode = "[rejected a pending action]"
	}
	return o.runTurn(ctx, conversationID, userID, content, episode, emit)
}

// History returns the conversation so far, oldest first. A conversation
// that does not exist yet has an empty history.
func (o *Orchestrator) History(ctx context.Context, conversationID, userID string) ([]entities.AgentMessage, error) {
	if !o.Configured() {
		return nil, ErrNotConfigured
	}
	resp, err := o.sessions.Get(ctx, &session.GetRequest{
		AppName:   OrchestratorAppName,
		UserID:    userID,
		SessionID: conversationID,
	})
	if err != nil {
		if !isSessionNotFound(err) {
			return nil, fmt.Errorf("load conversation %q: %w", conversationID, err)
		}
		// Missing session simply means the conversation hasn't started.
		o.logger.Debug(ctx, "no session history", "conversationID", conversationID, "error", err.Error())
		return []entities.AgentMessage{}, nil
	}
	var history []entities.AgentMessage
	for event := range resp.Session.Events().All() {
		text := textFromEvent(event)
		if text == "" {
			continue
		}
		if event.Content != nil && event.Content.Role == genai.RoleUser {
			history = append(history, entities.AgentMessage{Role: "user", Text: text})
			continue
		}
		parsed := widgets.Parse(text)
		history = append(history, entities.AgentMessage{Role: "agent", Widgets: &parsed})
	}
	return history, nil
}

// isSessionNotFound reports whether a session.Service.Get error means the
// session does not exist, as opposed to a session-store failure that must
// surface to the caller. ADK's database-backed service wraps
// gorm.ErrRecordNotFound; its in-memory service returns a plain
// "session ... not found" error with no sentinel to match, hence the
// message fallback.
func isSessionNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "not found")
}

// runTurn drives the runner for one inbound content (a user message or a
// confirmation response) and emits the turn's events.
func (o *Orchestrator) runTurn(
	ctx context.Context, conversationID, userID string,
	content *genai.Content, episodeMessage string, emit EventSink,
) error {
	if !o.Configured() {
		return ErrNotConfigured
	}
	root, err := o.buildRoot(ctx)
	if err != nil {
		return err
	}
	if err := o.ensureSession(ctx, userID, conversationID); err != nil {
		return err
	}
	r, err := runner.New(runner.Config{
		AppName:        OrchestratorAppName,
		Agent:          root,
		SessionService: o.sessions,
	})
	if err != nil {
		return fmt.Errorf("create runner: %w", err)
	}

	var out strings.Builder
	for event, err := range r.Run(ctx, userID, conversationID, content, agent.RunConfig{}) {
		if err != nil {
			return fmt.Errorf("run agent: %w", err)
		}
		o.emitConfirmationRequests(ctx, event, emit)
		if text := textFromEvent(event); text != "" {
			out.WriteString(text)
			emit(entities.AgentEvent{Type: entities.AgentEventText, Text: text})
		}
	}

	text := out.String()
	if episodeMessage != "" && text != "" {
		o.recordEpisode(ctx, conversationID, userID, episodeMessage, text)
	}
	// Server-side validation: whatever the model produced renders — contract
	// JSON passes through, anything else degrades to markdown.
	resp := widgets.Parse(text)
	emit(entities.AgentEvent{Type: entities.AgentEventWidgets, Widgets: &resp})
	emit(entities.AgentEvent{Type: entities.AgentEventDone})
	return nil
}

// emitConfirmationRequests surfaces any pending tool confirmations in the
// event as input_requested events the client can answer.
func (o *Orchestrator) emitConfirmationRequests(ctx context.Context, event *session.Event, emit EventSink) {
	if event == nil || event.Content == nil {
		return
	}
	for _, p := range event.Content.Parts {
		if p == nil || p.FunctionCall == nil || p.FunctionCall.Name != toolconfirmation.FunctionCallName {
			continue
		}
		out := entities.AgentEvent{Type: entities.AgentEventInputRequested, CallID: p.FunctionCall.ID}
		if orig, err := toolconfirmation.OriginalCallFrom(p.FunctionCall); err == nil && orig != nil {
			out.Tool = orig.Name
			out.Args = orig.Args
		} else if err != nil {
			o.logger.Error(ctx, "malformed tool confirmation request", "error", err.Error())
		}
		if tc, ok := p.FunctionCall.Args["toolConfirmation"].(map[string]any); ok {
			if hint, ok := tc["hint"].(string); ok {
				out.Hint = hint
			}
		}
		emit(out)
	}
}

// recordEpisode persists the completed turn as episodic memory; failures are
// logged, never surfaced — remembering must not break answering.
func (o *Orchestrator) recordEpisode(ctx context.Context, conversationID, userID, message, reply string) {
	o.mu.RLock()
	recorder := o.recorder
	o.mu.RUnlock()
	if recorder == nil {
		return
	}
	if err := recorder(ctx, conversationID, userID, message, reply); err != nil {
		if errors.Is(err, ErrEpisodicMemoryUnavailable) {
			// Not an error: the instance simply hasn't installed the memory
			// preset. Say so once instead of once per turn.
			o.memoryOffOnce.Do(func() {
				o.logger.Info(ctx, "episodic memory is off: install the memory preset "+
					"to remember conversation turns")
			})
			return
		}
		o.logger.Error(ctx, "failed to record conversation episode",
			"conversationID", conversationID, "error", err.Error())
	}
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

	instruction := coordinatorBrief
	hasDirectTools := ts != nil && len(defaultTools) > 0
	if hasDirectTools {
		instruction += coordinatorToolsBrief
	}
	instruction += widgetOutputInstruction

	cfg := llmagent.Config{
		Name:        "weos_coordinator",
		Description: "Routes requests to the right skill and answers simple questions directly",
		Model:       o.model,
		Instruction: instruction,
		Mode:        llmagent.ModeChat,
		SubAgents:   subAgents,
	}
	if hasDirectTools {
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
	if !isSessionNotFound(getErr) {
		// A store outage or auth failure is not "session missing" — surface
		// it rather than masking it behind a doomed create attempt.
		return fmt.Errorf("ensure session %q: %w", conversationID, getErr)
	}
	if _, createErr := o.sessions.Create(ctx, &session.CreateRequest{
		AppName:   OrchestratorAppName,
		UserID:    userID,
		SessionID: conversationID,
	}); createErr != nil {
		return fmt.Errorf("ensure session %q: create: %w", conversationID, createErr)
	}
	return nil
}
