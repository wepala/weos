@epic-wm-63gg0
Feature: Verifying a login assertion from a trusted issuer
  As someone the fleet's front door has already signed in
  I want the instance behind that door to accept the door's word that I am verified
  So that I reach my own instance without it having to talk to Google or Apple itself

  The door completes the OAuth flow and then hands the instance a short-lived signed
  statement that a named person is verified. The instance still owns its accounts and
  sessions, so the only thing it trusts is the signature of the one issuer it was
  configured with. Everything else about the statement is checked before anybody is
  signed in, and this contract is about those checks and nothing else. What an accepted
  assertion then does — resolving or creating the account, the shape of the answer, the
  new_account flag — belongs to the next story, so exactly one scenario here states that
  a good assertion is accepted. It is here because a refusal contract with no acceptance
  in it is satisfied by an endpoint that refuses everything.

  Every refusal carries a machine-readable reason, and the scenarios assert the reason
  rather than the status the refusals share. The door has to tell a clock problem it
  should retry from a key problem it must fix by publishing, and a person cannot read
  either off a bare 401. The reasons are signature, kid-miss, iss, aud, expired, window,
  jti-replay and claims.

  Three numbers decide most of the refusals and are staged on both sides of their
  boundary, because a check written with the wrong comparison passes every scenario that
  only stages the far side. Clocks are allowed to disagree by thirty seconds either way,
  so an assertion twenty seconds past its expiry is still good and one forty-five seconds
  past is not. An assertion may be minted to live at most sixty seconds, so sixty is
  accepted and ten minutes is not. An assertion may be presented once: its identifier is
  remembered for five minutes, which outlives any assertion that could still be valid.

  The issuer's key list is cached for ten minutes so that a sign-in does not depend on a
  fetch, and an unknown key triggers exactly one refetch so that the door can rotate a
  key without waiting out the cache. The refetch is the dangerous part: a fetch that
  comes back short — empty, failing, or simply without the key that was asked for — must
  fail that one request and leave the cached keys exactly as they were. Overwriting them
  would turn one bad rotation into ten minutes of a locked-out fleet, so one scenario
  below presents a good assertion after a refetch that came back empty, specifically to
  prove the cached keys survived it. Another presents a good assertion signed with a
  cached key while the key list cannot be reached, to prove that a cached key keeps
  working when the issuer does not answer.

  The assertion is a bearer credential for sixty seconds, so it is never written to a
  log. A refusal logs its reason; it does not log the token that caused it.

  # --- An assertion the instance trusts ---

  @story-wm-63gg0.1
  Scenario: An assertion from the trusted issuer signs the person in
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" signed with "door-2026-09"
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session
    And the answer names no refusal reason

  @story-wm-63gg0.1
  Scenario: An assertion minted to live a full sixty seconds is accepted
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" that lives 60 seconds
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  @story-wm-63gg0.1
  Scenario: An assertion 20 seconds past its expiry is inside the clock allowance
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" that expired 20 seconds ago
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  @story-wm-63gg0.1
  Scenario: An assertion issued 20 seconds ahead of the instance's clock is accepted
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" issued 20 seconds in the future
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  # --- Every refusal says which check refused it ---

  @story-wm-63gg0.1
  Scenario Outline: An assertion the instance cannot trust is refused with the reason why
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" <flaw>
    Then the sign-in is refused
    And the refusal names the reason "<reason>"
    And "ops@harborlegal.example" holds no session on the instance

    Examples:
      | flaw                                                    | reason    |
      | signed with a key the issuer has never published        | signature |
      | naming "https://door.cedarrealty.example" as its issuer | iss       |
      | naming the audience "9f8e7d6c"                          | aud       |
      | minted to live ten minutes                              | window    |
      | whose email address is not marked verified              | claims    |
      | carrying no subject claim                               | claims    |
      | naming the sign-in provider "okta"                      | claims    |

  @story-wm-63gg0.1
  Scenario: An assertion 45 seconds past its expiry is outside the clock allowance
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" that expired 45 seconds ago
    Then the sign-in is refused
    And the refusal names the reason "expired"

  @story-wm-63gg0.1
  Scenario Outline: An assertion that is not signed as the issuer signs is refused
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" <shape>
    Then the sign-in is refused
    And the refusal names the reason "signature"

    Examples:
      | shape                                                   |
      | signed with HMAC over the issuer's published public key |
      | carrying no signature at all                            |

  @story-wm-63gg0.1
  Scenario Outline: A request that carries no usable assertion is refused rather than failing
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    When someone sends a sign-in request <body>
    Then the sign-in is refused
    And the refusal names one of the contract's reasons
    And the instance does not report a failure of its own

    Examples:
      | body                                 |
      | carrying the assertion "not-a-token" |
      | carrying an empty assertion          |
      | with no assertion in it at all       |

  # --- An assertion is good for one sign-in ---

  @story-wm-63gg0.1
  Scenario: An assertion presented a second time is refused as a replay
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    And the door has signed one assertion for "ops@harborlegal.example"
    When the door presents that assertion
    And the door presents the same assertion again
    Then the first sign-in succeeds
    And the second sign-in is refused
    And the refusal names the reason "jti-replay"

  @story-wm-63gg0.1
  Scenario: Two sign-ins racing with one assertion leave a single session
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    And the door has signed one assertion for "ops@harborlegal.example"
    When two sign-ins present that assertion at the same moment
    Then exactly one of the two sign-ins succeeds
    And the other sign-in is refused
    And the refusal names the reason "jti-replay"

  @story-wm-63gg0.1
  Scenario: A person who signed in a moment ago signs in again with a fresh assertion
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    And "ops@harborlegal.example" signed in with an assertion a moment ago
    When the door presents a freshly signed assertion for "ops@harborlegal.example"
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  # --- The issuer's key list ---

  @story-wm-63gg0.1
  Scenario: A key the issuer has just published is accepted after one refetch
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has the issuer's key "door-2026-09" in hand
    And the issuer now also publishes the signing key "door-2026-10"
    When the door presents an assertion for "ops@harborlegal.example" signed with "door-2026-10"
    Then the sign-in succeeds
    And the instance read the issuer's key list again before answering

  @story-wm-63gg0.1
  Scenario: An assertion naming a key the issuer does not publish is refused as a key miss
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has the issuer's key "door-2026-09" in hand
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" signed with "door-2026-11"
    Then the sign-in is refused
    And the refusal names the reason "kid-miss"
    And the instance read the issuer's key list again before answering

  @story-wm-63gg0.1
  Scenario: A refetch that comes back empty leaves the cached keys in place
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has the issuer's key "door-2026-09" in hand
    And the issuer has since stopped publishing any key at all
    When the door presents an assertion signed with the unpublished key "door-2026-11"
    And the door then presents an assertion for "ops@harborlegal.example" signed with "door-2026-09"
    Then the first sign-in is refused
    And the refusal names the reason "kid-miss"
    And the second sign-in succeeds

  @story-wm-63gg0.1
  Scenario: A key list that cannot be reached does not stop an assertion signed with a cached key
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the instance has the issuer's key "door-2026-09" in hand
    And the issuer's key list has since become unreachable
    When the door presents an assertion for "ops@harborlegal.example" signed with "door-2026-09"
    Then the sign-in succeeds
    And "ops@harborlegal.example" holds an authenticated session

  @story-wm-63gg0.1
  Scenario: Two sign-ins a minute apart read the issuer's key list once
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" signed with "door-2026-09"
    And the door presents a second assertion for "ops@harborlegal.example" a minute later
    Then both sign-ins succeed
    And the instance read the issuer's key list once

  # --- What the instance writes down ---

  @story-wm-63gg0.1
  Scenario: A refused assertion is recorded by its reason and not by its token
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" signed with a key the issuer has never published
    Then the sign-in is refused
    And the instance's log records the reason "signature"
    And nothing the instance logged contains the assertion

  @story-wm-63gg0.1
  Scenario: An accepted assertion is never written to the instance's log
    Given a WeOS instance that trusts login assertions from "https://money.weos.cloud" for the audience "a1b2c3d4"
    And the issuer publishes only the signing key "door-2026-09"
    When the door presents an assertion for "ops@harborlegal.example" signed with "door-2026-09"
    Then the sign-in succeeds
    And nothing the instance logged contains the assertion

  # --- An instance outside the fleet has no assertion path ---

  @story-wm-63gg0.1
  Scenario: An instance with no trusted issuer configured answers as though the path never existed
    Given a WeOS instance with no trusted issuer configured
    When someone presents an assertion to that instance
    And someone posts the same details to "/api/auth/enroll", an endpoint this instance has never had
    Then both requests are answered with the same status and the same body

  @story-wm-63gg0.1
  Scenario: An instance missing one trusted-issuer setting mounts nothing and names what is missing
    Given a WeOS instance configured with a trusted issuer and its key list but no audience
    When the instance starts
    Then the instance warns that the trusted issuer's audience is missing
    And someone presenting an assertion is answered as though the path never existed
