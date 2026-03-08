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
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	// DefaultAppName is the default application name for the ADK runner.
	DefaultAppName = "WeOS"
)

// responseSchemaKeyType is the context key for an optional per-run response schema.
type responseSchemaKeyType struct{}

var responseSchemaKey = &responseSchemaKeyType{}

// ApplyResponseSchemaFromContext is a BeforeModelCallback that applies the
// response schema from context (when set by RunAgent) to the LLM request so
// the model returns structured output.
func ApplyResponseSchemaFromContext(
	ctx agent.CallbackContext, req *model.LLMRequest,
) (*model.LLMResponse, error) {
	schema := ctx.Value(responseSchemaKey)
	if schema == nil {
		return nil, nil
	}
	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	req.Config.ResponseSchema = schema.(*genai.Schema)
	req.Config.ResponseMIMEType = "application/json"
	return nil, nil
}

// RunAgent runs the given agent with the user input and returns the concatenated
// text from all non-partial LLM response events.
func RunAgent(
	ctx context.Context, a agent.Agent, appName, userID, sessionID, userInput string,
	responseSchema *genai.Schema, fileParts ...*genai.Part,
) (string, error) {
	if appName == "" {
		appName = DefaultAppName
	}
	if responseSchema != nil {
		ctx = context.WithValue(ctx, responseSchemaKey, responseSchema)
	}
	sessionService := session.InMemoryService()

	_, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          a,
		SessionService: sessionService,
	})
	if err != nil {
		return "", fmt.Errorf("create runner: %w", err)
	}

	parts := make([]*genai.Part, 0, len(fileParts)+1)
	parts = append(parts, fileParts...)
	parts = append(parts, &genai.Part{Text: userInput})
	msg := &genai.Content{Parts: parts, Role: genai.RoleUser}

	var out strings.Builder
	for event, err := range r.Run(ctx, userID, sessionID, msg, agent.RunConfig{}) {
		if err != nil {
			return "", fmt.Errorf("run agent: %w", err)
		}
		text := textFromEvent(event)
		if text != "" {
			out.WriteString(text)
		}
	}
	return out.String(), nil
}

// textFromEvent extracts plain text from a session event's LLM response content.
func textFromEvent(event *session.Event) string {
	if event == nil || event.Content == nil || len(event.Content.Parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range event.Content.Parts {
		if p != nil && p.Text != "" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
