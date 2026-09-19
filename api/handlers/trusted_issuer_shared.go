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

package handlers

import (
	"errors"
	"net/http"

	"github.com/wepala/weos/v3/internal/trustedissuer"
)

// What the two routes that take a trusted issuer's assertion — POST
// /auth/assert and POST /auth/revoke-tokens — do the same way. One copy, so
// the second route cannot drift into checking less than the first.

// originSet is the set of origins a request may carry, built from addresses.
// An address that names no origin is left out rather than admitted.
func originSet(addresses []string) map[string]struct{} {
	origins := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		if origin := originOf(address); origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return origins
}

// crossSiteRequest says why a request came from another site, or "" when it
// did not.
//
// An assertion is acted on for whoever posts it, so a page on another site
// that holds one for this audience could post it from a victim's browser — to
// sign that browser in as someone else (login CSRF), or to end that person's
// token access. A browser marks what it sends: Sec-Fetch-Site on every
// request, and Origin on every POST. The door serves the instance on the
// door's own origin, so a browser posts same-origin. A request with neither
// header is not a browser another site can drive — a server-side call from the
// door is one — and passes.
//
// The detail is a fixed phrase plus, at most, the normalized origin: no other
// header text reaches the log.
func crossSiteRequest(r *http.Request, origins map[string]struct{}) string {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		switch site {
		case "cross-site", "same-site", "none":
			return "the browser marks the request " + site + ", not same-origin"
		default:
			return "the request's Sec-Fetch-Site is not same-origin"
		}
	}
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return ""
	}
	if len(values) > 1 {
		return "the request carries more than one Origin"
	}
	origin := originOf(values[0])
	if origin == "" {
		return "the request's Origin is not an http or https origin"
	}
	if _, ok := origins[origin]; !ok {
		return "the request's Origin " + origin + " is neither this instance's nor the trusted issuer's"
	}
	return ""
}

// refusalOf is the reason and detail an assertion was refused with. An error
// that is not a *trustedissuer.Refusal — which the verifier never returns —
// is reported as a signature refusal, the least specific reason there is, so
// an unexpected error can never be read as an accepted assertion.
func refusalOf(err error) (trustedissuer.Reason, string) {
	var refusal *trustedissuer.Refusal
	if errors.As(err, &refusal) {
		return refusal.Reason, refusal.Detail
	}
	return trustedissuer.ReasonSignature, "the assertion could not be verified"
}
