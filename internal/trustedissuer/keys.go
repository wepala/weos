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

var (
	errNoKeyID          = errors.New("the assertion names no signing key")
	errKeyListRedirects = errors.New("the key list address answered with a redirect, and redirects are not followed")
)

// refuseRedirect is the key-list client's CheckRedirect. CheckJWKSURL holds the
// configured address to https; a redirect could send the read anywhere, plain
// http included, so none is followed.
func refuseRedirect(*http.Request, []*http.Request) error { return errKeyListRedirects }

// keyRefusal is the key lookup's refusal: no key the verifier holds or can
// read answers for the assertion. why is fixed wording and never carries the
// kid, which the caller chooses. cause is set only when the request ended
// while it waited for a read; it is that request's context error.
type keyRefusal struct {
	reason Reason
	why    string
	cause  error
}

func (r *keyRefusal) Error() string { return r.why }

// keyList is the issuer's published keys, cached.
//
// The rules, in the order they matter:
//
//   - A complete read is used for KeyListTTL without another read.
//   - An assertion naming a key the cached list lacks triggers one read.
//   - One read runs at a time, and every decision that could lead to a read —
//     the miss limit and the backoff below included — is made holding the
//     fetch slot. A request that needs a read while one is running waits for
//     it and then sees what it found, so a burst of assertions naming a newly
//     published key costs the issuer one read and is accepted whole. A
//     request that is waiting leaves when its own request ends. That is not
//     a failed read: it is not logged, and it does not touch the backoff.
//   - Reads caused by a key the fresh cached list lacks are limited to one per
//     missInterval, for the whole instance. Inside the interval such a key is
//     refused as a kid miss with no read, so a stream of assertions naming
//     invented keys cannot keep the instance reading the issuer's list. The
//     limit applies only to a fresh list: an empty or aged list is read as
//     usual, unless a read is backing off.
//   - The last complete read a miss caused is kept beside the cache. A key it
//     holds is accepted while the cache is fresh, so a read an invented kid
//     caused, which already holds the key the issuer has just rotated to, is
//     not thrown away. It never replaces or extends the cached keys, and the
//     next read that replaces them discards it.
//   - A read that fails — unreachable, answering other than 200, undecodable,
//     redirecting — or that holds no usable key starts a backoff in which no
//     read is made. The first failure backs off KeyListFirstRetry; each
//     further failure in a row doubles it, up to missInterval; a complete
//     read ends the run. An instance that wakes with nothing cached and
//     misses one read is signing people in again seconds later, while an
//     issuer that stays down is read at most once per missInterval. A cached
//     key, fresh or aged, still answers; anything else is refused with no
//     read: keys-unreachable after a read that failed, kid-miss after a read
//     that held no usable key. The failure is logged by the read, so once per
//     backoff.
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
	// fresh cached list lacks, and the longest reads back off after failures.
	missInterval time.Duration
	// firstRetry is how long reads back off after the first failure in a run.
	firstRetry time.Duration

	// fetchSlot holds a token while a request is deciding whether to read, or
	// reading. A channel rather than a mutex, so a waiter can leave when its
	// request ends.
	fetchSlot chan struct{}

	mu     sync.RWMutex
	keys   map[string]*ecdsa.PublicKey
	readAt time.Time
	// missKeys is the last complete read a miss caused that did not replace
	// the cache. Cleared whenever the cache is replaced.
	missKeys map[string]*ecdsa.PublicKey
	// missReadAt is when a key the fresh cached list lacked last caused a read.
	missReadAt time.Time
	// failedReadAt is when a read last failed or held no usable key;
	// failedUnreachable says which. Cleared by a read that succeeds.
	failedReadAt      time.Time
	failedUnreachable bool
	// failedReads counts the reads that failed in a row, and retryAfter is the
	// backoff the last of them started. Both are reset by a complete read.
	failedReads int
	retryAfter  time.Duration
}

// keyView is what the cache says about one kid at one moment, without a read.
type keyView struct {
	// key is the kid's key from the cached list or, failing that, from the
	// last miss read.
	key *ecdsa.PublicKey
	// fresh: the cached list is younger than KeyListTTL.
	fresh bool
	// throttled: a miss caused a read less than missInterval ago.
	throttled bool
	// backingOff: a read failed or held no usable key less than retryAfter
	// ago; unreachable says it failed.
	backingOff  bool
	unreachable bool
	retryAfter  time.Duration
}

func (l *keyList) look(kid string) keyView {
	l.mu.RLock()
	defer l.mu.RUnlock()
	now := l.now()
	v := keyView{
		fresh:       l.keys != nil && now.Sub(l.readAt) < KeyListTTL,
		key:         l.keys[kid],
		throttled:   !l.missReadAt.IsZero() && now.Sub(l.missReadAt) < l.missInterval,
		backingOff:  !l.failedReadAt.IsZero() && now.Sub(l.failedReadAt) < l.retryAfter,
		unreachable: l.failedUnreachable,
		retryAfter:  l.retryAfter,
	}
	if v.key == nil {
		v.key = l.missKeys[kid]
	}
	return v
}

