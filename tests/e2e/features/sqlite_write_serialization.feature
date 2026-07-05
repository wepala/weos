Feature: SQLite write bursts queue instead of failing with database-is-locked
  As an operator running WeOS on SQLite
  I want concurrent writes to queue for the database instead of erroring
  So that a burst of request and subscriber-checkpoint writes always lands
  while reads and server boot stay responsive

  Background:
    Given a clean WeOS instance backed by a file-based SQLite database
    And the "tasks" preset is installed

  Scenario: A concurrent write burst lands without database-is-locked errors
    Given the checkpointed subscribers "lexical-index", "event-references" and "display-values" are running
    When 10 "task" resources are created concurrently within one second
    Then every create succeeds
    And no "database is locked" error is logged during the burst

  Scenario: Subscriber checkpoints survive a write burst
    Given the checkpointed subscribers "lexical-index", "event-references" and "display-values" are running
    When 10 "task" resources are created concurrently within one second
    And the subscribers finish processing the burst
    Then each subscriber's checkpoint has advanced past every event in the burst

  Scenario: The server boots healthy while subscribers replay an event backlog
    Given the event store holds a backlog of 200 events no subscriber has processed
    When the server is restarted
    Then the health endpoint reports healthy within 5 seconds of boot
    And every subscriber eventually processes the backlog

  Scenario: Reads are served while a write burst is in flight
    Given a "project" named "Client onboarding" exists
    When a sustained burst of concurrent writes is in flight
    And I fetch the project "Client onboarding" during the burst
    Then the read succeeds while the burst is still in flight

  Scenario: A transient database-is-locked failure is retried until the write lands
    Given the next write to the database transiently fails with SQLITE_BUSY
    When I create a "project" named "Client onboarding"
    Then the create succeeds
    And the write was attempted more than once

  Scenario: A database that stays locked surfaces an error instead of retrying forever
    Given the database stays locked for longer than the retry budget allows
    When I create a "project" named "Client onboarding"
    Then the create fails with a database-locked error
    And the write gave up after a bounded number of attempts
