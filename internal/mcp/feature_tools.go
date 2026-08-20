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

type FeatureSetInput struct {
	Key     string `json:"key" jsonschema:"the feature key to change"`
	Enabled bool   `json:"enabled" jsonschema:"true to turn the feature on, false to turn it off"`
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
		var err error
		switch scopeOrInstance(input.Scope) {
		case featureScopeAccount:
			err = admin.SetAccount(ctx, input.Key, input.Enabled, entities.FeatureChangeSourceMCP)
		case featureScopeInstance:
			err = admin.SetInstance(ctx, input.Key, input.Enabled, entities.FeatureChangeSourceMCP)
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
		// The change landed; only the read-back failed. Returning an error
		// would report failure for work that succeeded.
		return nil, FeatureChangeOutput{}, nil
	}
	return nil, FeatureChangeOutput{Features: statuses}, nil
}
