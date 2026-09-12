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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
)

const (
	keyListFetchTimeout = 5 * time.Second
	keyListMaxBytes     = 1 << 20
)

var errNoKeyID = errors.New("the assertion names no signing key")

// keyMiss is the key lookup's refusal: the key list does not hold the key the
// assertion names.
type keyMiss struct{ why string }

func (m *keyMiss) Error() string { return m.why }

// keyList is the issuer's published keys, cached.
//
// The rules, in the order they matter:
//
//   - A complete read is used for KeyListTTL without another read.
//   - An assertion naming a key the cached list lacks triggers one read.
//   - Reads caused that way are limited to one per missInterval, for the whole
//     instance. Inside the interval, a key the fresh cached list lacks is
//     refused as a kid miss with no read, so a stream of assertions naming
//     invented keys cannot keep the instance reading the issuer's list. The
//     first unknown key after the interval is read for as before, and the
//     issuer publishes a key before it signs with one, so a rotation is
//     accepted on its first assertion unless another unknown key caused a
//     read in the interval before it. The limit applies only to a fresh list:
//     an empty or aged list is read as usual.
//   - A read that comes back short — failing, empty, or without the key that
//     was asked for — fails only the request that caused it. It never
//     replaces the cached keys and never restarts their clock. Replacing them
//     would turn one bad rotation into KeyListTTL of a locked-out fleet.
//   - Once the cached list has aged out, a complete read replaces it, even one
//     that no longer holds a key the cache did: that is how a retired key
//     stops being accepted. A read that fails or is empty leaves the aged
//     keys in use, because an issuer that cannot be reached has not retired
//     anything.
type keyList struct {
	url    string
	client *http.Client
	now    func() time.Time
	logger entities.Logger
	// missInterval is the shortest time between two reads caused by a key the
	// fresh cached list lacks.
	missInterval time.Duration

	mu     sync.RWMutex
	keys   map[string]*ecdsa.PublicKey
	readAt time.Time
	// missReadAt is when a key the fresh cached list lacked last caused a read.
	missReadAt time.Time

	// fetchMu lets one read run at a time, so a burst of assertions naming a
	// new key costs the issuer one read rather than one each.
	fetchMu sync.Mutex
}

// cached reports what the cached list says about kid without a read. throttled
// is true when the list is fresh, lacks kid, and a miss already caused a read
// less than missInterval ago.
func (l *keyList) cached(kid string) (key *ecdsa.PublicKey, fresh, throttled bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	now := l.now()
	fresh = l.keys != nil && now.Sub(l.readAt) < KeyListTTL
	key = l.keys[kid]
	throttled = fresh && key == nil &&
		!l.missReadAt.IsZero() && now.Sub(l.missReadAt) < l.missInterval
	return key, fresh, throttled
}

func (l *keyList) store(keys map[string]*ecdsa.PublicKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keys = keys
	l.readAt = l.now()
}

// markMissRead starts the interval in which no other miss causes a read. It is
// set before the read, so a read that fails still counts.
func (l *keyList) markMissRead() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missReadAt = l.now()
}

func (l *keyList) throttledMiss() error {
	return &keyMiss{why: fmt.Sprintf(
		"the issuer's key list does not hold the key the assertion names, and it was read for an unknown key less than %s ago",
		l.missInterval)}
}

// logKIDMax is the most of a kid a log line carries.
const logKIDMax = 16

// logKID is the part of a kid a log line may carry. The kid comes from the
// assertion's header, whose content and length the caller chooses, so it never
// reaches a refusal's detail and reaches a log line only as its first
// logKIDMax bytes, each outside printable ASCII replaced with '?'. Every
// published kid an operator needs to recognize fits in that.
func logKID(kid string) string {
	if len(kid) > logKIDMax {
		kid = kid[:logKIDMax]
	}
	b := []byte(kid)
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '?'
		}
	}
	return string(b)
}

func (l *keyList) key(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	// A throttled refusal is not logged here: the caller logs every refusal,
	// and a second line per invented kid would let the caller fill the log.
	if key, fresh, throttled := l.cached(kid); key != nil && fresh {
		return key, nil
	} else if throttled {
		return nil, l.throttledMiss()
	}
	l.fetchMu.Lock()
	defer l.fetchMu.Unlock()
	// Another request may have completed a read while this one waited.
	cachedKey, fresh, throttled := l.cached(kid)
	if cachedKey != nil && fresh {
		return cachedKey, nil
	}
	if throttled {
		return nil, l.throttledMiss()
	}
	if fresh {
		// The list is fresh and lacks kid, so this read is a miss's.
		l.markMissRead()
	}

	fetched, err := l.fetch(ctx)
	if err == nil && len(fetched) == 0 {
		err = errors.New("the key list holds no usable key")
	}
	if err != nil {
		if cachedKey != nil {
			l.logger.Warn(ctx, "trusted issuer key list could not be refreshed; the cached keys stay in use",
				"kid", logKID(kid), "kid_length", len(kid), "error", err.Error())
			return cachedKey, nil
		}
		l.logger.Warn(ctx, "trusted issuer key list came back short; the cached keys are unchanged",
			"kid", logKID(kid), "kid_length", len(kid), "error", err.Error())
		return nil, &keyMiss{why: "the issuer's key list could not be read to find the key the assertion names"}
	}

	key, published := fetched[kid]
	if published || !fresh {
		l.store(fetched)
	}
	if !published {
		l.logger.Warn(ctx, "trusted issuer key list does not publish the assertion's key",
			"kid", logKID(kid), "kid_length", len(kid))
		return nil, &keyMiss{why: "the issuer does not publish the key the assertion names"}
	}
	return key, nil
}

func (l *keyList) fetch(ctx context.Context) (map[string]*ecdsa.PublicKey, error) {
	ctx, cancel := context.WithTimeout(ctx, keyListFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build the key list request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read the key list: %w", err)
	}
	defer func() {
		// The body is read or abandoned by now; failing to close it changes
		// nothing about the keys that were, or were not, read from it.
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the key list answered %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, keyListMaxBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode the key list: %w", err)
	}
	keys := make(map[string]*ecdsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		pub, err := k.publicKey()
		if err != nil {
			// A key this verifier cannot use is not one it can accept an
			// assertion under; leaving it out of the list is that refusal.
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

// jsonWebKey is the part of an RFC 7517 key the verifier reads.
type jsonWebKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// publicKey returns the key when it is a P-256 signing key usable for ES256.
func (k jsonWebKey) publicKey() (*ecdsa.PublicKey, error) {
	switch {
	case k.Kid == "":
		return nil, errors.New("key has no kid")
	case k.Kty != "EC" || k.Crv != "P-256":
		return nil, errors.New("key is not a P-256 elliptic-curve key")
	case k.Alg != "" && k.Alg != "ES256":
		return nil, errors.New("key is not for ES256")
	case k.Use != "" && k.Use != "sig":
		return nil, errors.New("key is not a signing key")
	}
	x, errX := base64.RawURLEncoding.DecodeString(k.X)
	y, errY := base64.RawURLEncoding.DecodeString(k.Y)
	if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
		return nil, errors.New("key coordinates are not 32-byte base64url values")
	}
	point := make([]byte, 0, 65)
	point = append(point, 0x04)
	point = append(point, x...)
	point = append(point, y...)
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
}

// CheckJWKSURL reports whether a key-list address is one the verifier may
// read: https, or plain http to a loopback host, where there is no network
// for anyone to tamper with the keys on.
func CheckJWKSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return errors.New("the key list address is not an absolute URL")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
	}
	return errors.New("the key list address must use https (plain http is accepted only for a loopback host)")
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
