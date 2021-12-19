package context

import (
	"github.com/labstack/echo/v4"
	"golang.org/x/net/context"
)

type ContextKey string

//based on recommendations here https://www.calhoun.io/pitfalls-of-context-values-and-how-to-avoid-or-mitigate-them/
const HeaderXAccountID = "X-Account-ID"
const HeaderXLogLevel = "X-LOG-LEVEL"

//add more keys here if needed
const ACCOUNT_ID ContextKey = "ACCOUNT_ID"
const USER_ID ContextKey = "USER_ID"
const LOG_LEVEL ContextKey = "LOG_LEVEL"
const REQUEST_ID ContextKey = "REQUEST_ID"
const FILTERS ContextKey = "_filters"
const SORTS ContextKey = "_sorts"

//---- Context Getters

//Get account info from context
func GetAccount(ctx context.Context) string {
	if value, ok := ctx.Value(ACCOUNT_ID).(string); ok {
		return value
	}
	return ""
}

//Get user info from context
func GetUser(ctx context.Context) string {
	if value, ok := ctx.Value(USER_ID).(string); ok {
		return value
	}
	return ""
}

//Get log level from context
func GetLogLevel(ctx context.Context) string {
	if value, ok := ctx.Value(LOG_LEVEL).(string); ok {
		return value
	}
	return ""
}

//Get request id from context
func GetRequestID(ctx context.Context) string {
	if value, ok := ctx.Value(REQUEST_ID).(string); ok {
		return value
	}
	return ""
}

type Context struct {
	echo.Context
	requestContext context.Context
}

func New(ctxt echo.Context) *Context {
	return &Context{
		Context:        ctxt,
		requestContext: context.Background(),
	}
}

func (c *Context) WithValue(parent *Context, key, val interface{}) *Context {
	if parent.requestContext != nil {
		parent.requestContext = context.WithValue(parent.requestContext, key, val)
	} else {
		parent.requestContext = context.WithValue(context.TODO(), key, val)
	}
	return parent
}

func (c *Context) RequestContext() context.Context {
	return c.requestContext
}

func (c *Context) Value(key interface{}) interface{} {
	return c.requestContext.Value(key)
}
