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
	"context"
	"strings"

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// schemaChangeGuard is the middleware serve puts on every route that changes
// what all accounts share — creating, updating and deleting resource types,
// and installing presets (wm-gu3pm) — and says at boot what it does. Setting
// a type's behaviors is not one of them: it writes the caller's own account's
// setting only (wm-9m6sj).
//
//   - Sign-in on, INSTANCE_ADMIN_ACCOUNT set: only an owner or admin of that
//     account, acting in it, gets through (apimw.RequireInstanceAdmin).
//   - Sign-in on, unset: everything gets through, as before, and one warning
//     says that any signed-in account can change the resource types. mini-me
//     and Little Apollo run this way until they name an account.
//   - No sign-in (local development): everything gets through, as before.
//     SoftAuth answers every caller as the seeded dev user, so a check here
//     would guard nothing; a named account is ignored, and the boot says so.
func schemaChangeGuard(
	appCfg config.Config, accountRepo authrepos.AccountRepository, logger entities.Logger,
) echo.MiddlewareFunc {
	ctx := context.Background()
	// Trimmed here as the guard trims it, so the boot log and the routes
	// cannot disagree about whether an account is named (wm-0xf60).
	account := strings.TrimSpace(appCfg.InstanceAdminAccountID)
	switch {
	case !appCfg.AuthEnabled():
		if account != "" {
			logger.Warn(ctx,
				"INSTANCE_ADMIN_ACCOUNT is set but no sign-in is configured, so it is ignored: "+
					"every caller is the dev user and may change resource types",
				"remedy", "configure a sign-in (OAuth, PASSWORD_AUTH_ENABLED or a trusted issuer)")
		}
		return apimw.RequireInstanceAdmin("", accountRepo, logger)
	case account == "":
		logger.Warn(ctx,
			"INSTANCE_ADMIN_ACCOUNT is not set, so any signed-in account can create, change or delete "+
				"the resource types and presets every account on this instance shares",
			"remedy", "set INSTANCE_ADMIN_ACCOUNT to the id of the account whose owners and admins run the instance")
		return apimw.RequireInstanceAdmin("", accountRepo, logger)
	default:
		logger.Info(ctx, "resource type and preset changes are limited to the instance admin account",
			"account_id", account)
		checkInstanceAdminAccountExists(ctx, account, accountRepo, logger)
		return apimw.RequireInstanceAdmin(account, accountRepo, logger)
	}
}

// checkInstanceAdminAccountExists warns when account names no account on the
// instance. A mistyped id — or the operator's agent id in place of their
// account id — refuses every schema change, the operator's too, and nothing
// else would say why. It does not stop serve: on a fresh deploy the
// operator's account exists only after their first sign-in, so the variable
// may rightly name an account that comes later (wm-0xf60).
func checkInstanceAdminAccountExists(
	ctx context.Context, account string, accountRepo authrepos.AccountRepository, logger entities.Logger,
) {
	found, err := accountRepo.FindByID(ctx, account)
	if err != nil {
		logger.Error(ctx, "INSTANCE_ADMIN_ACCOUNT could not be checked: failed to read the account",
			"account_id", account, "error", err)
		return
	}
	if found == nil {
		logger.Warn(ctx,
			"INSTANCE_ADMIN_ACCOUNT names an account that does not exist, so no one can create, change or "+
				"delete resource types or install presets over HTTP",
			"account_id", account,
			"remedy", "sign in as the operator, read data.account_id (not data.id, the person) from GET /api/auth/me, "+
				"set INSTANCE_ADMIN_ACCOUNT to it and restart")
	}
}
