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

package trustedissuer

import (
	"errors"
	"net/url"
	"strings"
)

// CheckIssuerURL reports whether a trusted issuer's address can serve as both
// the iss an assertion is compared with and the base of the door's sign-in
// address, which is the address with /door/start appended. It must be an
// absolute https URL, or plain http to a loopback host as for the key list,
// with a host, and with no user information, query or fragment: any of those
// would put /door/start somewhere other than the door's path, and user
// information would publish a credential in the providers list.
//
// Give it the issuer with its trailing slashes trimmed
// (config.TrustedIssuerConfig.IssuerID); the errors never repeat the value.
func CheckIssuerURL(raw string) error {
	if strings.ContainsAny(raw, "?#") {
		return errors.New("the trusted issuer carries a query or a fragment, so the door's sign-in address cannot be built from it")
	}
	// The URL parser lets a space through in a path; an issuer holding one is a
	// paste error, and would be published as a broken sign-in address.
	if strings.ContainsAny(raw, " \t\r\n") {
		return errors.New("the trusted issuer contains white space")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return errors.New("the trusted issuer is not an absolute URL with a host")
	}
	if u.User != nil {
		return errors.New("the trusted issuer carries user information")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
	}
	return errors.New("the trusted issuer must use https (plain http is accepted only for a loopback host)")
}
