package context_test

import (
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/context"
	"testing"
)

func TestContext_WithValue(t *testing.T) {
	t.Run("adding variable to context", func(t *testing.T) {
		e := echo.New()
		parentContext := &context.Context{
			Context: e.AcquireContext(),
		}

		ctxt := parentContext.WithValue(parentContext, "test", "ing")
		if ctxt.RequestContext().Value("test") != "ing" {
			t.Errorf("expected the context to have key '%s' with value '%s', got '%s'", "test", "ing", ctxt.RequestContext().Value("test"))
		}
	})
}
