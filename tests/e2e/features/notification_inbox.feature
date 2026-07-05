@epic-427
Feature: Notification inbox
  A WeOS service can produce a notification addressed to a user, and that user
  can read their inbox: list recent notifications newest-first, see how many are
  unread, and mark one or all of them read. Production is idempotent on a stable
  key so re-emitting the same signal never duplicates, and every inbox operation
  is scoped to the authenticated user.

  Background:
    Given a running WeOS application with the notification capability
    And a user "Alice"
    And a user "Bob"

  Scenario: A produced notification appears in the recipient's inbox
    When the service notifies "Alice" with title "Payment received" and key "pay-001"
    Then "Alice" has 1 notification in her inbox
    And the newest notification in "Alice" inbox is titled "Payment received"

  Scenario: The unread count reflects unread notifications
    When the service notifies "Alice" with title "Payment received" and key "pay-001"
    And the service notifies "Alice" with title "Statement ready" and key "stmt-002"
    Then "Alice" has an unread count of 2

  Scenario: Marking one notification read decrements the unread count
    Given the service notifies "Alice" with title "Payment received" and key "pay-001"
    And the service notifies "Alice" with title "Statement ready" and key "stmt-002"
    When "Alice" marks the notification titled "Payment received" as read
    Then "Alice" has an unread count of 1
    And the notification titled "Payment received" in "Alice" inbox is read

  Scenario: Marking all notifications read zeroes the unread count
    Given the service notifies "Alice" with title "Payment received" and key "pay-001"
    And the service notifies "Alice" with title "Statement ready" and key "stmt-002"
    When "Alice" marks all notifications read
    Then "Alice" has an unread count of 0

  Scenario: Re-emitting the same signal does not duplicate
    When the service notifies "Alice" with title "Payment received" and key "pay-001"
    And the service notifies "Alice" with title "Payment received" and key "pay-001"
    Then "Alice" has 1 notification in her inbox
    And "Alice" has an unread count of 1

  Scenario: Notifications are scoped to their recipient
    When the service notifies "Alice" with title "For Alice" and key "a-1"
    And the service notifies "Bob" with title "For Bob" and key "b-1"
    Then "Alice" has 1 notification in her inbox
    And the newest notification in "Alice" inbox is titled "For Alice"
    And "Bob" has 1 notification in her inbox
    And the newest notification in "Bob" inbox is titled "For Bob"

  Scenario: The inbox lists the newest notification first
    When the service notifies "Alice" with title "First" and key "n-1"
    And the service notifies "Alice" with title "Second" and key "n-2"
    Then the newest notification in "Alice" inbox is titled "Second"

  Scenario: An account admin cannot read or mark another member's notification
    Given a user "Manager"
    And "Manager" is an account admin of "Alice"
    When the service notifies "Alice" with title "Salary posted" and key "s-1"
    Then "Manager" cannot read the notification titled "Salary posted"
    And "Manager" is denied marking the notification titled "Salary posted" as read

  Scenario: An inbox read with no identity is refused, not served cross-user
    When the service notifies "Alice" with title "Private note" and key "p-1"
    Then a caller with no identity is refused when listing notifications
