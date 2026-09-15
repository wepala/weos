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

// wm-ptcuk, Copilot review 5203947880. The agent chat streams with native
// fetch, which the $fetch interceptor never sees. Run with `npm run test:unit`.

import { test } from 'node:test'
import assert from 'node:assert/strict'

import { applyStreamRefusal } from '../../composables/agentStreamRefusal.ts'
import {
  IMPERSONATION_ENDED_TEXT,
  IMPERSONATION_REFUSED_CODE,
  impersonationRefusalOutcome,
} from '../../composables/impersonationRefusal.ts'

const STREAM_URL = '/api/agent/conversations/c1/messages'

interface Applied {
  request: unknown
  status: number | undefined
  body: unknown
}

function recorder() {
  const calls: Applied[] = []
  return {
    calls,
    apply(request: unknown, status: number | undefined, body: unknown) {
      calls.push({ request, status, body })
    },
  }
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

test('a 403 impersonation refusal on the stream is applied, so the impersonation ends', async () => {
  const refusal = { error: 'impersonation not allowed', code: IMPERSONATION_REFUSED_CODE }
  const seen = recorder()
  await applyStreamRefusal(jsonResponse(403, refusal), STREAM_URL, seen.apply)

  assert.equal(seen.calls.length, 1)
  const [call] = seen.calls
  assert.deepEqual(call, { request: STREAM_URL, status: 403, body: refusal })
  // What the $fetch interceptor decides for the same answer.
  assert.deepEqual(impersonationRefusalOutcome(call.status, call.body, call.request, true), {
    ended: true,
    notice: IMPERSONATION_ENDED_TEXT,
  })
})

test('a coded 401 on the stream is applied the same way', async () => {
  const seen = recorder()
  await applyStreamRefusal(jsonResponse(401, { error: 'not authenticated', code: 'account_deactivated' }), STREAM_URL, seen.apply)
  assert.deepEqual(seen.calls, [
    { request: STREAM_URL, status: 401, body: { error: 'not authenticated', code: 'account_deactivated' } },
  ])
})

test('a failure with no JSON body is applied with a null body', async () => {
  const seen = recorder()
  await applyStreamRefusal(new Response('upstream down', { status: 502 }), STREAM_URL, seen.apply)
  assert.deepEqual(seen.calls, [{ request: STREAM_URL, status: 502, body: null }])
})

test('a stream the server accepted is not applied', async () => {
  const seen = recorder()
  await applyStreamRefusal(new Response('data: {"type":"done"}\n\n', { status: 200 }), STREAM_URL, seen.apply)
  assert.equal(seen.calls.length, 0)
})

test('the refused body can still be read afterwards, for the error the stream throws', async () => {
  const res = jsonResponse(403, { error: 'impersonation not allowed', code: IMPERSONATION_REFUSED_CODE })
  await applyStreamRefusal(res, STREAM_URL, () => {})
  assert.equal((await res.json()).error, 'impersonation not allowed')
})
