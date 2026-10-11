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

package cli

import (
	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"go.uber.org/fx"
)

// nativeSessionOptions provide, in serve's graph, the refresh token store and
// the native session issuer built over it (wm-xbd5h). serve takes both from
// the graph, so the sign-ins, the renewal route, the token endpoint and the
// door's revocation share one store; and a downstream binary reaches the
// issuer from an fx.Invoke it registers with RegisterFxOptions, to start a
// native session from its own sign-in.
func nativeSessionOptions() fx.Option {
	return fx.Options(
		// One refresh token store for connectors and native apps: a native
		// sign-in's refresh token is kept, rotated and revoked there too
		// (wm-lnimb).
		fx.Provide(weosoauth.NewRefreshTokenRepository),
		fx.Provide(provideNativeSessionIssuer),
	)
}

// nativeSessionIssuerParams is what provideNativeSessionIssuer is built from.
type nativeSessionIssuerParams struct {
	fx.In
	AuthService   authapp.AuthenticationService
	RefreshTokens weosoauth.RefreshTokenRepository
	AgentRepo     authrepos.AgentRepository
	AccountRepo   authrepos.AccountRepository
	ErasureLocks  repositories.AccountErasureLocks
	Logger        entities.Logger
}

func provideNativeSessionIssuer(p nativeSessionIssuerParams) *handlers.NativeSessionIssuer {
	return handlers.NewNativeSessionIssuer(handlers.NativeSessionIssuerConfig{
		AuthService:   p.AuthService,
		RefreshTokens: p.RefreshTokens,
		AgentRepo:     p.AgentRepo,
		AccountRepo:   p.AccountRepo,
		ErasureLocks:  p.ErasureLocks,
		Logger:        p.Logger,
	})
}
