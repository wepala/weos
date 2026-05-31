package application

import (
	"context"

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
// callback can distinguish a freshly created account from a returning login.
// It embeds the wrapped service and overrides only FindOrCreateAgent; every
// other method passes straight through.
type newAccountSignalService struct {
	authapp.AuthenticationService
	credentials authrepos.CredentialRepository
}

// FindOrCreateAgent pre-checks whether a credential already exists for the
// incoming identity. The check is the same lookup FindOrCreateAgent does
// internally as its first step, so a fresh row means this call is about to
// create the account. Only the credential's existence determines the signal;
// a lookup error is treated as "not found" (new) rather than masking the login.
//
// The pre-check only runs when a caller installed a flag via WithNewAccountFlag,
// keeping password and MCP login paths free of the extra query.
func (s *newAccountSignalService) FindOrCreateAgent(ctx context.Context, userInfo authapp.UserInfo) (*entities.Agent, *entities.Credential, *entities.Account, error) {
	if flag := NewAccountFlagFromContext(ctx); flag != nil {
		existing, _ := s.credentials.FindByProvider(ctx, userInfo.Provider, userInfo.ProviderUserID)
		*flag = existing == nil
	}
	return s.AuthenticationService.FindOrCreateAgent(ctx, userInfo)
}
