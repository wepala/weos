# WeOS Glossary of terms 


1. CommandDispatcher - This is a service that is responsible for dispatching commands to the appropriate receiver
2. Controllers - These are functions that are triggered for a specific operation on a specific path. Each operation on a route can only have 1 controller
2. Initializers - These are functions that be configured to do actions during the initialization of the api/app. There are three types of initializers, global, path, operation. Global initializers are executed before any other initializers. Path initializers are executed before operation initializers. Operation initializers are executed after path initializers.
3. Middleware - These are functions that are executed before the operation is executed.