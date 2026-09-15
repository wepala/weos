package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/domain/repositories"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
)

// newAccountCtxKey is the context key under which the OAuth callback installs a
// *bool that FindOrCreateAgent flips to true for a first-time signup.
type newAccountCtxKey struct{}

// WithNewAccountFlag returns a context carrying flag, which a wrapped
// AuthenticationService will set to true when FindOrCreateAgent creates a brand
// new account (rather than matching an existing one). The OAuth callback uses
// this to append ?new_account=1 to its post-login redirect so the frontend can
// route first-time users into onboarding.
func WithNewAccountFlag(ctx context.Context, flag *bool) context.Context {
	return context.WithValue(ctx, newAccountCtxKey{}, flag)
}

// NewAccountFlagFromContext returns the flag pointer installed by
// WithNewAccountFlag, or nil if the caller didn't ask for the signal. The OAuth
// callback wrapper reads it after the login completes to decide whether to add
// the new-account marker to its redirect.
func NewAccountFlagFromContext(ctx context.Context) *bool {
	flag, _ := ctx.Value(newAccountCtxKey{}).(*bool)
	return flag
}

// newAccountSignalService decorates an AuthenticationService so the OAuth
// callback can distinguish a freshly created account from a returning login,
// and so every FindOrCreateAgent and every RegisterPassword holds the sign-in
// lock. It embeds the wrapped service and overrides only those two methods;
// every other method passes straight through.
type newAccountSignalService struct {
	authapp.AuthenticationService
	credentials authrepos.CredentialRepository
	// lock is the SignInLock owner binding holds. Both OAuth callbacks — core's
	// /oauth/callback and pericarp's /api/auth/callback — write google and apple
	// credentials through FindOrCreateAgent, and password registration creates a
	// person through RegisterPassword, so each holds the same identity and email
	// keys as an asserted sign-in. Otherwise an assertion that read "nobody holds
	// this email" while one of them wrote the first credential for it would
	// create a second person. Optional; without it neither method holds anything.
	lock repositories.SignInLock
}

// FindOrCreateAgent holds the sign-in lock for the identity and its email,
// unless its caller already holds those keys (an asserted sign-in does), and
// then pre-checks whether a credential already exists for the incoming
// identity. The check is the same lookup FindOrCreateAgent does internally as
// its first step, so a fresh row means this call is about to create the
// account. Only the credential's existence determines the signal; the lookup
// error is discarded because the wrapped call immediately repeats the same
// query and will surface any real DB error itself (failing the login before
// the flag is ever consumed on the redirect path).
//
// The pre-check only runs when a caller installed a flag via WithNewAccountFlag,
// keeping password and MCP login paths free of the extra query.
func (s *newAccountSignalService) FindOrCreateAgent(ctx context.Context, userInfo authapp.UserInfo) (*entities.Agent, *entities.Credential, *entities.Account, error) {
	if s.lock != nil {
		keys := signInLockKeys(userInfo.Provider, userInfo.ProviderUserID, repositories.FoldCredentialEmail(userInfo.Email))
		if !signInKeysHeld(ctx, keys) {
			release, err := s.lock.Hold(ctx, keys...)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("hold the sign-in lock: %w", err)
			}
			defer release()
			ctx = withSignInKeysHeld(ctx, keys)
		}
	}
	if flag := NewAccountFlagFromContext(ctx); flag != nil {
		existing, _ := s.credentials.FindByProvider(ctx, userInfo.Provider, userInfo.ProviderUserID)
		*flag = existing == nil
	}
	return s.AuthenticationService.FindOrCreateAgent(ctx, userInfo)
}

// RegisterPassword holds the sign-in lock for the password identity and its
// email around the registration, which POST /auth/register and the account
// command call. Registration reads whether a password credential holds the
// email and then creates a person, so without the keys an asserted sign-in that
// read "nobody holds this email" in between would create a second person.
//
// The lock does not make registration look at other credentials: it still only
// refuses an email a password credential holds, so registering after an
// asserted sign-in for the same email creates a second person, lock or not. See
// docs/decisions/trusted-issuer-login-assertion.md, "Races".
func (s *newAccountSignalService) RegisterPassword(ctx context.Context, email, displayName, plaintext string) (*entities.Agent, *entities.Credential, *entities.Account, error) {
	if s.lock != nil {
		// The subject pericarp stores a password credential under.
		subject := strings.ToLower(strings.TrimSpace(email))
		release, err := s.lock.Hold(ctx, signInLockKeys(entities.ProviderPassword, subject, repositories.FoldCredentialEmail(email))...)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("hold the sign-in lock: %w", err)
		}
		defer release()
	}
	return s.AuthenticationService.RegisterPassword(ctx, email, displayName, plaintext)
}
