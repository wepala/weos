Feature: Agent chat interface
  As a WeOS app user
  I want to chat with my app's agent in the admin UI
  So that I get grounded, renderable answers without a third-party AI client

  Scenario: The chat explains when the agent is not configured
    Given the instance has no agent model configured
    When the user opens the agent page
    Then the page explains the in-app agent is not configured

  Scenario: A streamed reply renders as prose
    Given the agent API is scripted to reply with the text "The graph holds 3 people."
    When the user opens the agent page
    And the user sends "how many people do we know?"
    Then the conversation shows the user message "how many people do we know?"
    And the conversation shows an agent reply containing "The graph holds 3 people."

  Scenario: A table widget renders as a table
    Given the agent API is scripted to reply with a table titled "People" listing:
      | Name  |
      | Ada   |
      | Grace |
    When the user opens the agent page
    And the user sends "list everyone as a table"
    Then the conversation shows a table titled "People"
    And the table lists "Ada"

  Scenario: A mutating action pauses for approval and resumes when approved
    Given the agent API is scripted to request confirmation for the tool "resource_create"
    And answering the confirmation is scripted to reply with the text "Created it."
    When the user opens the agent page
    And the user sends "add a note about the office move"
    Then an approval card for the tool "resource_create" appears
    When the user approves the pending action
    Then the conversation shows an agent reply containing "Created it."

  Scenario: The conversation survives a page reload
    Given the agent API is scripted to reply with the text "Noted."
    When the user opens the agent page
    And the user sends "remember the office moves Friday"
    And the agent history is scripted to return that exchange
    And the user reloads the page
    Then the conversation shows the user message "remember the office moves Friday"
    And the conversation shows an agent reply containing "Noted."
