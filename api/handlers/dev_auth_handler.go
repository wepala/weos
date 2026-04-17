package handlers

import (
	"net/http"

	"github.com/wepala/weos/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// DevMe returns an Echo handler for GET /api/auth/me in dev mode (OAuth disabled).
// It reads the X-Dev-Agent header to identify the caller, returning user info
// in the same shape the frontend expects from the real OAuth-based Me endpoint.
func DevMe(
	credRepo authrepos.CredentialRepository,
	agentRepo authrepos.AgentRepository,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		// First check if identity was already injected by SoftAuth middleware
		ctx := c.Request().Context()
		identity := auth.AgentFromCtx(ctx)

		// Fall back to reading the header, then default to first dev user
		if identity == nil {
			email := c.Request().Header.Get("X-Dev-Agent")
			if email == "" {
				// In dev mode, default to the first seeded dev user so the
				// browser works without needing custom headers.
				email = "admin@weos.dev"
			}
			creds, err := credRepo.FindByEmail(ctx, email)
			if err != nil || len(creds) == 0 {
				return respondError(c, http.StatusUnauthorized, "dev user not found")
			}
			agent, err := agentRepo.FindByID(ctx, creds[0].AgentID())
			if err != nil {
				return respondError(c, http.StatusUnauthorized, "agent not found")
			}
			accountID := ""
			accounts, _ := accountRepo.FindByMember(ctx, agent.GetID())
			if len(accounts) > 0 {
				accountID = accounts[0].GetID()
			}
			role := ""
			if accountID != "" {
				role, _ = accountRepo.FindMemberRole(ctx, accountID, agent.GetID())
			}
			return respond(c, http.StatusOK, map[string]any{
				"id":    agent.GetID(),
				"name":  agent.Name(),
				"email": email,
				"role":  role,
			})
		}

		// Identity already in context (from SoftAuth)
		agent, err := agentRepo.FindByID(ctx, identity.AgentID)
		if err != nil {
			return respondError(c, http.StatusUnauthorized, "agent not found")
		}
		role := ""
		if identity.ActiveAccountID != "" {
			role, _ = accountRepo.FindMemberRole(ctx, identity.ActiveAccountID, identity.AgentID)
		}
		creds, _ := credRepo.FindByAgent(ctx, identity.AgentID)
		email := ""
		if len(creds) > 0 {
			email = creds[0].Email()
		}
		return respond(c, http.StatusOK, map[string]any{
			"id":    agent.GetID(),
			"name":  agent.Name(),
			"email": email,
			"role":  role,
		})
	}
}
