@epic-135 @story-136
Feature: Password sign-in without open registration
  As an operator running a WeOS instance
  I want to turn on password sign-in without also letting anyone who reaches the
  hostname create an account
  So that an instance can ship with its own sign-in account and no way to make another

  Registration is governed by its own setting, PASSWORD_REGISTRATION_ENABLED, which
  defaults to off. When it is off the register endpoint is never mounted: a caller
  finds nothing there at all, rather than a handler that inspects the request and
  refuses it. That difference is the point of this story — a route that answers its
  own distinctive refusal still confirms it exists, still parses what is sent to it,
  and still has to be kept correct forever. A route that was never mounted does none
  of those things.

  The test of absence is therefore not any one status code. It is that the path
  becomes ordinary: whoever probes it learns exactly what they would learn probing a
  path this instance has never had, under every method. A status that singled the
  registration path out — including a "404 Not Found" where neighbouring unknown
  paths answer otherwise — would give it away just as loudly as a refusal would.
  So the scenarios below fix a status only where an invented path returns that same
  status, and otherwise compare against an invented path directly.

  Scenario: An existing account signs in while registration is closed
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    And the instance already has the account "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  Scenario: A closed registration endpoint is absent rather than refusing
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    Then the registration request is answered "404 Not Found"
    And the answer offers no authentication challenge
    And the answer advertises no permitted methods for that path

  Scenario Outline: Under every method the registration path answers like one that never existed
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    When someone sends a <method> request to the registration endpoint
    And someone sends the same <method> request to "/api/auth/enroll", a path this instance has never had
    Then both requests are answered with the same status and the same body

    Examples:
      | method |
      | POST   |
      | GET    |
      | PUT    |
      | DELETE |

  Scenario: A closed registration path answers like a path the instance has never had
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    And someone posts the same details to "/api/auth/enroll", an endpoint this instance has never had
    Then both requests are answered with the same status and the same body

  Scenario: A closed registration endpoint reveals nothing about what it was sent
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    When someone submits a well-formed registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    And someone submits a registration request whose body is not valid JSON
    Then both requests are answered with the same status and the same body

  Scenario: A registration attempt made while registration is closed creates nothing
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    Then no account exists for "newcomer@harborlegal.example"
    And "newcomer@harborlegal.example" cannot sign in with password "correct-horse-battery-staple"

  Scenario: Enabling password sign-in alone leaves registration closed
    Given a WeOS instance where password sign-in is enabled and account registration is not configured
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    Then the registration request is answered "404 Not Found"

  Scenario: Opening both settings lets a new account register and sign straight in
    Given a WeOS instance where password sign-in is enabled and account registration is enabled
    When someone registers "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    Then the registration succeeds
    And "newcomer@harborlegal.example" holds an authenticated session
    And "newcomer@harborlegal.example" can sign in again with password "correct-horse-battery-staple"

  Scenario: Registration cannot be opened while password sign-in is off
    Given a WeOS instance where password sign-in is disabled and account registration is enabled
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    Then the registration request is answered "404 Not Found"

  Scenario: With password sign-in off, neither password endpoint is mounted
    Given a WeOS instance where password sign-in is disabled and account registration is not configured
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" attempts a password sign-in with password "correct-horse-battery-staple"
    Then both requests are answered "404 Not Found"

  Scenario: Closing registration does not lock out an account that registered earlier
    Given a WeOS instance where password sign-in is enabled and account registration is enabled
    And "founder@harborlegal.example" registered with password "correct-horse-battery-staple"
    When the operator restarts the instance with account registration disabled
    And "founder@harborlegal.example" signs in with password "correct-horse-battery-staple"
    Then the sign-in succeeds
    And a registration for "second-user@harborlegal.example" is answered "404 Not Found"
