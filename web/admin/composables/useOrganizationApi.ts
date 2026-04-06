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

import { forwardMessages } from './useApi'
import type { ApiMessage } from './useNotifications'

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
  messages?: ApiMessage[]
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
  const { request } = useApi()

  async function listOrganizations(cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    try {
      const res = await $fetch<PaginatedResponse<Organization>>(
        `/api/organizations?${params}`,
      )
      forwardMessages(res)
      return res
    } catch (err: any) {
      if (err?.data) forwardMessages(err.data)
      throw err
    }
  }

  function getOrganization(id: string) {
    return request<Organization>(`/api/organizations/${id}`)
  }

  function createOrganization(payload: CreateOrganizationPayload) {
    return request<Organization>('/api/organizations', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  }

  function updateOrganization(id: string, payload: UpdateOrganizationPayload) {
    return request<Organization>(`/api/organizations/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  function deleteOrganization(id: string) {
    return request<void>(`/api/organizations/${id}`, { method: 'DELETE' })
  }

  async function listMembers(orgId: string, cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    try {
      const res = await $fetch<PaginatedResponse<Person>>(
        `/api/organizations/${orgId}/members?${params}`,
      )
      forwardMessages(res)
      return res
    } catch (err: any) {
      if (err?.data) forwardMessages(err.data)
      throw err
    }
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
