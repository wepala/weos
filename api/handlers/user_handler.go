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

package handlers

import (
	"context"
	"net/http"

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// UserHandler serves the users routes. Every route acts in the account the
// caller acts in, and only for the members of that account (wm-govvg): every
// person owns the account their first sign-in created, so an owner or admin
// role in one account says nothing about anybody outside it.
type UserHandler struct {
	agentRepo      authrepos.AgentRepository
	credentialRepo authrepos.CredentialRepository
	accountRepo    authrepos.AccountRepository
	members        repositories.AccountMemberQuery
	features       repositories.FeatureCacheInvalidator
	logger         entities.Logger
}

type UserHandlerConfig struct {
	AgentRepo      authrepos.AgentRepository
	CredentialRepo authrepos.CredentialRepository
	AccountRepo    authrepos.AccountRepository
	// Members lists the people of the caller's account. Required.
	Members repositories.AccountMemberQuery
	// Features drops a member's resolved feature set when their role changes.
	// Optional: a handler constructed without one simply does not invalidate,
	// which keeps existing test constructions working.
	Features repositories.FeatureCacheInvalidator
	Logger   entities.Logger
}

func NewUserHandler(cfg UserHandlerConfig) *UserHandler {
	return &UserHandler{
		agentRepo:      cfg.AgentRepo,
		credentialRepo: cfg.CredentialRepo,
		accountRepo:    cfg.AccountRepo,
		members:        cfg.Members,
		features:       cfg.Features,
		logger:         cfg.Logger,
	}
}

type UserResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Status string `json:"status"`
	Role   string `json:"role,omitempty"`
}

// userScope is an owner or admin of an account, and that account.
type userScope struct {
	callerID  string
	accountID string
}

// scope resolves the account the caller acts in and requires the owner or
// admin role there. When the request stops, scope returns nil and the value of
// the response it has already written.
func (h *UserHandler) scope(c echo.Context) (*userScope, error) {
	ctx := c.Request().Context()
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil, respondError(c, http.StatusForbidden, "admin role required")
	}
	accountID, err := apimw.CallerAccountID(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to resolve the caller's account", "error", err)
		return nil, respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	isAdmin := false
	if accountID != "" {
		isAdmin, err = apimw.IsOwnerOrAdmin(ctx, h.accountRepo, accountID, identity.AgentID)
		if err != nil {
			h.logger.Error(ctx, "failed to check admin status", "error", err)
			return nil, respondError(c, http.StatusInternalServerError, "authorization check failed")
		}
	}
	if !isAdmin {
		return nil, respondError(c, http.StatusForbidden, "admin role required")
	}
	return &userScope{callerID: identity.AgentID, accountID: accountID}, nil
}

