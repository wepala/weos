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
	"encoding/binary"
	"fmt"

	"github.com/wepala/weos/v3/domain/repositories"

	"gorm.io/gorm"
)

// signInLockNamespace prefixes every key before it is hashed, so a sign-in lock
// never shares an advisory lock id with another use of advisory locks.
const signInLockNamespace = "weos.asserted-sign-in\x00"

// SignInLock implements repositories.SignInLock.
//
// On PostgreSQL each key is a transaction-scoped advisory lock
// (pg_advisory_xact_lock), taken inside a transaction that exists only to hold
// the locks and writes nothing. Rolling it back releases them, and so does the
// server when the connection or the process holding it dies, so a crashed
// replica never leaves an email locked. The caller's own reads and writes run
// on other pooled connections, outside this transaction: they commit as they
// go, so a replica that waits on the lock reads them once it is released.
//
// On SQLite it holds nothing. A SQLite database is served by one process (see
// ProvideFeatureCacheInvalidator), where the caller's in-process locks already
// serialize, and a transaction opened here would take the SQLite write gate and
// stall the very writes it is meant to protect.
type SignInLock struct {
	db *gorm.DB
}

// ProvideSignInLock builds the lock.
func ProvideSignInLock(db *gorm.DB) repositories.SignInLock {
	return &SignInLock{db: db}
}

// Hold implements repositories.SignInLock.
//
// A sign-in that holds the lock keeps one pooled connection for as long as it
// holds it, and a sign-in waiting on the same key keeps one while it waits.
func (l *SignInLock) Hold(ctx context.Context, keys ...string) (func(), error) {
	if len(keys) == 0 || l.db.Name() != "postgres" {
		return func() {}, nil
	}
	// The transaction outlives ctx on purpose: it must stay open until the
	// caller releases it, not end the moment a request is canceled while the
	// caller's writes are still landing. Each wait for a key still ends with
	// ctx.
	tx := l.db.WithContext(context.WithoutCancel(ctx)).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("begin the sign-in lock transaction: %w", tx.Error)
	}
	for _, key := range keys {
		if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(?)", signInLockID(key)).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("take the sign-in lock: %w", err)
		}
	}
	// A rollback that fails leaves nothing held: the connection it failed on is
	// closed, and the server releases a closed session's locks.
	return func() { tx.Rollback() }, nil
}

// signInLockID is the advisory lock id for key: the first eight bytes of the
// SHA-256 of the namespaced key. Two keys that share an id only serialize two
// sign-ins that did not need it.
func signInLockID(key string) int64 {
	sum := sha256.Sum256([]byte(signInLockNamespace + key))
	return int64(binary.BigEndian.Uint64(sum[:8])) //nolint:gosec // an id, wrapping is intended
}
