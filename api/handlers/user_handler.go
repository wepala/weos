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

	apimw "github.com/wepala/weos/api/middleware"
	"github.com/wepala/weos/domain/entities"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

type UserHandler struct {
	agentRepo      authrepos.AgentRepository
	credentialRepo authrepos.CredentialRepository
	accountRepo    authrepos.AccountRepository
	logger         entities.Logger
}

type UserHandlerConfig struct {
	AgentRepo      authrepos.AgentRepository
	CredentialRepo authrepos.CredentialRepository
	AccountRepo    authrepos.AccountRepository
	Logger         entities.Logger
}

func NewUserHandler(cfg UserHandlerConfig) *UserHandler {
	return &UserHandler{
		agentRepo:      cfg.AgentRepo,
		credentialRepo: cfg.CredentialRepo,
		accountRepo:    cfg.AccountRepo,
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

func (h *UserHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	agents, err := h.agentRepo.FindAll(ctx, "", 100)
	if err != nil {
		h.logger.Error(ctx, "failed to list users", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to list users")
	}

	accountID := h.defaultAccountID(ctx)

	users := make([]UserResponse, 0, len(agents.Data))
	for _, agent := range agents.Data {
		users = append(users, h.buildUserResponse(ctx, agent, accountID))
	}

	return respond(c, http.StatusOK, users)
}

// Get returns a single user by ID. Admin-only.
func (h *UserHandler) Get(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	agent, err := h.agentRepo.FindByID(ctx, id)
	if err != nil || agent == nil {
		return respondError(c, http.StatusNotFound, "user not found")
	}

	accountID := h.defaultAccountID(ctx)
	return respond(c, http.StatusOK, h.buildUserResponse(ctx, agent, accountID))
}

type UpdateUserRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// Update modifies a user's name and/or role. Admin-only.
func (h *UserHandler) Update(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	var req UpdateUserRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}

	agent, err := h.agentRepo.FindByID(ctx, id)
	if err != nil || agent == nil {
		return respondError(c, http.StatusNotFound, "user not found")
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

	accountID := h.defaultAccountID(ctx)
	if req.Role != "" && accountID != "" {
		if err := h.accountRepo.SaveMember(ctx, accountID, id, req.Role); err != nil {
			h.logger.Error(ctx, "failed to save user role", "error", err, "user_id", id)
			return respondError(c, http.StatusInternalServerError, "failed to update user role")
		}
	}

	return respond(c, http.StatusOK, h.buildUserResponse(ctx, agent, accountID))
}

func (h *UserHandler) defaultAccountID(ctx context.Context) string {
	accounts, err := h.accountRepo.FindAll(ctx, "", 1)
	if err != nil || len(accounts.Data) == 0 {
		return ""
	}
	return accounts.Data[0].GetID()
}

func (h *UserHandler) buildUserResponse(
	ctx context.Context, agent *authentities.Agent, accountID string,
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

	role := ""
	if accountID != "" {
		var roleErr error
		role, roleErr = h.accountRepo.FindMemberRole(ctx, accountID, agent.GetID())
		if roleErr != nil {
			h.logger.Warn(ctx, "failed to load role for user", "agent_id", agent.GetID(), "error", roleErr)
		}
	}

	return UserResponse{
		ID:     agent.GetID(),
		Name:   agent.Name(),
		Email:  email,
		Status: agent.Status(),
		Role:   role,
	}
}
