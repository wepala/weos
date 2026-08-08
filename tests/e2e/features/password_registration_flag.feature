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
  So these scenarios assert by comparison against an invented control path, and
  "answered identically" means the same status, the same body and the same
  authentication challenge, because a prober reads all three.

  What an ordinary unknown path returns depends on how the instance is deployed. With
  no OAuth provider configured, an unmatched path under /api answers "404 Not Found".
  With one configured it answers "401 Unauthorized" carrying a bearer challenge,
  because the group that owns unmatched paths then authenticates before it routes.
  Both are correct, and neither is the point of this story. A scenario that fixes a
  literal status is therefore only sound for the deployment shape its Given describes,
  which is why the ones that could run either way compare instead — and why two
  scenarios below pin the property on an OAuth-configured instance, so that shape
  stops being invisible.

  Scenario: An existing account signs in while registration is closed
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    And the instance already has the account "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  Scenario: A closed registration endpoint issues no challenge or method hint of its own
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    And someone posts the same details to "/api/auth/enroll", an endpoint this instance has never had
    Then both answers carry the same authentication challenge
    And neither answer advertises permitted methods for its path

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
    And someone posts the same details to "/api/auth/enroll", an endpoint this instance has never had
    Then both requests are answered identically, including any authentication challenge

  Scenario: Opening both settings lets a new account register and sign straight in
    Given a WeOS instance where password sign-in is enabled and account registration is enabled
    When someone registers "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    Then the registration succeeds
    And "newcomer@harborlegal.example" holds an authenticated session
    And "newcomer@harborlegal.example" can sign in again with password "correct-horse-battery-staple"

  Scenario: Registration cannot be opened while password sign-in is off
    Given a WeOS instance where password sign-in is disabled and account registration is enabled
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    And someone posts the same details to "/api/auth/enroll", an endpoint this instance has never had
    Then both requests are answered identically, including any authentication challenge

  Scenario: With password sign-in off, neither password endpoint is mounted
    Given a WeOS instance where password sign-in is disabled and account registration is not configured
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" attempts a password sign-in with password "correct-horse-battery-staple"
    And someone posts the same details to "/api/auth/enroll", an endpoint this instance has never had
    Then all three requests are answered identically, including any authentication challenge

  Scenario: The registration path stays ordinary on an instance that also uses OAuth
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    And an OAuth provider is also configured on that instance
    When someone submits a registration for "newcomer@harborlegal.example" with password "correct-horse-battery-staple"
    And someone posts the same details to "/api/auth/enroll", an endpoint this instance has never had
    Then both requests are answered identically, including any authentication challenge
    And no account exists for "newcomer@harborlegal.example"

  Scenario: Password sign-in still works on an instance that also uses OAuth
    Given a WeOS instance where password sign-in is enabled and account registration is disabled
    And an OAuth provider is also configured on that instance
    And the instance already has the account "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  Scenario: Closing registration does not lock out an account that registered earlier
    Given a WeOS instance where password sign-in is enabled and account registration is enabled
    And "founder@harborlegal.example" registered with password "correct-horse-battery-staple"
    When the operator restarts the instance with account registration disabled
    And "founder@harborlegal.example" signs in with password "correct-horse-battery-staple"
    Then the sign-in succeeds
    And a registration for "second-user@harborlegal.example" is answered exactly like a post to an endpoint that has never existed
