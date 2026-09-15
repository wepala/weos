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

// wm-669nf. Run with `npm run test:unit` (Node's own test runner, which reads
// TypeScript directly on Node 23.6 or later).

import { test } from 'node:test'
import assert from 'node:assert/strict'

import {
  IMPERSONATION_ENDED_TEXT,
  IMPERSONATION_REFUSED_CODE,
  IMPERSONATION_REFUSED_TEXT,
  clearImpersonation,
  impersonationErrorText,
  impersonationRefusalOutcome,
  isImpersonationRefusal,
} from '../../composables/impersonationRefusal.ts'

const refusal = { error: 'impersonation not allowed', code: IMPERSONATION_REFUSED_CODE }

test('the refusal is a 403 carrying the impersonation code, and nothing else is', () => {
  assert.equal(isImpersonationRefusal(403, refusal), true)
  assert.equal(isImpersonationRefusal(401, refusal), false)
  assert.equal(isImpersonationRefusal(403, { error: 'admin role required' }), false)
  assert.equal(isImpersonationRefusal(403, { code: 'account_access_revoked' }), false)
  assert.equal(isImpersonationRefusal(403, null), false)
  assert.equal(isImpersonationRefusal(undefined, undefined), false)
})

test('a refused start is explained in plain words, not with the raw error', () => {
  const err = { status: 403, data: refusal }
  assert.equal(impersonationErrorText(err, 'Failed to start impersonation'), IMPERSONATION_REFUSED_TEXT)
  assert.equal(IMPERSONATION_REFUSED_TEXT, 'You can only impersonate a member of your account.')
})

test('any other failure keeps the server message, or the fallback when there is none', () => {
  assert.equal(
    impersonationErrorText({ statusCode: 403, data: { error: 'admin role required' } }, 'Failed to start impersonation'),
    'admin role required',
  )
  assert.equal(impersonationErrorText(new Error('network'), 'Failed to stop impersonation'), 'Failed to stop impersonation')
  assert.equal(impersonationErrorText(undefined, 'Failed'), 'Failed')
})

test('the refusal on any request ends the impersonation, and says so while one was shown', () => {
  assert.deepEqual(impersonationRefusalOutcome(403, refusal, '/api/persons', true), {
    ended: true,
    notice: IMPERSONATION_ENDED_TEXT,
  })
  assert.deepEqual(impersonationRefusalOutcome(403, refusal, '/api/persons', false), { ended: true, notice: null })
})

test('a refused start ends the impersonation without a second notice, because the page explains it', () => {
  assert.deepEqual(impersonationRefusalOutcome(403, refusal, '/api/admin/impersonate', true), { ended: true, notice: null })
  assert.deepEqual(
    impersonationRefusalOutcome(403, refusal, { url: 'http://localhost:8080/api/admin/impersonate?x=1' }, true),
    { ended: true, notice: null },
  )
})

test('any other refusal does not end the impersonation', () => {
  assert.deepEqual(impersonationRefusalOutcome(403, { error: 'admin role required' }, '/api/users', true), {
    ended: false,
    notice: null,
  })
  assert.deepEqual(impersonationRefusalOutcome(401, refusal, '/api/users', true), { ended: false, notice: null })
})

test('clearing the impersonation drops the banner state and keeps the rest of the identity', () => {
  const user = { id: 'counsel', name: 'Counsel', email: 'counsel@example.com', impersonating: true, real_user: { id: 'ops' } }
  assert.deepEqual(clearImpersonation(user), { id: 'counsel', name: 'Counsel', email: 'counsel@example.com', impersonating: false })
  assert.equal(clearImpersonation(null), null)
})
