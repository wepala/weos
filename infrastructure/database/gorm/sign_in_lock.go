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

package gorm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/wepala/weos/v3/domain/repositories"

	"gorm.io/gorm"
)

// signInLockNamespace prefixes every key before it is hashed, so a sign-in lock
// never shares an advisory lock id with another use of advisory locks.
const signInLockNamespace = "weos.asserted-sign-in\x00"

// unboundedPoolSignInLockHolders is how many sign-ins may hold or wait for
// advisory locks at once on a pool with no connection limit.
const unboundedPoolSignInLockHolders = 25

// SignInLock implements repositories.SignInLock.
//
// Every key is first held in this process, by a lock every caller of this
// SignInLock shares: owner binding and the OAuth callbacks' FindOrCreateAgent
// wait on each other here, on every database.
//
// On PostgreSQL each key is then also a transaction-scoped advisory lock
// (pg_advisory_xact_lock), taken inside a transaction that exists only to hold
// the locks and writes nothing. Rolling it back releases them, and so does the
// server when the connection or the process holding it dies, so a crashed
// replica never leaves an email locked. The caller's own reads and writes run
// on other pooled connections, outside this transaction: they commit as they
// go, so a replica that waits on the lock reads them once it is released.
//
// That transaction keeps one pooled connection while its caller needs another
// for its work. So only a quarter of the pool may hold or wait for advisory
// locks at once (see signInLockHolders); a sign-in past that bound waits in
// this process, holding no connection, and the rest of the pool stays free for
// the holders' work and every other request. Without the bound, as many
// concurrent sign-ins as the pool has connections would each hold one and wait
// forever for a second.
//
// On SQLite it takes no advisory lock. A SQLite database is served by one
// process (see ProvideFeatureCacheInvalidator), where the lock in process
// already serializes, and a transaction opened here would take the SQLite
// write gate and stall the very writes it is meant to protect.
type SignInLock struct {
	db   *gorm.DB
	keys keyedLocks

	holdersOnce sync.Once
	holders     chan struct{}
	holdersErr  error
}

// ProvideSignInLock builds the lock.
func ProvideSignInLock(db *gorm.DB) repositories.SignInLock {
	return &SignInLock{db: db}
}

// Hold implements repositories.SignInLock.
//
// Every wait ends with ctx: for a key in this process, for a place among the
// advisory lock holders, for a pooled connection, and for each advisory lock.
func (l *SignInLock) Hold(ctx context.Context, keys ...string) (func(), error) {
	if len(keys) == 0 {
		return func() {}, nil
	}
	releaseKeys, err := l.keys.lock(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("take the sign-in lock: %w", err)
	}
	if l.db.Name() != "postgres" {
		return releaseKeys, nil
	}
	releaseAdvisory, err := l.holdAdvisory(ctx, keys)
	if err != nil {
		releaseKeys()
		return nil, err
	}
	return func() {
		releaseAdvisory()
		releaseKeys()
	}, nil
}

// holdAdvisory takes the advisory lock for every key on one pooled connection,
// once this process has a place among the holders.
func (l *SignInLock) holdAdvisory(ctx context.Context, keys []string) (func(), error) {
	sqlDB, err := l.db.DB()
	if err != nil {
		return nil, fmt.Errorf("get the database pool for the sign-in lock: %w", err)
	}
	holders, err := l.holderPlaces(sqlDB)
	if err != nil {
		return nil, err
	}
	select {
	case holders <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait to hold the sign-in lock: %w", ctx.Err())
	}
	leave := func() { <-holders }

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		leave()
		return nil, fmt.Errorf("take a connection for the sign-in lock: %w", err)
	}
	// The transaction outlives ctx on purpose: it must stay open until the
	// caller releases it, not end the moment a request is canceled while the
	// caller's writes are still landing. The connection is already this
	// lock's, so beginning waits on nothing.
	tx, err := conn.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		_ = conn.Close()
		leave()
		return nil, fmt.Errorf("begin the sign-in lock transaction: %w", err)
	}
	// A rollback that fails leaves nothing held: the connection it failed on is
	// discarded, and the server releases a closed session's locks.
	release := func() {
		_ = tx.Rollback()
		_ = conn.Close()
		leave()
	}
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", signInLockID(key)); err != nil {
			release()
			return nil, fmt.Errorf("take the sign-in lock: %w", err)
		}
	}
	return release, nil
}

// holderPlaces returns the places among the advisory lock holders, sized from
// the pool the first time a lock is held, after the pool is configured.
func (l *SignInLock) holderPlaces(sqlDB *sql.DB) (chan struct{}, error) {
	l.holdersOnce.Do(func() {
		n := signInLockHolders(sqlDB.Stats().MaxOpenConnections)
		if n == 0 {
			l.holdersErr = errors.New("the database pool allows one connection, and a sign-in lock would hold it while its sign-in waits for another")
			return
		}
		l.holders = make(chan struct{}, n)
	})
	return l.holders, l.holdersErr
}

// signInLockHolders is how many sign-ins may hold or wait for advisory locks at
// once on a pool of maxOpen connections (0 is no limit): a quarter of the pool,
// and at least one. Each holder keeps one connection for its locks and needs
// one more at a time for its work, so the rest of the pool always has room for
// that work. A pool of one connection has no room, and gets 0.
func signInLockHolders(maxOpen int) int {
	switch {
	case maxOpen <= 0:
		return unboundedPoolSignInLockHolders
	case maxOpen == 1:
		return 0
	case maxOpen < 8:
		return 1
	}
	return maxOpen / 4
}

// signInLockID is the advisory lock id for key: the first eight bytes of the
// SHA-256 of the namespaced key. Two keys that share an id only serialize two
// sign-ins that did not need it.
func signInLockID(key string) int64 {
	sum := sha256.Sum256([]byte(signInLockNamespace + key))
	return int64(binary.BigEndian.Uint64(sum[:8])) //nolint:gosec // an id, wrapping is intended
}

// keyedLocks serializes the holders of each key within this process. A wait for
// a key ends with ctx. An entry lives only while someone holds or waits for its
// key, so the map does not grow with every key seen.
type keyedLocks struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	held  chan struct{}
	users int
}

// lock takes every key in the order given and returns the function that
// releases them all. When ctx ends first it releases the keys it took and
// returns ctx's error.
func (k *keyedLocks) lock(ctx context.Context, keys []string) (func(), error) {
	taken := make([]string, 0, len(keys))
	release := func() {
		for i := len(taken) - 1; i >= 0; i-- {
			<-k.entry(taken[i]).held
			k.leave(taken[i])
		}
	}
	for _, key := range keys {
		l := k.join(key)
		select {
		case l.held <- struct{}{}:
			taken = append(taken, key)
		case <-ctx.Done():
			k.leave(key)
			release()
			return nil, ctx.Err()
		}
	}
	return release, nil
}

// join counts one more holder or waiter for key and returns its entry.
func (k *keyedLocks) join(key string) *keyedLock {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l, ok := k.locks[key]
	if !ok {
		l = &keyedLock{held: make(chan struct{}, 1)}
		k.locks[key] = l
	}
	l.users++
	return l
}

// entry returns key's entry, which exists while its caller holds the key.
func (k *keyedLocks) entry(key string) *keyedLock {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.locks[key]
}

// leave counts one fewer holder or waiter for key, and forgets the key once
// nobody holds or waits for it.
func (k *keyedLocks) leave(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	l := k.locks[key]
	l.users--
	if l.users == 0 {
		delete(k.locks, key)
	}
}
