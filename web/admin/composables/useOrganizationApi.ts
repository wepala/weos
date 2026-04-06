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

import { unwrapEnvelope } from './useApi'

interface Organization {
  id: string
  name: string
  slug: string
  description?: string
  url?: string
  logo_url?: string
  status: string
  created_at: string
}

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

interface CreateOrganizationPayload {
  name: string
  slug: string
}

interface UpdateOrganizationPayload {
  name: string
  slug: string
  description: string
  url: string
  logo_url: string
  status: string
}

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

export function useOrganizationApi() {
  function listOrganizations(cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    return $fetch<PaginatedResponse<Organization>>(
      `/api/organizations?${params}`,
    )
  }

  async function getOrganization(id: string) {
    const res = await $fetch<unknown>(`/api/organizations/${id}`)
    return unwrapEnvelope<Organization>(res)
  }

  async function createOrganization(payload: CreateOrganizationPayload) {
    const res = await $fetch<unknown>('/api/organizations', {
      method: 'POST',
      body: payload,
    })
    return unwrapEnvelope<Organization>(res)
  }

  async function updateOrganization(id: string, payload: UpdateOrganizationPayload) {
    const res = await $fetch<unknown>(`/api/organizations/${id}`, {
      method: 'PUT',
      body: payload,
    })
    return unwrapEnvelope<Organization>(res)
  }

  async function deleteOrganization(id: string) {
    const res = await $fetch<unknown>(`/api/organizations/${id}`, { method: 'DELETE' })
    return unwrapEnvelope<void>(res)
  }

  function listMembers(orgId: string, cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    return $fetch<PaginatedResponse<Person>>(
      `/api/organizations/${orgId}/members?${params}`,
    )
  }

  return {
    listOrganizations,
    getOrganization,
    createOrganization,
    updateOrganization,
    deleteOrganization,
    listMembers,
  }
}
