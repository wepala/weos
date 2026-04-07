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

import "time"

// OAuthClient stores dynamically registered MCP clients (RFC 7591).
type OAuthClient struct {
	ClientID     string `gorm:"primaryKey;type:varchar(255)"`
	ClientSecret string `gorm:"type:varchar(255)"` // bcrypt hash; empty for public clients
	ClientName   string `gorm:"type:varchar(255);not null"`
	// JSON arrays. Defaults are populated by the registration handler
	// (not via GORM defaults, since SQL string literals can't represent
	// JSON arrays without escaping issues across drivers).
	RedirectURIs            string `gorm:"type:text;not null"`
	GrantTypes              string `gorm:"type:text;not null"`
	ResponseTypes           string `gorm:"type:text;not null"`
	TokenEndpointAuthMethod string `gorm:"type:varchar(50);not null;default:'none'"`
	Scope                   string `gorm:"type:varchar(500)"`
	CreatedAt               time.Time
}

func (OAuthClient) TableName() string { return "oauth_clients" }

// OAuthAuthorizationCode stores pending and issued authorization codes.
type OAuthAuthorizationCode struct {
	Code                string `gorm:"primaryKey;type:varchar(255)"`
	ClientID            string `gorm:"type:varchar(255);not null;index"`
	AgentID             string `gorm:"type:varchar(255)"` // set after identity resolution
	AccountID           string `gorm:"type:varchar(255)"`
	RedirectURI         string `gorm:"type:text;not null"`
	CodeChallenge       string `gorm:"type:varchar(255);not null"`
	CodeChallengeMethod string `gorm:"type:varchar(10);not null;default:'S256'"`
	Scope               string `gorm:"type:varchar(500)"`
	State               string `gorm:"type:varchar(255)"`                           // MCP client's state param
	Status              string `gorm:"type:varchar(20);not null;default:'pending'"` // pending/issued/exchanged
	ExpiresAt           time.Time
	CreatedAt           time.Time
}

func (OAuthAuthorizationCode) TableName() string { return "oauth_authorization_codes" }

// OAuthRefreshToken stores refresh tokens with revocation support.
type OAuthRefreshToken struct {
	ID        string `gorm:"primaryKey;type:varchar(255)"`
	TokenHash string `gorm:"type:varchar(255);not null;uniqueIndex"` // SHA-256 of token
	AgentID   string `gorm:"type:varchar(255);not null;index"`
	AccountID string `gorm:"type:varchar(255)"`
	ClientID  string `gorm:"type:varchar(255);not null"`
	Scope     string `gorm:"type:varchar(500)"`
	ExpiresAt time.Time
	Revoked   bool `gorm:"not null;default:false"`
	CreatedAt time.Time
}

func (OAuthRefreshToken) TableName() string { return "oauth_refresh_tokens" }
