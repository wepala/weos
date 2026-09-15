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

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/segmentio/ksuid"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("oauth: not found")

// Authorization code status constants.
const (
	StatusPending   = "pending"
	StatusIssued    = "issued"
	StatusExchanged = "exchanged"
)

// --- Client Repository ---

type ClientRepository interface {
	Create(ctx context.Context, client *OAuthClient) error
	FindByID(ctx context.Context, clientID string) (*OAuthClient, error)
}

type gormClientRepo struct{ db *gorm.DB }

func NewClientRepository(db *gorm.DB) ClientRepository {
	return &gormClientRepo{db: db}
}

func (r *gormClientRepo) Create(ctx context.Context, client *OAuthClient) error {
	if client.ClientID == "" {
		client.ClientID = ksuid.New().String()
	}
	return r.db.WithContext(ctx).Create(client).Error
}

func (r *gormClientRepo) FindByID(ctx context.Context, clientID string) (*OAuthClient, error) {
	var c OAuthClient
	err := r.db.WithContext(ctx).Where("client_id = ?", clientID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &c, err
}

// --- Authorization Code Repository ---

type AuthCodeRepository interface {
	Create(ctx context.Context, code *OAuthAuthorizationCode) error
	FindByCode(ctx context.Context, code string) (*OAuthAuthorizationCode, error)
	MarkExchanged(ctx context.Context, code string) error
	UpdateIdentity(ctx context.Context, code, agentID, accountID string) error
}

type gormAuthCodeRepo struct{ db *gorm.DB }

func NewAuthCodeRepository(db *gorm.DB) AuthCodeRepository {
	return &gormAuthCodeRepo{db: db}
}

func (r *gormAuthCodeRepo) Create(ctx context.Context, code *OAuthAuthorizationCode) error {
	if code.Code == "" {
		c, err := generateRandomCode()
		if err != nil {
			return err
		}
		code.Code = c
	}
	if code.ExpiresAt.IsZero() {
		code.ExpiresAt = time.Now().Add(10 * time.Minute)
	}
	return r.db.WithContext(ctx).Create(code).Error
}

func (r *gormAuthCodeRepo) FindByCode(ctx context.Context, code string) (*OAuthAuthorizationCode, error) {
	var c OAuthAuthorizationCode
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (r *gormAuthCodeRepo) MarkExchanged(ctx context.Context, code string) error {
	result := r.db.WithContext(ctx).
		Model(&OAuthAuthorizationCode{}).
		Where("code = ? AND status = ? AND expires_at > ?", code, StatusIssued, time.Now()).
		Update("status", StatusExchanged)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *gormAuthCodeRepo) UpdateIdentity(
	ctx context.Context, code, agentID, accountID string,
) error {
	result := r.db.WithContext(ctx).
		Model(&OAuthAuthorizationCode{}).
		Where("code = ? AND status = ? AND expires_at > ?",
			code, StatusPending, time.Now()).
		Updates(map[string]any{
			"agent_id":   agentID,
			"account_id": accountID,
			"status":     StatusIssued,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Refresh Token Repository ---

type RefreshTokenRepository interface {
	Create(ctx context.Context, token *OAuthRefreshToken, rawToken string) error
	FindByTokenHash(ctx context.Context, tokenHash string) (*OAuthRefreshToken, error)
	Revoke(ctx context.Context, id string) error
	// RevokeIfActive atomically revokes the token only if it is currently
	// not revoked. Returns ErrNotFound if the token is missing or already
	// revoked. Used for safe rotation under concurrent refresh requests.
	RevokeIfActive(ctx context.Context, id string) error
	// RevokeFamily revokes all active tokens in the given family. Used as
	// a compromise response when a previously-revoked token is presented
	// (signals theft of an earlier rotation). It returns nil only once no
	// token of the family is active, a successor a concurrent rotation
	// committed meanwhile included.
	RevokeFamily(ctx context.Context, familyID string) error
	// RevokeForAgent revokes every active token the agent holds for clientID,
	// in every account and family. A native app's sign-out uses it to end the
	// person's native sessions on every device (wm-lnimb). It returns nil
	// only once none is active, as RevokeFamily does.
	RevokeForAgent(ctx context.Context, agentID, clientID string) error
	// PurgeExpired deletes the tokens held for clientID that expired before
	// before, revoked or not, and answers how many (wm-sa7wv; see
	// NativeRefreshTokenPurger).
	PurgeExpired(ctx context.Context, clientID string, before time.Time) (int64, error)
	// Rotate atomically revokes the old token (only if active) and creates
	// the new token in a single transaction. If the old token is already
	// revoked, or has expired by the clock read inside that transaction,
	// returns ErrNotFound. If the new token cannot be created, the old token
	// is NOT revoked (transaction rollback).
	Rotate(
		ctx context.Context,
		oldID string,
		newToken *OAuthRefreshToken,
		newRawToken string,
	) error
}

type gormRefreshTokenRepo struct {
	db *gorm.DB
	// now is the clock a rotation checks the spent token's expiry against.
	now func() time.Time
}

func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &gormRefreshTokenRepo{db: db, now: time.Now}
}

func (r *gormRefreshTokenRepo) Create(
	ctx context.Context, token *OAuthRefreshToken, rawToken string,
) error {
	if token.ID == "" {
		token.ID = ksuid.New().String()
	}
	// Initial issuance: family ID equals the token's own ID. Subsequent
	// rotations inherit the same FamilyID via Rotate.
	if token.FamilyID == "" {
		token.FamilyID = token.ID
	}
	token.TokenHash = HashToken(rawToken)
	if token.ExpiresAt.IsZero() {
		token.ExpiresAt = time.Now().Add(30 * 24 * time.Hour) // 30 days
	}
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *gormRefreshTokenRepo) FindByTokenHash(
	ctx context.Context, tokenHash string,
) (*OAuthRefreshToken, error) {
	var t OAuthRefreshToken
	err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (r *gormRefreshTokenRepo) Revoke(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Model(&OAuthRefreshToken{}).
		Where("id = ?", id).
		Update("revoked", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *gormRefreshTokenRepo) RevokeFamily(ctx context.Context, familyID string) error {
	if familyID == "" {
		return nil
	}
	return r.revokeUntilNoneActive(ctx, "family_id = ?", familyID)
}

func (r *gormRefreshTokenRepo) RevokeForAgent(ctx context.Context, agentID, clientID string) error {
	if agentID == "" || clientID == "" {
		return nil
	}
	return r.revokeUntilNoneActive(ctx, "agent_id = ? AND client_id = ?", agentID, clientID)
}

// revokePasses bounds revokeUntilNoneActive. A pass after the first is needed
// only when a rotation committed a successor during the pass before.
const revokePasses = 5

// errStillRotating is what a revocation answers when rotations kept committing
// active successors through every pass.
var errStillRotating = errors.New("oauth: refresh tokens were still being rotated while they were revoked")

// revokeUntilNoneActive revokes the active tokens matching query, then counts
// them, until a count finds none. The update and the count must stay separate
// statements outside a transaction. On PostgreSQL an update that waits on the
// row a rotation is spending never sees the successor that rotation commits; a
// later statement does, and until the rotation commits, the row it spends still
// counts as active.
func (r *gormRefreshTokenRepo) revokeUntilNoneActive(ctx context.Context, query string, args ...any) error {
	db := r.db.WithContext(ctx)
	for range revokePasses {
		if err := db.Model(&OAuthRefreshToken{}).Where(query, args...).Where("revoked = ?", false).
			Update("revoked", true).Error; err != nil {
			return err
		}
		var active int64
		if err := db.Model(&OAuthRefreshToken{}).Where(query, args...).Where("revoked = ?", false).
			Count(&active).Error; err != nil {
			return err
		}
		if active == 0 {
			return nil
		}
	}
	return errStillRotating
}

func (r *gormRefreshTokenRepo) RevokeIfActive(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Model(&OAuthRefreshToken{}).
		Where("id = ? AND revoked = ?", id, false).
		Update("revoked", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *gormRefreshTokenRepo) Rotate(
	ctx context.Context,
	oldID string,
	newToken *OAuthRefreshToken,
	newRawToken string,
) error {
	if newToken.ID == "" {
		newToken.ID = ksuid.New().String()
	}
	newToken.TokenHash = HashToken(newRawToken)
	if newToken.ExpiresAt.IsZero() {
		newToken.ExpiresAt = r.now().Add(30 * 24 * time.Hour)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Conditional revoke — fails if the token is already revoked or has
		// expired. The spent token records its successor's id and when it was
		// spent, so a native renewal repeated inside the grace window can be
		// answered that successor (wm-3dgs0).
		rotatedAt := r.now()
		result := tx.Model(&OAuthRefreshToken{}).
			Where("id = ? AND revoked = ? AND expires_at > ?", oldID, false, rotatedAt).
			Updates(map[string]any{"revoked": true, "successor_id": newToken.ID, "rotated_at": rotatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		// The update can wait on the row's lock long enough for the token to
		// expire, so the clock is read again now that this transaction holds it.
		var unexpired int64
		if err := tx.Model(&OAuthRefreshToken{}).
			Where("id = ? AND expires_at > ?", oldID, r.now()).
			Count(&unexpired).Error; err != nil {
			return err
		}
		if unexpired == 0 {
			return ErrNotFound
		}
		// Persist new token in the same transaction. If this fails,
		// the revocation is rolled back automatically.
		return tx.Create(newToken).Error
	})
}

// --- Helpers ---

func generateRandomCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateRefreshToken creates a random refresh token string.
func GenerateRefreshToken() (string, error) {
	return generateRandomCode()
}

// HashToken returns the hex-encoded SHA-256 hash of a token string.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// MaskCode returns a safe-to-log identifier for a sensitive credential
// (authorization code, refresh token, etc.) — the first 8 hex chars of
// the SHA-256 hash. Never logs the raw value.
func MaskCode(code string) string {
	if code == "" {
		return ""
	}
	return HashToken(code)[:8]
}
