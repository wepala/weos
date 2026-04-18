package middleware

import (
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/labstack/echo/v4"
)

// Messages returns middleware that injects a message accumulator into the
// request context. Services can call entities.AddMessage(ctx, msg) to
// accumulate messages; handlers extract them with entities.GetMessages(ctx).
func Messages() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := entities.ContextWithMessages(c.Request().Context())
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}
