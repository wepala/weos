package application

import (
	"context"
	"errors"
	"testing"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
)

// fakeCredentialRepo implements just the FindByProvider seam the decorator
// touches; every other method falls through to the embedded nil interface and
// would panic if called (none are, by this test).
type fakeCredentialRepo struct {
	authrepos.CredentialRepository
	cred *entities.Credential
	err  error
}

func (f *fakeCredentialRepo) FindByProvider(context.Context, string, string) (*entities.Credential, error) {
	return f.cred, f.err
}

// fakeAuthService stubs the embedded AuthenticationService so the decorator has
// something to delegate to. Only FindOrCreateAgent is exercised.
type fakeAuthService struct {
	authapp.AuthenticationService
}

func (fakeAuthService) FindOrCreateAgent(context.Context, authapp.UserInfo) (*entities.Agent, *entities.Credential, *entities.Account, error) {
	return nil, nil, nil, nil
}

func TestNewAccountSignalService_FindOrCreateAgent(t *testing.T) {
	tests := []struct {
		name        string
		repo        *fakeCredentialRepo
		installFlag bool
		wantNew     bool
	}{
		{
			name:        "no flag installed leaves no signal and skips the lookup",
			repo:        &fakeCredentialRepo{err: errors.New("should not be called")},
			installFlag: false,
		},
		{
			name:        "missing credential marks the account as new",
			repo:        &fakeCredentialRepo{cred: nil},
			installFlag: true,
			wantNew:     true,
		},
		{
			name:        "existing credential marks the account as returning",
			repo:        &fakeCredentialRepo{cred: &entities.Credential{}},
			installFlag: true,
			wantNew:     false,
		},
		{
			name:        "lookup error is treated as new rather than failing login",
			repo:        &fakeCredentialRepo{err: errors.New("db down")},
			installFlag: true,
			wantNew:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &newAccountSignalService{AuthenticationService: fakeAuthService{}, credentials: tt.repo}

			ctx := context.Background()
			var flag *bool
			if tt.installFlag {
				flag = new(bool)
				ctx = WithNewAccountFlag(ctx, flag)
			}

			if _, _, _, err := svc.FindOrCreateAgent(ctx, authapp.UserInfo{Provider: "apple", ProviderUserID: "sub-123"}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.installFlag {
				if flag != nil {
					t.Fatalf("no flag was installed")
				}
				return
			}
			if *flag != tt.wantNew {
				t.Errorf("new-account flag = %v, want %v", *flag, tt.wantNew)
			}
		})
	}
}
