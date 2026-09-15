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
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"time"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
)

// NativeRefreshGraceWindow is how long after a native refresh token is spent an
// exact repeat of it still gets the successor that renewal made (wm-3dgs0). An
// app whose renewal answer was lost on the network retries with the token it
// still holds, and two requests that renew at once send it twice. Thirty
// seconds covers a mobile retry and a race, and is short enough that a copy
// taken from a device is still caught as reuse: a repeat after the window, or a
// repeat of a token whose successor was already spent, revokes the family.
const NativeRefreshGraceWindow = 30 * time.Second

// nativeSuccessorKeyLabel keeps the successor key apart from any other use of
// the signing key's bytes.
const nativeSuccessorKeyLabel = "weos native refresh token successor key v1"

// NativeRefreshSuccessorKey is the key a native refresh token's successor is
// derived with, so a repeat inside the grace window can be answered the same
// successor while the store never keeps a raw refresh token. It is derived from
// the instance's signing key when svc is the service ProvideJWTService built,
// so it is the same across a restart and on every process that shares
// JWT_SIGNING_KEY. Otherwise it is random for this process, and a repeat that
// reaches another process is reuse, as it was before the grace window existed.
func NativeRefreshSuccessorKey(svc authapp.JWTService) []byte {
	if instance, ok := svc.(*instanceJWTService); ok && instance != nil && len(instance.successorKey) > 0 {
		return append([]byte(nil), instance.successorKey...)
	}
	key := make([]byte, sha256.Size)
	if _, err := rand.Read(key); err != nil {
		// With no key there is no grace window: a successor is random and a
		// repeat is reuse, which is safe, only less forgiving.
		return nil
	}
	return key
}

// nativeSuccessorKeyFor derives the successor key from the instance's signing
// key.
func nativeSuccessorKeyFor(key *rsa.PrivateKey) []byte {
	if key == nil {
		return nil
	}
	mac := hmac.New(sha256.New, x509.MarshalPKCS1PrivateKey(key))
	// A hash's Write never returns an error.
	_, _ = mac.Write([]byte(nativeSuccessorKeyLabel))
	return mac.Sum(nil)
}

// nativeSuccessorRaw is the raw successor of the native refresh token whose row
// id is spentID and whose raw value is presentedRaw: an HMAC of both under the
// successor key. Computing it needs the presented token and the key, and the
// store keeps only the successor's hash.
func nativeSuccessorRaw(key []byte, spentID, presentedRaw string) string {
	mac := hmac.New(sha256.New, key)
	// A hash's Write never returns an error.
	_, _ = mac.Write([]byte(spentID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(presentedRaw))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// NativeRefreshSuccessorInGrace answers the successor of spent, a native refresh
// token presented again as presentedRaw, when the repeat is inside the grace
// window: spent was rotated no more than window before now, and the successor
// that rotation made is still live — not revoked, spent or expired. A family
// that was revoked, and a successor that was itself spent, are never answered.
// ok is false, with no error, whenever the repeat is not inside the window. A
// window of zero or less, or no successor key, turns the grace window off.
func NativeRefreshSuccessorInGrace(
	ctx context.Context, repo RefreshTokenRepository, spent *OAuthRefreshToken, presentedRaw string,
	successorKey []byte, now time.Time, window time.Duration,
) (successor *OAuthRefreshToken, refresh NativeRefreshToken, ok bool, err error) {
	if window <= 0 || len(successorKey) == 0 || !IsNativeRefreshToken(spent) || !spent.Revoked ||
		spent.SuccessorID == "" || spent.RotatedAt == nil || now.Sub(*spent.RotatedAt) > window {
		return nil, NativeRefreshToken{}, false, nil
	}
	raw := nativeSuccessorRaw(successorKey, spent.ID, presentedRaw)
	row, err := repo.FindByTokenHash(ctx, HashToken(raw))
	if errors.Is(err, ErrNotFound) {
		return nil, NativeRefreshToken{}, false, nil
	}
	if err != nil {
		return nil, NativeRefreshToken{}, false, err
	}
	if row.ID != spent.SuccessorID || row.FamilyID != spent.FamilyID || !IsNativeRefreshToken(row) ||
		row.Revoked || !now.Before(row.ExpiresAt) {
		return nil, NativeRefreshToken{}, false, nil
	}
	return row, NativeRefreshToken{Raw: raw, ExpiresAt: row.ExpiresAt}, true, nil
}
