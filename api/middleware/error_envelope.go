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

package middleware

import (
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/labstack/echo/v4"
)

// ErrorEnvelope wraps an error API response. The "error" key is kept for
// backward compatibility; the messages array provides the structured form.
// Code, when set, is a stable machine-readable name for the failure, for the
// cases a client must tell apart from the generic one.
//
// It is defined here, not in api/handlers, so middleware can answer with it
// too: handlers imports this package, so the type cannot live there and be
// used here. handlers.ErrorEnvelope is an alias of it, so there is one shape
// and one client parser (constitution Article VIII; wm-poxmk).
type ErrorEnvelope struct {
	Error    string             `json:"error"`
	Code     string             `json:"code,omitempty"`
	Messages []entities.Message `json:"messages,omitempty"`
}

// respondErrorEnvelope answers status with the error envelope, carrying the
// messages gathered on the request context, as handlers' respondErrorCode
// does.
func respondErrorEnvelope(c echo.Context, status int, msg, code string) error {
	msgs := entities.GetMessages(c.Request().Context())
	return c.JSON(status, ErrorEnvelope{Error: msg, Code: code, Messages: msgs})
}
