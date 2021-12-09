# WeOS  Content Service

You can use the Content Service to manage content in an application. You can have a fully functional API by simply modeling your data in an Open API specification.

## Quick Start
1. Define content types in the API spec file.
2. Define the endpoints for interacting with content types
3. Run the API

### Define Content Types

For any WeOS service, you can define schemas for data used in the service. The Content Service uses those schema definitions (Content Types) to set up CRUD functionality and essential data stores.  WeOS also provides extensions for everyday data modeling tasks like setting relationships between Content Types, setting data types not offered by the OAS 3.0 specification, etc. Visit the Content Types documentation to learn more about modeling data in the Content Service.

### Define Endpoints

You can create endpoints that sort, filter, and paginate the content returned. You can set up the endpoints to create, delete, list, or view content. The Content Service automatically associates functionality to those endpoints based on the HTTP method and parameters applied.

### Deploy

You can run the API by executing a command on the Content Service and referencing the API specification you create. One binary, one API spec, that's all you need. Deploy your content service to WeOS and get a secure, easy to maintain API that is ready to use