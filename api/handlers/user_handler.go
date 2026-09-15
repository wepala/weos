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
	"strconv"

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
	members        repositories.AccountMemberDirectory
	features       repositories.FeatureCacheInvalidator
	logger         entities.Logger
}

type UserHandlerConfig struct {
	AgentRepo      authrepos.AgentRepository
	CredentialRepo authrepos.CredentialRepository
	AccountRepo    authrepos.AccountRepository
	// Members lists the people of the caller's account. Required:
	// NewUserHandler panics without one.
	Members repositories.AccountMemberDirectory
	// Features drops a member's resolved feature set when their role changes.
	// Optional: a handler constructed without one simply does not invalidate,
	// which keeps existing test constructions working.
	Features repositories.FeatureCacheInvalidator
	Logger   entities.Logger
}

// NewUserHandler builds the users handler. It panics when cfg.Members is nil
// (wm-ii1hz): the list route cannot answer without it, and a handler built
// that way would fail on its first request instead of when it is wired, where
// the missing dependency is a mistake in code, not in a request.
func NewUserHandler(cfg UserHandlerConfig) *UserHandler {
	if cfg.Members == nil {
		panic("handlers.NewUserHandler: UserHandlerConfig.Members is required " +
			"(a repositories.AccountMemberDirectory, such as gorm.ProvideAccountMemberDirectory)")
	}
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

// scope takes the account the caller's session or token names and requires the
// owner or admin role there. When the request stops, scope returns nil and the
// value of the response it has already written.
func (h *UserHandler) scope(c echo.Context) (*userScope, error) {
	ctx := c.Request().Context()
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil, respondError(c, http.StatusForbidden, "admin role required")
	}
	// The routes act only in the account the caller names (wm-8uq74). A caller
	// who names none is refused with the code the auth middleware gives an
	// unscoped session, not matched to one of their accounts: for a person in
	// several accounts, that match may not be the account they mean, and a
	// role change would land in the wrong one.
	accountID := identity.ActiveAccountID
	if accountID == "" {
		h.logger.Warn(ctx, "users request refused: the caller names no active account",
			"caller_agent_id", identity.AgentID)
		return nil, respondErrorCode(c, http.StatusUnauthorized, "not authenticated", apimw.CodeUnscopedSession)
	}
	isAdmin, err := apimw.IsOwnerOrAdmin(ctx, h.accountRepo, accountID, identity.AgentID)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return nil, respondError(c, http.StatusInternalServerError, "authorization check failed")
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

// List returns one page of the members of the caller's account, each with the
// role they hold in it. Owner or admin of that account only.
//
// The page holds repositories.DefaultMemberPageSize people unless the client
// names a limit, and never more than repositories.MaxMemberPageSize. A client
// asks for the next page by sending the response's cursor back as ?cursor=,
// until has_more is false (wm-g7284).
func (h *UserHandler) List(c echo.Context) error {
	s, err := h.scope(c)
	if s == nil {
		return err
	}
	ctx := c.Request().Context()

	// A missing or unreadable limit is 0, which the directory reads as its
	// default page size.
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	page, err := h.members.ListMembers(ctx, s.accountID, c.QueryParam("cursor"), limit)
	if err != nil {
		h.logger.Error(ctx, "failed to list users", "account_id", s.accountID, "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to list users")
	}

	users := make([]UserResponse, 0, len(page.Members))
	for _, m := range page.Members {
		if !m.HasRecord {
			// A membership whose person record is gone has nobody to show or
			// manage; listing it would render an empty row.
			h.logger.Warn(ctx, "a member of the account has no person record", "account_id", s.accountID, "agent_id", m.AgentID)
			continue
		}
		email := m.Email
		if email == "" {
			email = m.Name
		}
		users = append(users, UserResponse{ID: m.AgentID, Name: m.Name, Email: email, Status: m.Status, Role: m.RoleID})
	}

	return respondPaginated(c, http.StatusOK, users, page.Cursor, page.HasMore)
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

// CodeLastOwnerRequired is the code on the refusal of a role change that would
// leave an account with no owner.
const CodeLastOwnerRequired = "last_owner_required"

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

	// An account keeps at least one owner (wm-qhda1): with none, nobody can
	// manage its people or invites again. Checked before anything is written,
	// so a refused request changes nothing, the name included. Two owners
	// demoting each other at the same moment can still both pass; the count
	// and the write are not one transaction.
	if req.Role != "" && role == authentities.RoleOwner && req.Role != authentities.RoleOwner {
		owners, err := h.members.CountMembersWithRole(ctx, s.accountID, authentities.RoleOwner)
		if err != nil {
			h.logger.Error(ctx, "failed to count the owners of the account", "account_id", s.accountID, "error", err)
			return respondError(c, http.StatusInternalServerError, "failed to update user role")
		}
		if owners <= 1 {
			h.logger.Warn(ctx, "users request refused: the change would leave the account with no owner",
				"caller_agent_id", s.callerID,
				"account_id", s.accountID,
				"target_agent_id", id,
			)
			return respondErrorCode(c, http.StatusBadRequest, "an account must keep at least one owner", CodeLastOwnerRequired)
		}
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
