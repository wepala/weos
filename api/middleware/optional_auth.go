package middleware

import (
	"net/http"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
)

// OptionalAuth loads session identity into the request context when a valid
// session exists, but passes through anonymously when it does not. Use this
// for endpoints that behave differently for authenticated vs. anonymous
// callers (e.g., invite acceptance derives email from the session when
// available).
func OptionalAuth(
	sm session.SessionManager,
	as authapp.AuthenticationService,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionData, err := sm.GetHTTPSession(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			sessionInfo, err := as.ValidateSession(r.Context(), sessionData.SessionID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			id := &auth.Identity{
				AgentID:         sessionInfo.AgentID,
				AccountIDs:      []string{sessionInfo.AccountID},
				ActiveAccountID: sessionInfo.AccountID,
			}
			ctx := auth.ContextWithAgent(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
