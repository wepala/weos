// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"context"
	"fmt"

	weosentities "github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
)

// RefreshTokenRevoker ends every refresh token one person holds — a
// connector's and a native app session's alike, in every family, client and
// account — and answers how many it ended.
// internal/oauth's refresh token repository is the implementation.
type RefreshTokenRevoker interface {
	RevokeAllForAgent(ctx context.Context, agentID string) (int64, error)
}

// BrowserSessionRevoker ends every browser session one person holds, so no
// cookie of theirs authenticates anybody again. pericarp's
// AuthenticationService is the implementation: its sessions are stored rows,
// and ValidateSession refuses one that is no longer active.
//
// Declared here, where it is used, and narrowed to the one method — the
// revocation needs nothing else of an authentication service.
type BrowserSessionRevoker interface {
	RevokeAllSessions(ctx context.Context, agentID string) error
}

// AuthorizationCodeVoider spends every authorization code one person holds
// that nobody has redeemed yet, so none of them can still be exchanged for a
// token, and answers how many it voided. internal/oauth's authorization code
// repository is the implementation.
type AuthorizationCodeVoider interface {
	VoidUnredeemedForAgent(ctx context.Context, agentID string) (int64, error)
}

// AssertedTokenRevocationConfig wires an AssertedTokenRevocation.
type AssertedTokenRevocationConfig struct {
	// Credentials resolves the asserted (provider, subject) to the person
	// holding it, the way a returning sign-in resolves it.
	Credentials authrepos.CredentialRepository
	// Sessions is the browser half: without it a live cookie mints a fresh
	// authorization code seconds after the tokens are ended, and the
	// revocation undoes itself. Optional only so a caller can wire the token
	// half alone in a test; serve always supplies it.
	Sessions BrowserSessionRevoker
	// Codes voids the authorization codes already handed out and not yet
	// redeemed. Without it a code minted before the revocation still
	// exchanges for a fresh 30-day refresh token after it. Optional on the
	// same terms as Sessions.
	Codes AuthorizationCodeVoider
	// Tokens is what the revocation ends.
	Tokens RefreshTokenRevoker
	// Logger receives one line per revocation: what was revoked, or that
	// nobody here held the identity. Optional; without it nothing is logged.
	Logger weosentities.Logger
}

// AssertedTokenRevocation ends the token access of the person a trusted
// issuer's accepted assertion names. The door calls it at the end of a
// successful password reset: the person's password has changed, so every
// refresh token issued under the old one must stop renewing (bead wm-fcpzx).
//
// It reaches exactly one person: the one holding a credential for the asserted
// (provider, subject). It never resolves by email. A token issued under the
// door's password was issued through a door sign-in, and every door sign-in
// leaves a credential for the door identity on the person it signed in (it
// links one or creates one), so the identity alone reaches every token the
// reset is about. An identity this instance has never seen means the old
// password never signed anybody in here, and there is nothing to revoke. A
// fallback by email would reach further than that — on an instance with no
// OAUTH_ALLOWED_EMAILS, to a person the issuer never signed in — and would buy
// the door nothing it needs.
//
// It creates nobody and links nothing.
//
// It ends three things for that person, in this order: every browser session,
// every authorization code nobody has redeemed yet, and every refresh token.
// The order is the whole of the fix: a session left alive mints a fresh
// authorization code with no re-authentication, and that code exchanges for a
// new 30-day refresh token, so ending the tokens first would undo itself
// within seconds. Sessions first closes the mint; voiding the codes next
// closes the ones already handed out; the tokens are ended last, when nothing
// can issue another.
//
// It does not shorten an access token: those are stateless and last their
// hour, so revocation stops renewal, not the current hour.
type AssertedTokenRevocation struct {
	cfg AssertedTokenRevocationConfig
}

// NewAssertedTokenRevocation builds an AssertedTokenRevocation.
func NewAssertedTokenRevocation(cfg AssertedTokenRevocationConfig) *AssertedTokenRevocation {
	if cfg.Logger == nil {
		cfg.Logger = discardSignInLogs{}
	}
	return &AssertedTokenRevocation{cfg: cfg}
}

// TokenRevocationResult is what a revocation ended: the people it reached, how
// many refresh tokens it revoked across them, and how many unredeemed
// authorization codes it voided. All three are for the instance's own log —
// the route's answer says none of them, so a caller cannot learn from it
// whether an address has an account here (see TokenRevocationHandler).
type TokenRevocationResult struct {
	People []string
	Tokens int64
	Codes  int64
}

