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
	"errors"
	"net/http"
	"strconv"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/labstack/echo/v4"
)

// NotificationHandler exposes the per-user notification inbox: list recent
// notifications, read the unread count, and mark one or all read. Every
// operation is scoped to the authenticated caller by the underlying service;
// the handler additionally refuses unauthenticated callers so the inbox can
// never fall back to a cross-user (system) read if mounted without auth.
type NotificationHandler struct {
	notifications application.NotificationService
	logger        entities.Logger
}

// NewNotificationHandler creates the handler.
func NewNotificationHandler(
	notifications application.NotificationService, logger entities.Logger,
) *NotificationHandler {
	return &NotificationHandler{notifications: notifications, logger: logger}
}

// List handles GET /notifications — the caller's notifications, newest first.
// An optional ?limit caps the page (default applied by the service).
func (h *NotificationHandler) List(c echo.Context) error {
	if !authenticated(c) {
		return respondUnauthorized(c)
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit")) //nolint:errcheck // 0 => service default
	views, err := h.notifications.List(c.Request().Context(), limit)
	if err != nil {
		h.logger.Error(c.Request().Context(), "list notifications failed", "error", err)
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusOK, views)
}

// UnreadCount handles GET /notifications/unread-count.
func (h *NotificationHandler) UnreadCount(c echo.Context) error {
	if !authenticated(c) {
		return respondUnauthorized(c)
	}
	count, err := h.notifications.UnreadCount(c.Request().Context())
	if err != nil {
		h.logger.Error(c.Request().Context(), "unread count failed", "error", err)
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusOK, map[string]int{"unread": count})
}

// MarkRead handles POST /notifications/:id/read.
func (h *NotificationHandler) MarkRead(c echo.Context) error {
	if !authenticated(c) {
		return respondUnauthorized(c)
	}
	view, err := h.notifications.MarkRead(c.Request().Context(), c.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, entities.ErrAccessDenied):
			return respondForbidden(c)
		case errors.Is(err, repositories.ErrNotFound):
			return respondError(c, http.StatusNotFound, "notification not found")
		default:
			h.logger.Error(c.Request().Context(), "mark notification read failed",
				"id", c.Param("id"), "error", err)
			return respondError(c, http.StatusInternalServerError, err.Error())
		}
	}
	return respond(c, http.StatusOK, view)
}

// MarkAllRead handles POST /notifications/mark-all-read.
func (h *NotificationHandler) MarkAllRead(c echo.Context) error {
	if !authenticated(c) {
		return respondUnauthorized(c)
	}
	marked, err := h.notifications.MarkAllRead(c.Request().Context())
	if err != nil {
		h.logger.Error(c.Request().Context(), "mark all notifications read failed", "error", err)
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusOK, map[string]int{"marked": marked})
}

// authenticated reports whether the request carries a resolved identity.
func authenticated(c echo.Context) bool {
	id := auth.AgentFromCtx(c.Request().Context())
	return id != nil && id.AgentID != ""
}

func respondUnauthorized(c echo.Context) error {
	return respondError(c, http.StatusUnauthorized, "authentication required")
}
