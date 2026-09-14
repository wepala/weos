@epic-wm-63gg0
Feature: Limiting trusted-issuer sign-in to the allowlist and offering the door as the way back in
  As the owner of a fleet instance that signs people in through its front door
  I want the instance to admit only the people its allowlist names and to send me back to the door when my session ends
  So that the door's word opens my instance to me alone and an expired session is never a dead end

  The door verifies a person with Google or Apple before it vouches for them, but a verified
  person is not necessarily someone this instance should let in. An instance with an allowlist
  admits the people it names and nobody else, and it applies that rule to an assertion exactly
  as its own Google sign-in applies it: the assertion's email and every entry on the allowlist
  are compared without regard to capitals, and everything else about the address has to match.
  A plus-address or the same name at another domain is a different address. An instance whose
  allowlist is empty admits anyone the door vouches for.

  The allowlist is checked before the instance decides whom an assertion names, so a refused
  assertion leaves nothing behind: no person, no session, and no link to a person the instance
  already holds. That last case is staged against an owner the instance already has, because
  the account contract links an owner's new identity by email and the refusal has to win over
  that link. A person who has signed in before is refused like anyone else once the allowlist
  stops naming them. Every refusal here names the reason allowlist, and the scenarios assert
  that reason rather than the status the refusals share.

  An instance whose only way in is the door used to answer an empty list when the sign-in
  screen asked which sign-in providers it offers, so a person whose session had expired was
  shown nothing that worked. When the trusted issuer is fully configured the list now carries
  one more provider, named issuer, whose sign-in address is the door's start page on the
  issuer's own host. The providers the instance already offered are unchanged. An instance
  with no trusted issuer offers no such provider, and neither does one whose trusted issuer is
  only partly configured, because that instance has no assertion path for the door to use.

  Whether the door's start page really lives at that address is the door's contract. This one
  fixes only the address the instance publishes.

  The issuer's entry also lists the sign-in providers an assertion may name on this instance.
  An instance on an older build refuses a provider it does not know with the same reason as an
  assertion that carries no subject, so the door cannot learn from a refusal that the instance
  is too old. It reads the list instead, before it sends a person there. The list names
  providers only, as the entry already did, and carries no configuration.

  The issuer's entry also says that the instance joins a door identity and a Google or Apple
  identity with the same email into one person. An instance on an older build makes a second,
  empty person instead, and the two stay apart after it is upgraded. So the door offers a
  person a second way to sign in to an instance only when the instance says this.

  # --- An email the allowlist does not name ---

  @story-wm-63gg0.3
  Scenario: An assertion for an email the allowlist does not name is refused without creating anybody
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    When the door presents an assertion for "marcus.okafor@harborlegal.example" from "google" with the subject "114902375861204937715"
    Then the sign-in is refused
    And the refusal names the reason "allowlist"
    And "marcus.okafor@harborlegal.example" holds no session on the instance
    And the store holds no person for "marcus.okafor@harborlegal.example"

  @story-wm-63gg0.3
  Scenario Outline: An email that only resembles the allowlisted one is refused
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    When the door presents an assertion for "<email>" from "google" with the subject "103958274610385729164"
    Then the sign-in is refused
    And the refusal names the reason "allowlist"

    Examples:
      | email                                   |
      | dana.whitfield+door@harborlegal.example |
      | dana.whitfield@harborlegal.example.net  |
      | whitfield@harborlegal.example           |

  # --- The allowlist is checked before the instance decides whom an assertion names ---

  @story-wm-63gg0.3
  Scenario: A person the instance already holds is refused rather than linked when the allowlist does not name them
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the account "Harbor Legal", whose owner "ops@harborlegal.example" signs in with password "correct-horse-battery-staple"
    And the instance's allowlist names "dana.whitfield@harborlegal.example"
    When the door presents an assertion for "ops@harborlegal.example" from "google" with the subject "117590246813570924368"
    Then the sign-in is refused
    And the refusal names the reason "allowlist"
    And no identity from "google" is linked to "ops@harborlegal.example"

  @story-wm-63gg0.3
  Scenario: A person who signed in before is refused once the allowlist stops naming them
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And "dana.whitfield@harborlegal.example" signed in through the door from "google" with the subject "108234917650023841257" earlier
    And the instance has since been restarted with an allowlist naming only "marcus.okafor@harborlegal.example"
    When the door presents an assertion for "dana.whitfield@harborlegal.example" from "google" with the subject "108234917650023841257"
    Then the sign-in is refused
    And the refusal names the reason "allowlist"

  # --- Who the allowlist admits ---

  @story-wm-63gg0.3
  Scenario Outline: The allowlist admits an email written in different capitals
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance's allowlist names "<listed>"
    When the door presents an assertion for "<presented>" from "google" with the subject "108234917650023841257"
    Then the sign-in succeeds
    And the sign-in reports that it created a new account

    Examples:
      | listed                             | presented                          |
      | dana.whitfield@harborlegal.example | Dana.Whitfield@HarborLegal.example |
      | Dana.Whitfield@HarborLegal.example | dana.whitfield@harborlegal.example |

  @story-wm-63gg0.3
  Scenario: An instance with no allowlist admits anyone the door vouches for
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has no allowlist
    When the door presents an assertion for "marcus.okafor@harborlegal.example" from "apple" with the subject "000917.3b6e2d1c9a8f4b7e8c5d2a1f0e9b6c3d.2208"
    Then the sign-in succeeds
    And the sign-in reports that it created a new account

  # --- The way back to the door ---

  @story-wm-63gg0.3
  Scenario: An instance that trusts an issuer offers the door as a sign-in provider
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    When someone who is not signed in asks the instance which sign-in providers it offers
    Then the instance offers the sign-in provider "issuer" with the sign-in address "https://money.weos.cloud/door/start"
    And that provider carries nothing but its name, its sign-in address, the sign-in providers an assertion may name and whether it joins identities by email

  @wm-6lx6z
  Scenario: The door can read that the instance joins a door identity and a Google or Apple identity with one email
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    When someone who is not signed in asks the instance which sign-in providers it offers
    Then the "issuer" provider says the instance joins a door identity and a Google or Apple identity with the same email

  @wm-x0l4m
  Scenario Outline: The door can read which sign-in providers an assertion may name before it sends a person
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    When someone who is not signed in asks the instance which sign-in providers it offers
    Then the "issuer" provider lists "<provider>" among the sign-in providers an assertion may name

    Examples:
      | provider |
      | google   |
      | apple    |
      | door     |

  @story-wm-63gg0.3
  Scenario: The door is offered beside the sign-in providers the instance already had
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And Google sign-in is also configured on that instance
    When someone who is not signed in asks the instance which sign-in providers it offers
    Then the instance offers exactly the sign-in providers "google" and "issuer"
    And the "google" provider carries no sign-in address

  @story-wm-63gg0.3
  Scenario: An instance with no trusted issuer offers no door to sign in through
    Given a WeOS instance with no trusted issuer configured
    When someone who is not signed in asks the instance which sign-in providers it offers
    Then the instance offers no sign-in provider named "issuer"

  @story-wm-63gg0.3
  Scenario: An instance missing its trusted issuer's audience offers no door to sign in through
    Given a WeOS instance configured with a trusted issuer and its key list but no audience
    When someone who is not signed in asks the instance which sign-in providers it offers
    Then the instance offers no sign-in provider named "issuer"