// Revoke ends every refresh token of the person the identity names. An
// identity nobody here holds revokes nothing and is not an error: the door
// resets a password for a person who may never have reached this instance.
//
// It is idempotent: a second call revokes nothing more and answers the same.
func (s *AssertedTokenRevocation) Revoke(
	ctx context.Context, id AssertedIdentity,
) (TokenRevocationResult, error) {
	email := repositories.FoldCredentialEmail(id.Email)
	people, err := s.peopleFor(ctx, id)
	if err != nil {
		return TokenRevocationResult{}, err
	}
	if len(people) == 0 {
		s.cfg.Logger.Info(ctx,
			"trusted issuer token revocation: nobody on this instance holds the asserted identity, so no token was revoked",
			identityFields(id, email)...)
		return TokenRevocationResult{}, nil
	}

	result := TokenRevocationResult{People: people}
	for _, agentID := range people {
		// The browser session first. While one is alive the person needs no
		// credential at all to start again: GET /oauth/authorize takes the
		// cookie, mints an authorization code with no re-authentication, and
		// POST /oauth/token exchanges it for a fresh 30-day refresh token. A
		// revocation that ended the tokens and left the cookie would be undone
		// by one request.
		if s.cfg.Sessions != nil {
			if err := s.cfg.Sessions.RevokeAllSessions(ctx, agentID); err != nil {
				return result, s.failed(ctx, id, email, people, agentID, result,
					"a person's browser sessions were not all ended, so a live cookie can still mint new token access; ask again",
					"end the browser sessions", err)
			}
		}
		// Then the codes already handed out. One issued before the revocation
		// is still redeemable after it — the token endpoint checks the code's
		// status, membership and the agent, and nothing about a revocation.
		if s.cfg.Codes != nil {
			voided, err := s.cfg.Codes.VoidUnredeemedForAgent(ctx, agentID)
			result.Codes += voided
			if err != nil {
				return result, s.failed(ctx, id, email, people, agentID, result,
					"a person's unredeemed authorization codes were not all voided, so one already handed out can still be exchanged for a token; ask again",
					"void the authorization codes", err)
			}
		}
		revoked, err := s.cfg.Tokens.RevokeAllForAgent(ctx, agentID)
		result.Tokens += revoked
		if err != nil {
			return result, s.failed(ctx, id, email, people, agentID, result,
				"a person's refresh tokens were not all revoked; ask again",
				"revoke the refresh tokens", err)
		}
	}
	s.cfg.Logger.Info(ctx,
		"trusted issuer token revocation: every refresh token of the person is revoked and every browser session of theirs is ended, so no connector, app session or cookie of theirs renews or mints access again",
		append(identityFields(id, email, people...),
			"revoked", result.Tokens, "codes_voided", result.Codes)...)
	return result, nil
}

// failed logs a step that did not finish and names it in the error the door is
// answered 503 from. Said out loud, and answered as a failure, because a
// revocation that silently did not finish leaves an intruder renewing. The
// door asks again, and every step is idempotent, so it may.
func (s *AssertedTokenRevocation) failed(
	ctx context.Context,
	id AssertedIdentity,
	email string,
	people []string,
	agentID string,
	result TokenRevocationResult,
	message, step string,
	err error,
) error {
	s.cfg.Logger.Error(ctx,
		"trusted issuer token revocation: "+message,
		append(identityFields(id, email, people...),
			"agent_id", agentID, "revoked", result.Tokens,
			"codes_voided", result.Codes, "error", err.Error())...)
	return fmt.Errorf("%s of %s: %w", step, agentID, err)
}

// peopleFor is whom the revocation reaches: the holder of the asserted
// identity, or nobody. It never creates anybody and never links an identity,
// so a revocation for an identity nobody holds leaves the instance exactly as
// it was.
func (s *AssertedTokenRevocation) peopleFor(ctx context.Context, id AssertedIdentity) ([]string, error) {
	credential, err := s.cfg.Credentials.FindByProvider(ctx, id.Provider, id.Subject)
	if err != nil {
		return nil, fmt.Errorf("look up the credential for provider %s: %w", id.Provider, err)
	}
	if credential == nil || credential.AgentID() == "" {
		return nil, nil
	}
	return []string{credential.AgentID()}, nil
}
