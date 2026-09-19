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

// AssertedTokenRevocationConfig wires an AssertedTokenRevocation.
type AssertedTokenRevocationConfig struct {
	// Credentials resolves the asserted (provider, subject) to the person
	// holding it, the way a returning sign-in resolves it.
	Credentials authrepos.CredentialRepository
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
// It does not end browser sessions. Those are signed cookies with no store to
// revoke, so an instance-wide SESSION_SECRET rotation is still the only lever
// on them, and it is the reason this call exists: token access had no lever at
// all. It does not shorten an access token either: those are stateless and
// last their hour, so revocation stops renewal, not the current hour.
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

// TokenRevocationResult is what a revocation ended: the people it reached, and
// how many refresh tokens it revoked across them. Both are for the instance's
// own log — the route's answer says neither, so a caller cannot learn from it
// whether an address has an account here (see TokenRevocationHandler).
type TokenRevocationResult struct {
	People []string
	Tokens int64
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
		revoked, err := s.cfg.Tokens.RevokeAllForAgent(ctx, agentID)
		result.Tokens += revoked
		if err != nil {
			// Said out loud, and answered as a failure to the door, because a
			// revocation that silently did not finish leaves an intruder
			// renewing. The door asks again.
			s.cfg.Logger.Error(ctx,
				"trusted issuer token revocation: a person's refresh tokens were not all revoked; ask again",
				append(identityFields(id, email, people...),
					"agent_id", agentID, "revoked", result.Tokens, "error", err.Error())...)
			return result, fmt.Errorf("revoke the refresh tokens of %s: %w", agentID, err)
		}
	}
	s.cfg.Logger.Info(ctx,
		"trusted issuer token revocation: every refresh token of the person is revoked, so no connector or app session of theirs renews again",
		append(identityFields(id, email, people...), "revoked", result.Tokens)...)
	return result, nil
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
