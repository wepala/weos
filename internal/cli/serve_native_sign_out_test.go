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

package cli

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// wm-ehtnq. A native sign-out says in its answer what it did to the app's
// session, and an app whose access token has expired can still sign out with
// it. No failure message prints a token or a refresh token.

// nativeSignOutSigningKey is a signing key for an instance whose tokens a test
// re-signs: the PEM the configuration takes, and the key.
func nativeSignOutSigningKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	pemKey := testJWTSigningKey(t)
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		t.Fatal("decode the signing key's PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse the signing key: %v", err)
	}
	return pemKey, key
}

// expiredCopyOfNativeToken re-signs token's claims with key, expired a minute
// ago: the token an app holds once its hour has passed.
func expiredCopyOfNativeToken(t *testing.T, token string, key *rsa.PrivateKey) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the access token has %d parts, want 3", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode the access token's claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("read the access token's claims: %v", err)
	}
	now := time.Now()
	claims["iat"] = now.Add(-2 * time.Hour).Unix()
	claims["nbf"] = now.Add(-2 * time.Hour).Unix()
	claims["exp"] = now.Add(-time.Minute).Unix()
	signed, err := gojwt.NewWithClaims(gojwt.SigningMethodRS256, gojwt.MapClaims(claims)).SignedString(key)
	if err != nil {
		t.Fatalf("re-sign the access token: %v", err)
	}
	return signed
}

// nativeSignOutAppSession reads what a sign-out's answer says about the app's
// session, and its code.
func nativeSignOutAppSession(t *testing.T, answer serveAnswer) (appSession, code string) {
	t.Helper()
	var body struct {
		AppSession string `json:"app_session"`
		Code       string `json:"code"`
	}
	if err := json.Unmarshal([]byte(answer.body), &body); err != nil {
		t.Fatalf("decode the sign-out's answer: %v", err)
	}
	return body.AppSession, body.Code
}

func TestServe_ANativeSignOutSaysWhatItEndedAndTakesAnExpiredAccessToken(t *testing.T) {
	door := newBootDoor(t)
	pemKey, key := nativeSignOutSigningKey(t)
	cfg := trustedIssuerConfig(door)
	cfg.OAuth.JWTSigningKey = pemKey
	srv := bootServe(t, cfg)

	t.Run("a live access token ends its session and says so", func(t *testing.T) {
		phone := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		if appSession, code := nativeSignOutAppSession(t, signOutNatively(t, srv, phone.token, nil)); appSession != "ended" || code != "" {
			t.Fatalf("the sign-out says app_session %q code %q, want ended and no code", appSession, code)
		}
	})

	t.Run("an expired access token this instance signed ends its session", func(t *testing.T) {
		phone := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		tablet := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		expired := expiredCopyOfNativeToken(t, phone.token, key)
		if me := serveCallWithToken(t, srv, http.MethodGet, "/api/auth/me", expired); me.status != http.StatusUnauthorized {
			t.Fatalf("GET /api/auth/me with the expired token answered %d, want 401: the token must be expired", me.status)
		}
		if appSession, code := nativeSignOutAppSession(t, signOutNatively(t, srv, expired, nil)); appSession != "ended" || code != "" {
			t.Fatalf("the sign-out with an expired token says app_session %q code %q, want ended and no code", appSession, code)
		}
		requireRenewalRefused(t, renewNativeSession(t, srv, phone.refreshToken), "invalid_refresh_token",
			"the phone's renewal after it signed out with its expired access token")
		decodeNativeSession(t, renewNativeSession(t, srv, tablet.refreshToken), "the tablet's renewal after the phone signed out")
	})

	t.Run("an expired access token another key signed ends nothing and says so", func(t *testing.T) {
		phone := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate another key: %v", err)
		}
		forged := expiredCopyOfNativeToken(t, phone.token, otherKey)
		appSession, code := nativeSignOutAppSession(t, signOutNatively(t, srv, forged, nil))
		if appSession != "not_identified" || code != "app_session_not_identified" {
			t.Fatalf("the sign-out with a forged token says app_session %q code %q, want not_identified", appSession, code)
		}
		decodeNativeSession(t, renewNativeSession(t, srv, phone.refreshToken), "the phone's renewal after a forged sign-out")
	})

	t.Run("a browser's token names no app session and says so", func(t *testing.T) {
		browser := serveCall(t, srv, http.MethodPost, "/api/auth/assert",
			door.assertionBodyFor(t, bootOwnerEmail, bootOwnerSubject, bootOwnerName), nil)
		var signIn struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(browser.body), &signIn); err != nil || signIn.Data.Token == "" {
			t.Fatalf("the browser sign-in carries no token (decode error %v)", err)
		}
		appSession, code := nativeSignOutAppSession(t, signOutNatively(t, srv, signIn.Data.Token, nil))
		if appSession != "not_identified" || code != "app_session_not_identified" {
			t.Fatalf("the sign-out with a browser's token says app_session %q code %q, want not_identified", appSession, code)
		}
	})

	t.Run("asking everywhere with a spent refresh token ends its session and says everywhere was refused", func(t *testing.T) {
		phone := signInNativelyThroughTheDoor(t, srv, door, bootOwnerEmail, bootOwnerSubject, bootOwnerName)
		decodeNativeSession(t, renewNativeSession(t, srv, phone.refreshToken), "the phone's renewal")
		appSession, code := nativeSignOutAppSession(t,
			signOutNatively(t, srv, "", map[string]any{"refresh_token": phone.refreshToken, "everywhere": true}))
		if appSession != "ended" || code != "sign_out_everywhere_refused" {
			t.Fatalf("the sign-out says app_session %q code %q, want ended and sign_out_everywhere_refused", appSession, code)
		}
	})

	t.Run("a cookie-only sign-out answers as it always did", func(t *testing.T) {
		browser := serveCall(t, srv, http.MethodPost, "/api/auth/assert",
			door.assertionBodyFor(t, bootOwnerEmail, bootOwnerSubject, bootOwnerName), nil)
		signOut := serveCall(t, srv, http.MethodPost, "/api/auth/logout", "", browser.cookies)
		if signOut.status != http.StatusOK {
			t.Fatalf("POST /api/auth/logout with only the cookie answered %d, want 200", signOut.status)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(signOut.body), &body); err != nil {
			t.Fatalf("decode the cookie-only sign-out's answer: %v", err)
		}
		if len(body) != 1 || body["status"] != "logged out" {
			t.Fatalf("the cookie-only sign-out answered %v, want exactly {\"status\":\"logged out\"}", body)
		}
	})
}
