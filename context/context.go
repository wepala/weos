package context

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"golang.org/x/net/context"
)

type ContextKey string

//based on recommendations here https://www.calhoun.io/pitfalls-of-context-values-and-how-to-avoid-or-mitigate-them/
const HeaderXAccountID = "X-Account-ID"
const HeaderXLogLevel = "X-LOG-LEVEL"

//add more keys here if needed
const ACCOUNT_ID ContextKey = "ACCOUNT_ID"
const OPERATION_ID = "OPERATION_ID"
const USER_ID ContextKey = "USER_ID"
const LOG_LEVEL ContextKey = "LOG_LEVEL"
const REQUEST_ID ContextKey = "REQUEST_ID"
const WEOS_ID ContextKey = "WEOS_ID"
const CONTENT_TYPE ContextKey = "_contentType"
const ENTITY_FACTORY ContextKey = "_entityFactory"
const MIDDLEWARES ContextKey = "_middlewares"
const CONTROLLER ContextKey = "_controller"
const PROJECTION ContextKey = "_projection"
const COMMAND_DISPATCHER ContextKey = "_command_disptacher"
const EVENT_STORE ContextKey = "_event_store"
const SCHEMA_BUILDERS ContextKey = "_schema_builders"
const FILTERS ContextKey = "_filters"
const SORTS ContextKey = "_sorts"
const PAYLOAD ContextKey = "_payload"
const SEQUENCE_NO string = "sequence_no"
const RESPONSE_PREFIX string = "_httpstatus"

//Path initializers are run per path and can be used to configure routes that are not defined in the open api spec
const METHODS_FOUND ContextKey = "_methods_found"

//entity
const ENTITY_ID = "_entity_id"
const ENTITY_COLLECTION = "_entity_collection"
const ENTITY = "_entity"
const ERROR = "_error"

//ContentType this makes it easier to access the content type information in the context
type ContentType struct {
	Name   string           `json:"name"`
	Schema *openapi3.Schema `json:"fields"`
}

//---- Context Getters

func GetContentType(ctx context.Context) *ContentType {
	if value, ok := ctx.Value(CONTENT_TYPE).(*ContentType); ok {
		return value
	}
	return nil
}

//GetAccount info from context
func GetAccount(ctx context.Context) string {
	if value, ok := ctx.Value(ACCOUNT_ID).(string); ok {
		return value
	}
	return ""
}

//GetUser info from context
func GetUser(ctx context.Context) string {
	if value, ok := ctx.Value(USER_ID).(string); ok {
		return value
	}
	return ""
}

//GetLogLevel from context
func GetLogLevel(ctx context.Context) string {
	if value, ok := ctx.Value(LOG_LEVEL).(string); ok {
		return value
	}
	return ""
}

//GetRequestID from context
func GetRequestID(ctx context.Context) string {
	if value, ok := ctx.Value(REQUEST_ID).(string); ok {
		return value
	}
	return ""
}

//GetEntityID if it's in the context
func GetEntityID(ctx context.Context) string {
	if value, ok := ctx.Value(ENTITY_ID).(string); ok {
		return value
	}
	return ""
}

//GetError return error from context
func GetError(ctx context.Context) error {
	if value, ok := ctx.Value(ctx).(error); ok {
		return value
	}
	return nil
}

//GetPayload returns payload from context
func GetPayload(ctx context.Context) []byte {
	if value, ok := ctx.Value(PAYLOAD).([]byte); ok {
		return value
	}
	return []byte("")
}

//Deprecated: Context Use the Go context in the echo request instead
type Context struct {
	echo.Context
	requestContext context.Context
}

//Deprecated: New use the context in the echo request instead
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
