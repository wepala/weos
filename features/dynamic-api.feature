Feature: Dynamically Manage API

  You can manage an api statically by update the OpenAPI specification that WeOS uses or by sending requests to endpoints
  that are dynamically managed.

  Scenario: Create collection by sending an item in a json payload

    A developer can send an item to a none existent endpoint and a collection should be created at that endpoint with
    the item sent as the first item

  Scenario: Create collection by sending an array of items in a json payload

    A developer can create a collection by sending an array of items

  Scenario: Use JSONPath to specify the input for request

  Scenario: Create collection by declaring a get endpoint in the OpenAPI spec

  Scenario: Create item without creating collection

    A developer may want to create a specific item at a specific endpoint without the default behavior of a collection
    being created. This is the same as updating an item

  Scenario: Creating an item at an endpoint that already exists

    If an item already exists at an endpoint then an error should be return (unless it's a PUT which is idempotent)

  Scenario: Delete a collection

    Deleting a collection deletes the collection only and NOT the sub items

  Scenario: Delete a collection and sub items

  Scenario: Patching an item

    This is a partial update to an item





