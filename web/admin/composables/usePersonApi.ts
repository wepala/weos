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

import { unwrapEnvelope, forwardMessages } from './useApi'

interface Person {
  id: string
  given_name: string
  family_name: string
  name: string
  email: string
  avatar_url?: string
  status: string
  created_at: string
}

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

interface CreatePersonPayload {
  given_name: string
  family_name: string
  email: string
}

interface UpdatePersonPayload {
  given_name: string
  family_name: string
  email: string
  avatar_url: string
  status: string
}

export function usePersonApi() {
  async function listPersons(cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    const res = await $fetch<PaginatedResponse<Person>>(
      `/api/persons?${params}`,
    )
    forwardMessages(res)
    return res
  }

  async function getPerson(id: string) {
    const res = await $fetch<unknown>(`/api/persons/${id}`)
    return unwrapEnvelope<Person>(res)
  }

  async function createPerson(payload: CreatePersonPayload) {
    const res = await $fetch<unknown>('/api/persons', {
      method: 'POST',
      body: payload,
    })
    return unwrapEnvelope<Person>(res)
  }

  async function updatePerson(id: string, payload: UpdatePersonPayload) {
    const res = await $fetch<unknown>(`/api/persons/${id}`, {
      method: 'PUT',
      body: payload,
    })
    return unwrapEnvelope<Person>(res)
  }

  async function deletePerson(id: string) {
    const res = await $fetch<unknown>(`/api/persons/${id}`, { method: 'DELETE' })
    return unwrapEnvelope<void>(res)
  }

  return {
    listPersons,
    getPerson,
    createPerson,
    updatePerson,
    deletePerson,
  }
}
