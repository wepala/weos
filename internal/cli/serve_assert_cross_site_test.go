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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/internal/config"
)

// serveCallWithHeaders is serveCall with request headers.
func serveCallWithHeaders(t *testing.T, srv *httptest.Server, path, body string, headers map[string]string) serveAnswer {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build POST %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read POST %s: %v", path, err)
	}
	return serveAnswer{status: resp.StatusCode, body: strings.TrimSpace(string(raw)), cookies: resp.Cookies()}
}

// serve wires the assertion route's allowed origins: the trusted issuer's and
// the instance's BASE_URL. A browser on another site is refused before the
// assertion is read, so the same assertion still signs its person in when it
// arrives from the door.
func TestServe_AssertionPostedFromAnotherSiteIsRefusedBeforeItIsRead(t *testing.T) {
	door := newBootDoor(t)
	cfg := config.Default()
	cfg.SessionSecret = bootOwnSecret
	cfg.TrustedIssuer = door.settings()
	cfg.OAuth.BaseURL = "https://dana.money.weos.cloud/"
	srv := bootServe(t, cfg)

	for name, headers := range map[string]map[string]string{
		"a page on another site":         {"Origin": "https://attacker.example", "Sec-Fetch-Site": "cross-site"},
		"another origin, no fetch marks": {"Origin": "https://attacker.example"},
	} {
		t.Run(name, func(t *testing.T) {
			body := door.assertionBody(t, bootOwnerEmail)
			refused := serveCallWithHeaders(t, srv, "/api/auth/assert", body, headers)
			if refused.status != http.StatusForbidden || len(refused.cookies) != 0 {
				t.Fatalf("a cross-site assertion answered %d %s with %d cookies, want 403 and none",
					refused.status, refused.body, len(refused.cookies))
			}
			var answer struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal([]byte(refused.body), &answer); err != nil || answer.Code != "cross-site" {
				t.Fatalf("the refusal's code is %q (%v), want cross-site: %s", answer.Code, err, refused.body)
			}
			// The refused request never reached the verifier, so the jti is
			// unspent and the door's own page can still use the assertion.
			same := serveCallWithHeaders(t, srv, "/api/auth/assert", body,
				map[string]string{"Origin": bootDoorIssuer, "Sec-Fetch-Site": "same-origin"})
			if same.status != http.StatusOK {
				t.Fatalf("the same assertion from the door's page answered %d %s, want 200", same.status, same.body)
			}
		})
	}

	own := serveCallWithHeaders(t, srv, "/api/auth/assert", door.assertionBody(t, bootOwnerEmail),
		map[string]string{"Origin": "https://dana.money.weos.cloud", "Sec-Fetch-Site": "same-origin"})
	if own.status != http.StatusOK {
		t.Fatalf("an assertion from the instance's own page answered %d %s, want 200", own.status, own.body)
	}
}