// store replaces the cached keys with a complete read.
func (l *keyList) store(keys map[string]*ecdsa.PublicKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keys = keys
	l.readAt = l.now()
	l.missKeys = nil
	l.clearFailuresLocked()
}

// storeMissRead keeps a complete read a miss caused beside the cached keys,
// which it leaves as they are.
func (l *keyList) storeMissRead(keys map[string]*ecdsa.PublicKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missKeys = keys
	l.clearFailuresLocked()
}

// clearFailuresLocked ends a run of failed reads. l.mu must be held.
func (l *keyList) clearFailuresLocked() {
	l.failedReadAt = time.Time{}
	l.failedReads = 0
	l.retryAfter = 0
}

// markMissRead starts the interval in which no other miss causes a read. It is
// set before the read, so a read that fails still counts.
func (l *keyList) markMissRead() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.missReadAt = l.now()
}

// markFailedRead starts the backoff in which no read is made and returns its
// length: firstRetry after the first failure in a run, doubled by each further
// failure, never longer than missInterval.
func (l *keyList) markFailedRead(unreachable bool) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failedReads++
	retry := min(l.firstRetry, l.missInterval)
	for i := 1; i < l.failedReads && retry < l.missInterval; i++ {
		retry = min(2*retry, l.missInterval)
	}
	l.retryAfter = retry
	l.failedReadAt = l.now()
	l.failedUnreachable = unreachable
	return retry
}

func (l *keyList) throttledMiss() error {
	return &keyRefusal{reason: ReasonKidMiss, why: fmt.Sprintf(
		"the issuer's key list does not hold the key the assertion names, and it was read for an unknown key less than %s ago",
		l.missInterval)}
}

func backoffRefusal(v keyView) error {
	if v.unreachable {
		return &keyRefusal{reason: ReasonKeysUnreachable, why: fmt.Sprintf(
			"the issuer's key list could not be read less than %s ago, and it is not read again before then",
			v.retryAfter)}
	}
	return &keyRefusal{reason: ReasonKidMiss, why: fmt.Sprintf(
		"the issuer's key list held no usable key when it was read less than %s ago, and it is not read again before then",
		v.retryAfter)}
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
	if v := l.look(kid); v.key != nil && v.fresh {
		return v.key, nil
	}

	select {
	case l.fetchSlot <- struct{}{}:
	case <-ctx.Done():
		// The request went away, not the key list: nothing is logged here and
		// the backoff is left as it is. cause lets the caller tell this apart
		// from a key list that could not be read.
		return nil, &keyRefusal{reason: ReasonKeysUnreachable,
			why:   "the request ended while it waited for the issuer's key list to be read",
			cause: ctx.Err()}
	}
	defer func() { <-l.fetchSlot }()

	// Decided again holding the slot: a read that finished while this request
	// waited may have found the key, started the miss interval, or failed.
	// Refusals made here are not logged: the caller logs every refusal, and a
	// second line per request would let the caller fill the log.
	v := l.look(kid)
	switch {
	case v.key != nil && v.fresh:
		return v.key, nil
	case v.backingOff:
		if v.key != nil {
			// An aged key stays in use: an issuer that cannot be read has not
			// retired it.
			return v.key, nil
		}
		return nil, backoffRefusal(v)
	case v.fresh && v.throttled:
		return nil, l.throttledMiss()
	}
	if v.fresh {
		// The list is fresh and lacks kid, so this read is a miss's.
		l.markMissRead()
	}

	// Requests queued behind this read act on what it finds, so it runs to its
	// own timeout even when this request ends first: a client that went away
	// is not an issuer that cannot be reached.
	fetched, err := l.fetch(context.WithoutCancel(ctx))
	unreachable := err != nil
	if err == nil && len(fetched) == 0 {
		err = errors.New("the key list holds no usable key")
	}
	if err != nil {
		retry := l.markFailedRead(unreachable)
		if v.key != nil {
			l.logger.Warn(ctx, "trusted issuer key list could not be refreshed; the cached keys stay in use",
				"kid", logKID(kid), "kid_length", len(kid), "error", err.Error(), "retry_after", retry.String())
			return v.key, nil
		}
		l.logger.Warn(ctx, "trusted issuer key list came back short; the cached keys are unchanged",
			"kid", logKID(kid), "kid_length", len(kid), "error", err.Error(), "retry_after", retry.String())
		if unreachable {
			return nil, &keyRefusal{reason: ReasonKeysUnreachable,
				why: "the issuer's key list could not be read to find the key the assertion names"}
		}
		return nil, &keyRefusal{reason: ReasonKidMiss, why: "the issuer's key list holds no usable key"}
	}

	key, published := fetched[kid]
	if published || !v.fresh {
		l.store(fetched)
	} else {
		l.storeMissRead(fetched)
	}
	if !published {
		l.logger.Warn(ctx, "trusted issuer key list does not publish the assertion's key",
			"kid", logKID(kid), "kid_length", len(kid))
		return nil, &keyRefusal{reason: ReasonKidMiss, why: "the issuer does not publish the key the assertion names"}
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
// for anyone to tamper with the keys on. The verifier never follows a
// redirect from it, so the address checked is the address read.
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
