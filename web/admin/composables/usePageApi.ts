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

interface Page {
  id: string
  name: string
  slug: string
  description?: string
  template?: string
  position: number
  status: string
  created_at: string
}

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
}

interface CreatePagePayload {
  website_id: string
  name: string
  slug: string
}

interface UpdatePagePayload {
  website_id: string
  name: string
  slug: string
  description: string
  template: string
  position: number
  status: string
}

export function usePageApi() {
  function listPages(cursor = '', limit = 20, websiteId = '') {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    if (websiteId) params.set('website_id', websiteId)
    return $fetch<PaginatedResponse<Page>>(`/api/pages?${params}`)
  }

  function getPage(id: string) {
    return $fetch<Page>(`/api/pages/${id}`)
  }

  function createPage(payload: CreatePagePayload) {
    return $fetch<Page>('/api/pages', {
      method: 'POST',
      body: payload,
    })
  }

  function updatePage(id: string, payload: UpdatePagePayload) {
    return $fetch<Page>(`/api/pages/${id}`, {
      method: 'PUT',
      body: payload,
    })
  }

  function deletePage(id: string) {
    return $fetch(`/api/pages/${id}`, { method: 'DELETE' })
  }

  return { listPages, getPage, createPage, updatePage, deletePage }
}
