@wip @story-wm-kb6sg-2
Feature: Every upload lands in its account's own folder
  As an account holder
  I want a photo I upload to be stored under my own account's folder
  So that no other account can read it by its URL

  An upload has had no owner. The store wrote every file under one flat prefix, and the read
  route was a plain file server that any signed-in person with a URL could read, whatever
  account they were in. This feature gives an upload an owner and makes the owner the one
  thing that lets it be read.

  The account is the one a sign-in already resolved for the caller — the identity the session
  or bearer middleware put on the request. It is never taken from a header, a form field or a
  query parameter, because those are supplied by whoever is calling and would let a caller
  claim another account's folder. A caller with no account cannot upload at all.

  The read route answers a file outside the caller's own folder with the same 404 it gives a
  file that was never uploaded — not a 403. A 403 would confirm the file exists to someone who
  may not read it; a 404 tells a probe nothing it did not already know. The account is read
  from the decoded, cleaned path, so an encoded "../" that resolves into another account's
  folder is answered the same way rather than escaping the check.

  Two decisions here are written on purpose rather than left to fall out of the code. There is
  no cross-account file viewing at launch: a person granted a resource that belongs to another
  account can read the resource, but the photo it carries is answered 404, because file reads
  do not consult the resource's cross-agent grants. And files uploaded before this change keep
  serving at their old flat URLs — they cannot be attributed to an account, so moving them is
  follow-up work, not part of this story.

  The stored object key carries the account, so the key shape per backend (GCS, S3 and local)
  is part of the contract. Cloud backends are not reachable from this suite, which runs against
  local storage, so their key shape is pinned by unit tests named in the story's report; the
  scenarios below assert the shape the local backend and the returned URL make observable.

  Scenario: A photo is stored under the uploading account's own folder
    Given a WeOS instance where requests are authenticated by their session
    And "Harbor Legal" is signed in
    When "Harbor Legal" sends a photo named "lasagna.jpg"
    Then the upload succeeds
    And the stored file's location is under the folder for "Harbor Legal"
    And the file's URL is under the folder for "Harbor Legal"

  Scenario: An upload with no account behind it is refused
    Given a WeOS instance where requests are authenticated by their session
    When someone carrying no session sends a photo named "lasagna.jpg"
    Then the request is refused as not authenticated
    And nothing is stored

  Scenario: The account that uploaded a photo can read it back
    Given a WeOS instance where requests are authenticated by their session
    And "Harbor Legal" has a stored photo named "lasagna.jpg"
    When "Harbor Legal" requests that photo by its URL
    Then the photo is served

  Scenario: Another account cannot read a photo by its URL
    Given a WeOS instance where requests are authenticated by their session
    And "Harbor Legal" has a stored photo named "lasagna.jpg"
    And "Cedar Realty" is signed in
    When "Cedar Realty" requests "Harbor Legal"'s photo by its URL
    Then the request is refused as not found

  Scenario: A file that was never uploaded is answered the same as another account's file
    Given a WeOS instance where requests are authenticated by their session
    And "Harbor Legal" has a stored photo named "lasagna.jpg"
    And "Cedar Realty" is signed in
    When "Cedar Realty" requests "Harbor Legal"'s photo by its URL
    And "Cedar Realty" requests a photo at a URL in its own folder that names no stored file
    Then both requests are answered with the same status and the same body

  Scenario: A photo on a resource shared from another account is not readable by its URL
    Given a WeOS instance where requests are authenticated by their session
    And the meal-planning preset is installed
    And "Harbor Legal" has a recipe "Sunday Lasagna" carrying a stored photo named "lasagna.jpg"
    And "Harbor Legal" has granted "Cedar Realty" read access to the recipe "Sunday Lasagna"
    When "Cedar Realty" reads the recipe "Sunday Lasagna"
    And "Cedar Realty" requests the recipe's photo by its URL
    Then the recipe "Sunday Lasagna" is read
    And the request for its photo is refused as not found

  Scenario Outline: An encoded path that escapes the caller's folder cannot reach another account's file
    Given a WeOS instance where requests are authenticated by their session
    And "Harbor Legal" has a stored photo named "lasagna.jpg"
    And "Cedar Realty" is signed in
    When "Cedar Realty" requests a file below its own folder whose path escapes it as "<escape>"
    Then the request is refused as not found

    Examples:
      | escape       |
      | ../          |
      | %2e%2e%2f    |
      | ..%2f        |
      | %2e%2e/      |

  Scenario: A file uploaded before account folders existed still serves at its old URL
    Given a WeOS instance where requests are authenticated by their session
    And a photo "old-menu.jpg" was stored at its flat URL before files were kept by account
    And "Harbor Legal" is signed in
    When "Harbor Legal" requests "old-menu.jpg" at its flat URL
    Then the photo is served
