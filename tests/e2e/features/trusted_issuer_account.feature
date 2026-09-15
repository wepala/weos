@epic-wm-63gg0
Feature: Signing a person in to their own account from a trusted issuer's assertion
  As the owner of a fleet instance who signs in through its front door
  I want every sign-in the door vouches for to reach the one person and account I already have
  So that switching from Google to Apple never leaves me in a second, empty account

  Once an assertion has passed every check in the verification contract, the instance still
  has to decide whom it signs in. It decides the way its own Google and Apple sign-in already
  does: by the provider and the subject that provider gave the person, never by the email
  alone. A subject the instance has seen before reaches the same person every time, even when
  the email beside it has changed.

  One case differs, and it is the reason this contract exists. When a person signs in with a
  provider the instance has not seen them use, deciding by subject alone would create a second
  person with a second, empty account. So an identity the instance has never seen is linked to
  the person who already holds a credential for the same email, compared without regard to
  capitals, instead of becoming someone new. That holds whether or not the instance has an
  allowlist; the instances behind the door run with none. Only a credential whose email was
  proved counts. The link is stored rather than worked out again on every sign-in: the new
  identity keeps reaching its owner after the allowlist is cleared, and the identity it joined
  keeps working too.

  A person who signed up to the door with an email and a password arrives under the door's own
  provider, door. The door sends one provider and one subject for each person, so when that
  person later chooses Google at the door, the instance sees a Google identity it has never
  seen. A door credential means the issuer vouches for the email: the door proves it with a
  mailbox code at sign-up, and the door's operator also writes demo and owner people
  directly, so a door identity and a Google or Apple identity with the same email are one
  person, in either order. Two door identities with the same email are not one person.

  A link needs a credential that proves who owns the email. When credentials hold the email but
  none of them proves it, such as a password on an instance whose operator has not said that a
  password proves an owner, the instance neither links nor creates. It refuses the sign-in with
  the reason unproven-owner and tells the person to contact the operator of the instance, because
  only the operator can make the owner provable.

  The answer is a password sign-in's answer, field for field and cookie for cookie, because the
  browser makes this request itself through the door and must be left holding exactly what a
  password sign-in leaves it holding. It adds one field, new_account, which is true only when
  this sign-in created the person. The instance's own OAuth callback reports the same fact as a
  query on its redirect; a JSON answer has no redirect to carry it.

  Refusing an assertion belongs to the verification contract, and refusing an email the
  allowlist does not name belongs to the next story, so every email staged below is on the
  allowlist of the instance it is staged on.

  # --- A person is found by the identity their provider gave them ---

  @story-wm-63gg0.2
  Scenario: A first sign-in through the door creates the person and their account
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "google" with the subject "108234917650023841257"
    Then the sign-in succeeds
    And the sign-in reports that it created a new account
    And the store holds exactly one account for "dana.whitfield@harborlegal.example"
    And the account their requests act in is the one the sign-in reported

  @story-wm-63gg0.2
  Scenario: Signing in again with the same identity reaches the person made the first time
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "google" with the subject "108234917650023841257"
    Then the sign-in succeeds
    And the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier
    And the store holds exactly one account for "dana.whitfield@harborlegal.example"

  @story-wm-63gg0.2
  Scenario: A returning identity reaches its person even after their email changes
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example" and "dana.whitfield@cedarrealty.example"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    When the door presents an assertion for "dana.whitfield@cedarrealty.example" from "google" with the subject "108234917650023841257"
    Then the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier

  @story-wm-63gg0.2
  Scenario: Two first sign-ins for one identity arriving together leave a single person
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    When the door presents two separately signed assertions for "dana.whitfield@harborlegal.example" from "google" with the subject "108234917650023841257" at the same moment
    Then both sign-ins succeed
    And both sign-ins name the same person
    And the store holds exactly one account for "dana.whitfield@harborlegal.example"

  # --- An owner's new identity is linked to the owner instead of minting a second person ---

  @story-wm-63gg0.2
  Scenario: An owner who signed in with Google and then with Apple remains one person
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "apple" with the subject "001482.7f3c9a2e5b8d4e61a0c2f9b7d3e6a815.1734"
    Then the sign-in succeeds
    And the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier
    And the store holds exactly one account for "dana.whitfield@harborlegal.example"

  @wm-6lx6z
  Scenario: An owner who signed in with Google and then with Apple remains one person on an instance with no allowlist
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has no allowlist
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "apple" with the subject "001482.7f3c9a2e5b8d4e61a0c2f9b7d3e6a815.1734"
    Then the sign-in succeeds
    And the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier
    And the store holds exactly one account for "dana.whitfield@harborlegal.example"

  @story-wm-63gg0.2
  Scenario: An owner the operator created with a password is reached by their first sign-in through the door
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the instance's allowlist names "ops@harborlegal.example"
    When the door presents an assertion for "ops@harborlegal.example" from "google" with the subject "117590246813570924368"
    Then the sign-in succeeds
    And the sign-in reports that it created no new account
    And the account their requests act in is "Harbor Legal"

  @story-wm-63gg0.2
  Scenario: An owner is recognized when the door writes their email in different capitals
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    When the door presents an assertion for "Dana.Whitfield@HarborLegal.example" from "apple" with the subject "001482.7f3c9a2e5b8d4e61a0c2f9b7d3e6a815.1734"
    Then the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier

  @story-wm-63gg0.2
  Scenario Outline: Both of an owner's identities stay with the owner after the allowlist is cleared
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    And "dana.whitfield@harborlegal.example" signed in through the door from "apple" with the subject "001482.7f3c9a2e5b8d4e61a0c2f9b7d3e6a815.1734" earlier
    And the instance has since been restarted with no allowlist
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "<provider>" with the subject "<subject>"
    Then the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier

    Examples:
      | provider | subject                                      |
      | google   | 108234917650023841257                        |
      | apple    | 001482.7f3c9a2e5b8d4e61a0c2f9b7d3e6a815.1734 |

  # --- Where no link is made ---

  @story-wm-63gg0.2
  Scenario: Two people with different emails are never linked to one another
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example" and "marcus.okafor@harborlegal.example"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    When the door presents an assertion for "marcus.okafor@harborlegal.example" from "apple" with the subject "000917.3b6e2d1c9a8f4b7e8c5d2a1f0e9b6c3d.2208"
    Then the sign-in succeeds
    And the sign-in reports that it created a new account
    And the person the sign-in names is not the one "dana.whitfield@harborlegal.example" signed in as earlier

  @wm-am5ly
  Scenario: A sign-in for an email only a password holds is refused and tells the person to contact the operator
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has no allowlist
    And the instance does not let a password prove who owns an email
    And the operator created "ops@harborlegal.example" with the password "correct-horse-battery-staple"
    When the door presents an assertion for "ops@harborlegal.example" from "google" with the subject "117590246813570924368"
    Then the sign-in is refused with the reason "unproven-owner"
    And the refusal tells the person to contact the operator of the instance
    And the store holds exactly one account for "ops@harborlegal.example"

  # --- A door sign-in and a Google or Apple sign-in with the same email are one person ---

  @wm-6lx6z
  Scenario: A person who signed up to the door and then signs in with Google remains one person on an instance with no allowlist
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has no allowlist
    And "dana.whitfield@harborlegal.example" signed in through the door from "door" with the subject "2VhQ7kX9mT4rY8nL1pW6zC3dF5b" earlier
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "google" with the subject "108234917650023841257"
    Then the sign-in succeeds
    And the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier
    And the store holds exactly one account for "dana.whitfield@harborlegal.example"

  @wm-6lx6z
  Scenario: A person who signed in with Google and then signs up to the door remains one person on an instance with no allowlist
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has no allowlist
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "door" with the subject "2VhQ7kX9mT4rY8nL1pW6zC3dF5b"
    Then the sign-in succeeds
    And the sign-in reports that it created no new account
    And the person the sign-in names is the one "dana.whitfield@harborlegal.example" signed in as earlier
    And the store holds exactly one account for "dana.whitfield@harborlegal.example"

  # --- The answer the browser is left holding ---

  @story-wm-63gg0.2
  Scenario: The answer has a password sign-in's fields plus whether the sign-in created the person
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    When the door presents an assertion for "dana.whitfield@harborlegal.example" named "Dana Whitfield" from "google" with the subject "108234917650023841257"
    Then the sign-in succeeds
    And the answer carries these fields and no others:
      | field       | holds                                                                                   |
      | agent       | the person's id, the name "Dana Whitfield" and the email "dana.whitfield@harborlegal.example" |
      | account     | the id and name of the account the session acts in                                     |
      | token       | a token for that account                                                                |
      | expires_at  | the moment the session expires                                                          |
      | new_account | true                                                                                    |
    And the token cookie it sets holds the token the answer carries

  @story-wm-63gg0.2
  Scenario: A sign-in through the door sets the same cookies a password sign-in sets
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And password sign-in is also enabled on that instance
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the instance's allowlist names "ops@harborlegal.example"
    When "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the door presents an assertion for "ops@harborlegal.example" from "google" with the subject "117590246813570924368"
    Then the two sign-ins set the same cookies, each with the same path, lifetime and protections

  @story-wm-63gg0.2
  Scenario: A person the door gives no name is named after their email
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "marcus.okafor@harborlegal.example"
    When the door presents an assertion for "marcus.okafor@harborlegal.example" from "apple" with the subject "000917.3b6e2d1c9a8f4b7e8c5d2a1f0e9b6c3d.2208" and no name
    Then the sign-in succeeds
    And the answer names the person "marcus.okafor"

  @story-wm-63gg0.2
  Scenario: A returning person with no account left to act in is handed no token
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    And that person's own personal account has been deactivated
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "google" with the subject "108234917650023841257"
    Then the sign-in reports no account
    And the sign-in hands back no token
    And no token cookie is set
