Feature: In-app agent conversations over the HTTP API
  As a WeOS app user
  I want to converse with my app's agent and get renderable widget replies
  So that I have the same grounded experience in-app as through MCP

  Scenario: A message streams back a reply and completes
    Given a clean WeOS application
    And the in-app agent is configured with a scripted model that replies:
      """
      The knowledge graph holds 3 people.
      """
    When the user sends "how many people do we know?" in conversation "counting"
    Then the stream emits "text", "widgets", and "done" events in order
    And the reply contains a markdown widget with text "The knowledge graph holds 3 people."

  Scenario: Contract JSON from the model arrives as a typed widget
    Given a clean WeOS application
    And the in-app agent is configured with a scripted model that replies:
      """
      {"schemaVersion":1,"widgets":[{"type":"table","title":"People","columns":["Name"],"rows":[["Ada"]]}]}
      """
    When the user sends "list people as a table" in conversation "tables"
    Then the reply contains a "table" widget titled "People"

  Scenario: Malformed model output still renders
    Given a clean WeOS application
    And the in-app agent is configured with a scripted model that replies:
      """
      {"schemaVersion":1,"widgets":[{"type":"hologram","spin":true}]}
      """
    When the user sends "do something odd" in conversation "degrade"
    Then the reply contains a markdown widget carrying the unrenderable payload

  Scenario: Conversation history survives across turns
    Given a clean WeOS application
    And the in-app agent is configured with a scripted model that replies:
      """
      Noted.
      """
    When the user sends "remember the office moves Friday" in conversation "moving"
    And the user sends "and the lease ends in March" in conversation "moving"
    And the user asks for the history of conversation "moving"
    Then the history holds 4 messages alternating user and agent
    And the first history message is a user message saying "remember the office moves Friday"

  Scenario: A completed turn is remembered as an episodic note
    Given a clean WeOS application
    And the in-app agent is configured with a scripted model that replies:
      """
      Got it — the office moves Friday.
      """
    And the "memory" preset is installed on the application
    When the user sends "the office moves Friday" in conversation "episodes"
    Then an episodic note records the exchange about "the office moves Friday"

  Scenario: Answering a confirmation without a decision is rejected
    Given a clean WeOS application
    And the in-app agent is configured with a scripted model that replies:
      """
      ok
      """
    When the user answers pending confirmation "call-1" in conversation "hitl" with no decision
    Then the request is rejected as a bad request

  Scenario: An unconfigured agent says so instead of failing obscurely
    Given a clean WeOS application
    And the in-app agent has no model configured
    When the user sends "hello" in conversation "unconfigured"
    Then the request is rejected as unavailable
    And the error explains the agent is not configured
