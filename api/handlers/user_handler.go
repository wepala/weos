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

	apimw "weos/api/middleware"
	"weos/domain/entities"

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
	agents, err := h.agentRepo.FindAll(ctx, "", 100)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	accountID := h.defaultAccountID(ctx)

	users := make([]UserResponse, 0, len(agents.Data))
	for _, agent := range agents.Data {
		users = append(users, h.buildUserResponse(ctx, agent, accountID))
	}

	return c.JSON(http.StatusOK, map[string]any{"data": users})
}

// Get returns a single user by ID.
func (h *UserHandler) Get(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	agent, err := h.agentRepo.FindByID(ctx, id)
	if err != nil || agent == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "user not found"})
	}

	accountID := h.defaultAccountID(ctx)
	return c.JSON(http.StatusOK, h.buildUserResponse(ctx, agent, accountID))
}

type UpdateUserRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// Update modifies a user's name and/or role. Admin-only.
func (h *UserHandler) Update(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if !apimw.IsAdmin(ctx, h.accountRepo) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "admin role required"})
	}

	var req UpdateUserRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	agent, err := h.agentRepo.FindByID(ctx, id)
	if err != nil || agent == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "user not found"})
	}

	if req.Name != "" && req.Name != agent.Name() {
		if err := agent.UpdateName(req.Name); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if err := h.agentRepo.Save(ctx, agent); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}

	accountID := h.defaultAccountID(ctx)
	if req.Role != "" && accountID != "" {
		if err := h.accountRepo.SaveMember(ctx, accountID, id, req.Role); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}

	return c.JSON(http.StatusOK, h.buildUserResponse(ctx, agent, accountID))
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
	creds, _ := h.credentialRepo.FindByAgent(ctx, agent.GetID())
	if len(creds) > 0 {
		email = creds[0].Email()
	}
	if email == "" {
		email = agent.Name()
	}

	role := ""
	if accountID != "" {
		role, _ = h.accountRepo.FindMemberRole(ctx, accountID, agent.GetID())
	}

	return UserResponse{
		ID:     agent.GetID(),
		Name:   agent.Name(),
		Email:  email,
		Status: agent.Status(),
		Role:   role,
	}
}
