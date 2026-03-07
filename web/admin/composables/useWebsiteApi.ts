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

interface Website {
  id: string
  name: string
  url: string
  description?: string
  language: string
  status: string
  created_at: string
}

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

interface CreateWebsitePayload {
  name: string
  url: string
  slug?: string
}

interface UpdateWebsitePayload {
  name: string
  url: string
  description: string
  language: string
  status: string
}

export function useWebsiteApi() {
  function listWebsites(cursor = '', limit = 20) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    return $fetch<PaginatedResponse<Website>>(
      `/api/websites?${params}`,
    )
  }

  function getWebsite(id: string) {
    return $fetch<Website>(`/api/websites/${id}`)
  }

  function createWebsite(payload: CreateWebsitePayload) {
    return $fetch<Website>('/api/websites', {
      method: 'POST',
      body: payload,
    })
  }

  function updateWebsite(id: string, payload: UpdateWebsitePayload) {
    return $fetch<Website>(`/api/websites/${id}`, {
      method: 'PUT',
      body: payload,
    })
  }

  function deleteWebsite(id: string) {
    return $fetch(`/api/websites/${id}`, { method: 'DELETE' })
  }

  return {
    listWebsites,
    getWebsite,
    createWebsite,
    updateWebsite,
    deleteWebsite,
  }
}
