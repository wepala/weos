@wip @postgres
Feature: PostgreSQL write concurrency is untouched by SQLite serialization
  As an operator running WeOS on PostgreSQL
  I want the connection pool to stay concurrent and the busy-retry to stay inert
  So that the SQLite write-serialization fix never slows down or alters PostgreSQL

  Background:
    Given a clean WeOS instance backed by a PostgreSQL database
    And the "tasks" preset is installed

  Scenario: Concurrent PostgreSQL writes are not serialized
    When 10 "task" resources are created concurrently within one second
    Then every create succeeds
    And more than one database connection served the writes at the same time

  Scenario: A failing PostgreSQL write is not retried by the busy handler
    Given the next write to the database fails with a non-recoverable error
    When I create a "project" named "Client onboarding"
    Then the create fails
    And the write was attempted exactly once
