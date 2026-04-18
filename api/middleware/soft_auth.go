package middleware

import (
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

const devAgentHeader = "X-Dev-Agent"

// SoftAuth returns Echo middleware for dev mode (OAuth disabled).
// If the request carries an X-Dev-Agent header with a seeded user's email,
// the middleware resolves the user and injects auth.Identity into the context.
// If the header is absent, the request passes through anonymously.
func SoftAuth(
	credRepo authrepos.CredentialRepository,
	agentRepo authrepos.AgentRepository,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			email := c.Request().Header.Get(devAgentHeader)
			if email == "" {
				// In dev mode, default to the first seeded dev user so the
				// browser works without needing custom headers.
				email = "admin@weos.dev"
			}

			ctx := c.Request().Context()

			creds, err := credRepo.FindByEmail(ctx, email)
			if err != nil || len(creds) == 0 {
				logger.Warn(ctx, "soft-auth: no credential found for dev agent",
					"email", email)
				return next(c)
			}
			cred := creds[0]

			agent, err := agentRepo.FindByID(ctx, cred.AgentID())
			if err != nil {
				logger.Warn(ctx, "soft-auth: agent not found",
					"agentID", cred.AgentID(), "error", err)
				return next(c)
			}

			activeAccountID := ""
			accounts, err := accountRepo.FindByMember(ctx, agent.GetID())
			if err == nil && len(accounts) > 0 {
				activeAccountID = accounts[0].GetID()
			}

			identity := &auth.Identity{
				AgentID:         agent.GetID(),
				AccountIDs:      []string{activeAccountID},
				ActiveAccountID: activeAccountID,
			}
			authCtx := auth.ContextWithAgent(ctx, identity)
			c.SetRequest(c.Request().WithContext(authCtx))

			return next(c)
		}
	}
}
