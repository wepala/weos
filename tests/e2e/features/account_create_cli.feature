@epic-135 @story-137
Feature: Minting an account from the command line
  As an operator standing up a WeOS instance
  I want to create the one account the instance needs from the command line
  So that minting an account never requires an HTTP route strangers can also reach

  Story #136 closed the registration endpoint by default, which leaves an instance
  with no way to get its first account. This command is the sanctioned replacement:
  it opens the store directly, with no server running, and creates the account
  through the same path the register endpoint used, so an account made this way and
  one that registered over HTTP are the same thing downstream.

  Its caller is an entrypoint script running under `set -e` on every start and every
  redeploy, so the exit code carries the meaning. Two conditions must be told apart
  by that one number. "The account is already there" is the normal state of every
  restart after the first, and must exit 0 and change nothing — including the
  existing account's password, which this command never rewrites, so a rotated
  variable quietly does nothing rather than locking the instance out of itself. A
  real failure must exit non-zero so the boot stops rather than continuing into a
  half-provisioned instance.

  "Already there" is judged by the email, not by how that email signs in. An address
  that arrived through Google is already a person on this instance, and registering a
  password identity for it would hand one human two unlinked agents — which nobody
  would notice, because it can only happen on some later restart after OAuth was
  added for an address this command had already provisioned.

  Two failures are reachable through the real command, and both are silent-success
  traps rather than crashes. A store that will not open is the obvious one. The
  quieter one is a database that was never specified: the command must refuse rather
  than fall back to a default file, because provisioning "an" instance is meaningless
  — the whole point is provisioning one particular store, and an instance whose DSN
  went missing would otherwise report success while its account went somewhere
  nobody will look. The third failure the code guards against — password support
  being unavailable — cannot be produced by any configuration, because the
  password-credential repository is wired unconditionally (application/module.go,
  application/auth_providers.go). A scenario for it could only be staged by handing
  the command a deliberately crippled service, which would demonstrate the harness
  rather than the product, so that mapping is asserted by a unit test in
  internal/cli instead. If the wiring ever becomes conditional, the scenario belongs
  back here.

  The password is the other reason this command exists. It must be able to arrive
  through a channel that does not put it in shell history or in the process table —
  the environment, or standard input — and once it arrives, no output, no error
  report and no log line may repeat it back. The scenarios below therefore assert
  the password's absence from what the command says as firmly as they assert the
  account's presence in the store.

  Scenario: An operator mints the instance's first account
    Given a WeOS store with no accounts, and no server running against it
    When the operator creates an account for "ops@harborlegal.example" with the password supplied through the environment
    Then the command exits successfully
    And the store holds exactly one account for "ops@harborlegal.example"

  Scenario: An account minted from the command line signs in like any other
    Given a WeOS store with no accounts, and no server running against it
    And the operator has created an account for "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When the instance is started with password sign-in enabled
    And "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  Scenario: A command-line account and a registered account are alike downstream
    Given a WeOS store with no accounts, and no server running against it
    And the operator has created an account for "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When the instance is started with password sign-in and account registration enabled
    And "founder@harborlegal.example" registers with password "correct-horse-battery-staple"
    And both "ops@harborlegal.example" and "founder@harborlegal.example" sign in with password "correct-horse-battery-staple"
    Then both sign-ins succeed
    And each of them owns a personal account

  Scenario: An account created without a name is named after its email
    Given a WeOS store with no accounts, and no server running against it
    When the operator creates an account for "ops@harborlegal.example" with no display name given
    Then the command exits successfully
    And the account for "ops@harborlegal.example" is presented as "ops"

  Scenario: An operator names the account when creating it
    Given a WeOS store with no accounts, and no server running against it
    When the operator creates an account for "ops@harborlegal.example" with the display name "Harbor Legal Ops"
    Then the command exits successfully
    And the account for "ops@harborlegal.example" is presented as "Harbor Legal Ops"

  Scenario: Creating the same account a second time succeeds and changes nothing
    Given a WeOS store where the operator has already created the account "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When the operator creates an account for "ops@harborlegal.example" with password "correct-horse-battery-staple" again
    Then the command exits successfully
    And the command reports that the account already exists
    And the store holds exactly one account for "ops@harborlegal.example"
    And "ops@harborlegal.example" can sign in with password "correct-horse-battery-staple"

  Scenario: A repeat run is a no-op rather than a password reset
    Given a WeOS store where the operator has already created the account "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When the operator creates an account for "ops@harborlegal.example" with password "trellis-anchor-mango-9"
    Then the command exits successfully
    And "ops@harborlegal.example" can sign in with password "correct-horse-battery-staple"
    And "ops@harborlegal.example" cannot sign in with password "trellis-anchor-mango-9"

  Scenario: A repeat run recognises the account whatever the email's capitalisation
    Given a WeOS store where the operator has already created the account "ops@harborlegal.example" with password "correct-horse-battery-staple"
    When the operator creates an account for "Ops@HarborLegal.example" with password "correct-horse-battery-staple"
    Then the command exits successfully
    And the command reports that the account already exists
    And the store holds exactly one account for "ops@harborlegal.example"

  Scenario: An email that already signs in another way is recognised, not duplicated
    Given a WeOS store where "ops@harborlegal.example" already signs in through Google
    When the operator creates an account for "ops@harborlegal.example" with password "correct-horse-battery-staple"
    Then the command exits successfully
    And the command reports that the account already exists, naming Google as how it signs in
    And the store holds exactly one account for "ops@harborlegal.example"
    And "ops@harborlegal.example" cannot sign in with password "correct-horse-battery-staple"

  Scenario: A store that cannot be opened stops the boot
    Given a WeOS store whose database cannot be opened
    When the operator creates an account for "ops@harborlegal.example" with the password supplied through the environment
    Then the command exits with a failure
    And the failure names the database as the reason

  Scenario: An unspecified database stops the boot rather than provisioning somewhere arbitrary
    Given a WeOS instance with no database configured
    When the operator creates an account for "ops@harborlegal.example" with the password supplied through the environment
    Then the command exits with a failure
    And the failure says no database was specified and names how to supply one
    And no account is created in any store
    And the command leaves no database behind in the directory it ran from

  Scenario: A missing password creates nothing rather than an account anyone can open
    Given a WeOS store with no accounts, and no server running against it
    When the operator creates an account for "ops@harborlegal.example" with no password supplied through any channel
    Then the command exits with a failure
    And no account exists for "ops@harborlegal.example"

  Scenario: A password can be handed over on standard input
    Given a WeOS store with no accounts, and no server running against it
    When the operator creates an account for "ops@harborlegal.example" with the password supplied on standard input
    Then the command exits successfully
    And "ops@harborlegal.example" can sign in with the password that was supplied
    And the password was never given as a command-line argument

  Scenario: An operator working by hand may still pass the password on the command line
    Given a WeOS store with no accounts, and no server running against it
    When the operator creates an account for "ops@harborlegal.example" with the password given on the command line
    Then the command exits successfully
    And "ops@harborlegal.example" can sign in with the password that was supplied

  Scenario: A successful run never repeats the password back, even when asked for detail
    Given a WeOS store with no accounts, and no server running against it
    And the operator has asked for verbose logging
    When the operator creates an account for "ops@harborlegal.example" with password "correct-horse-battery-staple" supplied through the environment
    Then the command exits successfully
    And the password appears nowhere in what the command printed or logged

  Scenario: A failure report never repeats the password back
    Given a WeOS store whose database cannot be opened
    And the operator has asked for verbose logging
    When the operator creates an account for "ops@harborlegal.example" with password "correct-horse-battery-staple" supplied through the environment
    Then the command exits with a failure
    And the password appears nowhere in what the command printed or logged

  Scenario: An account is minted into a store that is already in use
    Given a WeOS store that already holds the account "founder@harborlegal.example" and a task named "Renew the practice licence"
    And no server is running against it
    When the operator creates an account for "ops@harborlegal.example" with password "correct-horse-battery-staple"
    Then the command exits successfully
    And the store still holds the account "founder@harborlegal.example" and the task named "Renew the practice licence"
    And "ops@harborlegal.example" can sign in with password "correct-horse-battery-staple" once the instance is started
