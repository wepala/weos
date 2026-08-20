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

package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

// Feature scopes an MCP caller can name.
const (
	featureScopeInstance = "instance"
	featureScopeAccount  = "account"
)

type FeatureListInput struct{}

type FeatureListOutput struct {
	Features []entities.FeatureStatus `json:"features"`
}

// FeatureSetInput carries Enabled as a pointer so an omitted field is
// distinguishable from false. An LLM that leaves the argument out would
// otherwise silently DISABLE the feature it was asked to enable — the worst
// possible reading of an ambiguous call, and one nothing downstream could
// detect.
type FeatureSetInput struct {
	Key     string `json:"key" jsonschema:"the feature key to change"`
	Enabled *bool  `json:"enabled" jsonschema:"required: true to turn the feature on, false to turn it off"`
	Scope   string `json:"scope,omitempty" jsonschema:"instance (default) or account"`
}

type FeatureResetInput struct {
	Key   string `json:"key" jsonschema:"the feature key to reset"`
	Scope string `json:"scope,omitempty" jsonschema:"instance (default) or account"`
}

type FeatureChangeOutput struct {
	Features []entities.FeatureStatus `json:"features"`
}

// registerFeatureTools exposes the operator surface over MCP.
//
// Every tool delegates to FeatureAdminService, which is where the permission
// check lives — the same one the REST handler and the CLI go through. Over
// HTTP an ordinary member is refused here exactly as they are at the API. Over
// the local stdio transport the check passes, because whoever can run
// `weos mcp` on the machine can already run `weos feature disable` against the
// same database; the marker for that is set once by the stdio server and needs
// nothing from this file.
func registerFeatureTools(server *mcp.Server, admin *application.FeatureAdminService) {
	if admin == nil {
		return
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "feature_list",
		Description: "List every feature this instance declares, whether it is on for you, " +
			"and which layer decided that (declared default, instance override, account override, or a grant).",
		Annotations: annReadOnly(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, _ FeatureListInput,
	) (*mcp.CallToolResult, FeatureListOutput, error) {
		statuses, err := admin.Listing(ctx)
		if err != nil {
			return nil, FeatureListOutput{}, err
		}
		return nil, FeatureListOutput{Features: statuses}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "feature_set",
		Description: "Turn a feature on or off. Scope \"instance\" affects everyone and is final — " +
			"no account override or grant reaches past an instance-level off. Scope \"account\" " +
			"affects only the account you are signed in to.",
		Annotations: annDestructive(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input FeatureSetInput,
	) (*mcp.CallToolResult, FeatureChangeOutput, error) {
		if input.Enabled == nil {
			return nil, FeatureChangeOutput{}, fmt.Errorf(
				"enabled is required: say true or false rather than leaving it out")
		}
		var err error
		switch scopeOrInstance(input.Scope) {
		case featureScopeAccount:
			err = admin.SetAccount(ctx, input.Key, *input.Enabled, entities.FeatureChangeSourceMCP)
		case featureScopeInstance:
			err = admin.SetInstance(ctx, input.Key, *input.Enabled, entities.FeatureChangeSourceMCP)
		default:
			err = fmt.Errorf("unknown scope %q: use %q or %q",
				input.Scope, featureScopeInstance, featureScopeAccount)
		}
		if err != nil {
			return nil, FeatureChangeOutput{}, err
		}
		return featureListingResult(ctx, admin)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "feature_reset",
		Description: "Remove a feature override so it returns to the default it was declared with. " +
			"Not the same as turning it off: a reset feature can still be turned on by an account " +
			"override or a grant, where an explicit off cannot.",
		Annotations: annDestructive(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input FeatureResetInput,
	) (*mcp.CallToolResult, FeatureChangeOutput, error) {
		var err error
		switch scopeOrInstance(input.Scope) {
		case featureScopeAccount:
			err = admin.ResetAccount(ctx, input.Key, entities.FeatureChangeSourceMCP)
		case featureScopeInstance:
			err = admin.ResetInstance(ctx, input.Key, entities.FeatureChangeSourceMCP)
		default:
			err = fmt.Errorf("unknown scope %q: use %q or %q",
				input.Scope, featureScopeInstance, featureScopeAccount)
		}
		if err != nil {
			return nil, FeatureChangeOutput{}, err
		}
		return featureListingResult(ctx, admin)
	})

	registerGrantTools(server, admin)
}

// Grant inputs carry no account field, ever. Over HTTP the account comes off
// the session; over stdio there is none, which is what makes the tool refuse
// there rather than guess. An account argument would turn that refusal into a
// multi-tenant hole.
type FeatureGrantInput struct {
	Key        string `json:"key" jsonschema:"the feature key to grant"`
	Email      string `json:"email,omitempty" jsonschema:"the person to grant it to; give exactly one of email or role"`
	Role       string `json:"role,omitempty" jsonschema:"the role to grant it to; give exactly one of email or role"`
	ValidFrom  string `json:"valid_from,omitempty" jsonschema:"RFC3339; when the grant starts. Omit for immediately"`
	ValidUntil string `json:"valid_until,omitempty" jsonschema:"RFC3339; when the grant ends. Omit for indefinitely"`
}

type FeatureRevokeInput struct {
	Key   string `json:"key" jsonschema:"the feature key to take back"`
	Email string `json:"email,omitempty" jsonschema:"give exactly one of email or role"`
	Role  string `json:"role,omitempty" jsonschema:"give exactly one of email or role"`
}

type FeatureGrantsInput struct {
	Key string `json:"key" jsonschema:"the feature key to list grants for"`
}

type FeatureGrantsOutput struct {
	Grants []application.GrantView `json:"grants"`
}

// parseGrantTime reads an optional RFC3339 instant.
func parseGrantTime(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be an RFC3339 time like 2026-08-21T09:00:00Z: %w", field, err)
	}
	return &t, nil
}

// registerGrantTools adds the grant surface to the same opt-in group.
func registerGrantTools(server *mcp.Server, admin *application.FeatureAdminService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "feature_grant",
		Description: "Give a feature to one person or to a role, within the account you are signed in " +
			"to. Optionally bounded by a validity window, which takes effect and expires on its own.",
		Annotations: annDestructive(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input FeatureGrantInput,
	) (*mcp.CallToolResult, FeatureGrantsOutput, error) {
		from, err := parseGrantTime("valid_from", input.ValidFrom)
		if err != nil {
			return nil, FeatureGrantsOutput{}, err
		}
		until, err := parseGrantTime("valid_until", input.ValidUntil)
		if err != nil {
			return nil, FeatureGrantsOutput{}, err
		}
		if err := admin.Grant(ctx, application.GrantRequest{
			Key: input.Key, Email: input.Email, Role: input.Role,
			ValidFrom: from, ValidThrough: until,
			Source: entities.FeatureChangeSourceMCP,
		}); err != nil {
			return nil, FeatureGrantsOutput{}, err
		}
		return grantListingResult(ctx, admin, input.Key)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "feature_revoke",
		Description: "Take a feature back from one person or from a role. Taking back what nobody " +
			"holds succeeds and changes nothing.",
		Annotations: annDestructive(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input FeatureRevokeInput,
	) (*mcp.CallToolResult, FeatureGrantsOutput, error) {
		if err := admin.RevokeGrant(ctx, application.RevokeRequest{
			Key: input.Key, Email: input.Email, Role: input.Role,
			Source: entities.FeatureChangeSourceMCP,
		}); err != nil {
			return nil, FeatureGrantsOutput{}, err
		}
		return grantListingResult(ctx, admin, input.Key)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "feature_grants",
		Description: "List who holds a feature in this account, with each grant's window and who " +
			"made it. Includes grants that have not started yet and ones whose window has closed.",
		Annotations: annReadOnly(),
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input FeatureGrantsInput,
	) (*mcp.CallToolResult, FeatureGrantsOutput, error) {
		views, err := admin.GrantsOn(ctx, input.Key, "")
		if err != nil {
			return nil, FeatureGrantsOutput{}, err
		}
		return nil, FeatureGrantsOutput{Grants: views}, nil
	})
}

func grantListingResult(
	ctx context.Context, admin *application.FeatureAdminService, key string,
) (*mcp.CallToolResult, FeatureGrantsOutput, error) {
	views, err := admin.GrantsOn(ctx, key, "")
	if err != nil {
		// The change landed; only the read-back failed. Said in words rather
		// than answered with an empty list, which a client would read as
		// "nobody holds this".
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: "The change was applied. The grant listing could not be read just now.",
			}},
		}, FeatureGrantsOutput{}, nil
	}
	return nil, FeatureGrantsOutput{Grants: views}, nil
}

// scopeOrInstance defaults an omitted scope to the instance. Instance is the
// scope an operator reaching for this tool almost always means, and an account
// override silently applied when they meant the instance would look like the
// change had no effect for anyone else.
func scopeOrInstance(scope string) string {
	if scope == "" {
		return featureScopeInstance
	}
	return scope
}

// featureListingResult answers a change with the caller's resolved listing, so
// the client can see what the change produced without a second call — a reset
// in particular resolves to whatever layer now decides.
func featureListingResult(
	ctx context.Context, admin *application.FeatureAdminService,
) (*mcp.CallToolResult, FeatureChangeOutput, error) {
	statuses, err := admin.Listing(ctx)
	if err != nil {
		// The change landed; only the read-back failed. Said in words rather
		// than answered with an empty list, which a client — or an LLM — would
		// read as "this instance declares no features".
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: "The change was applied. The feature listing could not be read just now.",
			}},
		}, FeatureChangeOutput{}, nil
	}
	return nil, FeatureChangeOutput{Features: statuses}, nil
}
