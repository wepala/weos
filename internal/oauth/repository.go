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
		Where("code = ? AND status = ?", code, StatusPending).
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
}

type gormRefreshTokenRepo struct{ db *gorm.DB }

func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &gormRefreshTokenRepo{db: db}
}

func (r *gormRefreshTokenRepo) Create(
	ctx context.Context, token *OAuthRefreshToken, rawToken string,
) error {
	if token.ID == "" {
		token.ID = ksuid.New().String()
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
