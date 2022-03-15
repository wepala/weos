package rest

//WeOSConfigExtension weos configuration key
const WeOSConfigExtension = "x-weos-config"

//ContextNameExtension alias parameter name to a different name in the context
const ContextNameExtension = "x-context-name"

//MiddlewareExtension add middleware
const MiddlewareExtension = "x-middleware"

//ControllerExtension set controller
const ControllerExtension = "x-controller"

//RemoveExtension marks a field for removal
const RemoveExtension = "x-remove"

//CopyExtension is used to copy a field's values into another field, ignoring those that are not of the same type
const CopyExtension = "x-copy"

//IdentifierExtension set identifier
const IdentifierExtension = "x-identifier"

//AliasExtension alias parameter name to a different name in the controller
const AliasExtension = "x-alias"

//SchemaExtension alias for specifying the content type instead of the request body
const SchemaExtension = "x-schema"

//ProjectionExtension set custom projection
const ProjectionExtension = "x-projection"

//CommandDispatcherExtension set custom command dispatcher
const CommandDispatcherExtension = "x-command-dispatcher"

//EventStoreExtension set custom event store
const EventStoreExtension = "x-event-store"

const UniqueExtension = "x-unique"

//OPENIDCONNECTURLEXTENSION set the open id connect url
const OPENIDCONNECTURLEXTENSION = "openIdConnectUrl"

//SKIPEXPIRYCHECKEXTENSION set the expiry check
const SKIPEXPIRYCHECKEXTENSION = "skipExpiryCheck"

//ContextExtension set parameters directly in the context
const ContextExtension = "x-context"

//UploadExtension for the storage location of an upload
const UploadExtension = "x-upload"