// member returns the person id and the role they hold in the scope's account.
// A person who is not a member of that account gets the 404 a person who does
// not exist gets, so the answer says nothing about who exists elsewhere on the
// instance, and the refusal is logged. When the request stops, member returns
// a nil agent and the value of the response it has already written.
func (h *UserHandler) member(c echo.Context, s *userScope, id string) (*authentities.Agent, string, error) {
	ctx := c.Request().Context()
	role, err := h.accountRepo.FindMemberRole(ctx, s.accountID, id)
	if err != nil {
		h.logger.Error(ctx, "failed to check the person's membership", "account_id", s.accountID, "error", err)
		return nil, "", respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if role == "" {
		h.logger.Warn(ctx, "users request refused: the person is not a member of the caller's account",
			"caller_agent_id", s.callerID,
			"account_id", s.accountID,
			"target_agent_id", id,
		)
		return nil, "", respondError(c, http.StatusNotFound, "user not found")
	}
	agent, err := h.agentRepo.FindByID(ctx, id)
	if err != nil {
		h.logger.Error(ctx, "failed to find a member's record", "account_id", s.accountID, "agent_id", id, "error", err)
		return nil, "", respondError(c, http.StatusInternalServerError, "failed to load user")
	}
	if agent == nil {
		h.logger.Warn(ctx, "a member of the account has no person record", "account_id", s.accountID, "agent_id", id)
		return nil, "", respondError(c, http.StatusNotFound, "user not found")
	}
	return agent, role, nil
}

// List returns the members of the caller's account, each with the role they
// hold in it. Owner or admin of that account only.
func (h *UserHandler) List(c echo.Context) error {
	s, err := h.scope(c)
	if s == nil {
		return err
	}
	ctx := c.Request().Context()

	members, err := h.members.ListMembers(ctx, s.accountID)
	if err != nil {
		h.logger.Error(ctx, "failed to list users", "account_id", s.accountID, "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to list users")
	}

	users := make([]UserResponse, 0, len(members))
	for _, m := range members {
		agent, err := h.agentRepo.FindByID(ctx, m.AgentID)
		if err != nil {
			h.logger.Error(ctx, "failed to load a member of the account", "account_id", s.accountID, "agent_id", m.AgentID, "error", err)
			return respondError(c, http.StatusInternalServerError, "failed to list users")
		}
		if agent == nil {
			// A membership whose person record is gone has nobody to show or
			// manage; listing it would render an empty row.
			h.logger.Warn(ctx, "a member of the account has no person record", "account_id", s.accountID, "agent_id", m.AgentID)
			continue
		}
		users = append(users, h.buildUserResponse(ctx, agent, m.RoleID))
	}

	return respond(c, http.StatusOK, users)
}

// Get returns one member of the caller's account. Owner or admin of that
// account only.
func (h *UserHandler) Get(c echo.Context) error {
	s, err := h.scope(c)
	if s == nil {
		return err
	}
	agent, role, err := h.member(c, s, c.Param("id"))
	if agent == nil {
		return err
	}
	return respond(c, http.StatusOK, h.buildUserResponse(c.Request().Context(), agent, role))
}

type UpdateUserRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// Update changes the name and/or the role of a member of the caller's account.
// The role is saved in that account and no other. Owner or admin of that
// account only.
func (h *UserHandler) Update(c echo.Context) error {
	s, err := h.scope(c)
	if s == nil {
		return err
	}
	id := c.Param("id")
	ctx := c.Request().Context()

	var req UpdateUserRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}

	agent, role, err := h.member(c, s, id)
	if agent == nil {
		return err
	}

	if req.Name != "" && req.Name != agent.Name() {
		if err := agent.UpdateName(req.Name); err != nil {
			h.logger.Error(ctx, "failed to update user name", "error", err, "user_id", id)
			return respondError(c, http.StatusInternalServerError, "failed to update user")
		}
		if err := h.agentRepo.Save(ctx, agent); err != nil {
			h.logger.Error(ctx, "failed to save user", "error", err, "user_id", id)
			return respondError(c, http.StatusInternalServerError, "failed to update user")
		}
	}

	if req.Role != "" {
		if err := h.accountRepo.SaveMember(ctx, s.accountID, id, req.Role); err != nil {
			h.logger.Error(ctx, "failed to save user role", "error", err, "user_id", id)
			return respondError(c, http.StatusInternalServerError, "failed to update user role")
		}
		// A role change changes what features this person resolves, because a
		// feature can be granted to a role. SaveMember writes the projection
		// directly and emits no event, so nothing else would ever notice —
		// this is the one invalidation call site outside FeatureService, and
		// without it the person keeps their old access until the cache ages
		// out. Dropping their resolved set does not sign them out; their next
		// evaluation costs one database read.
		if h.features != nil {
			h.features.InvalidateAgents(ctx, s.accountID, id)
		}
		role = req.Role
	}

	return respond(c, http.StatusOK, h.buildUserResponse(ctx, agent, role))
}

func (h *UserHandler) buildUserResponse(
	ctx context.Context, agent *authentities.Agent, role string,
) UserResponse {
	email := ""
	creds, credErr := h.credentialRepo.FindByAgent(ctx, agent.GetID())
	if credErr != nil {
		h.logger.Warn(ctx, "failed to load credentials for user", "agent_id", agent.GetID(), "error", credErr)
	}
	if len(creds) > 0 {
		email = creds[0].Email()
	}
	if email == "" {
		email = agent.Name()
	}

	return UserResponse{
		ID:     agent.GetID(),
		Name:   agent.Name(),
		Email:  email,
		Status: agent.Status(),
		Role:   role,
	}
}
