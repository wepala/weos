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

interface PaginatedResponse<T> {
  data: T[]
  cursor: string
  has_more: boolean
  messages?: ApiMessage[]
}

export function useResourceApi(typeSlug: string) {
  const { request } = useApi()

  async function list(
    cursor = '',
    limit = 20,
    sortBy = '',
    sortOrder = '',
    filters?: Record<string, Record<string, string>>,
  ) {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    params.set('limit', String(limit))
    if (sortBy) params.set('sort_by', sortBy)
    if (sortOrder) params.set('sort_order', sortOrder)
    if (filters) {
      for (const [field, ops] of Object.entries(filters)) {
        for (const [op, value] of Object.entries(ops)) {
          params.append(`_filter[${field}][${op}]`, value)
        }
      }
    }
    // Paginated responses keep their top-level shape; forwardMessages
    // processes any messages without unwrapping.
    try {
      const res = await $fetch<PaginatedResponse<any>>(
        `/api/${typeSlug}?${params}`,
      )
      forwardMessages(res)
      return res
    } catch (err: any) {
      if (err?.data) forwardMessages(err.data)
      throw err
    }
  }

  function get(id: string) {
    return request<any>(`/api/${typeSlug}/${id}`)
  }

  function create(data: Record<string, any>) {
    return request<any>(`/api/${typeSlug}`, {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  function update(id: string, data: Record<string, any>) {
    return request<any>(`/api/${typeSlug}/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    })
  }

  function remove(id: string) {
    return request<void>(`/api/${typeSlug}/${id}`, { method: 'DELETE' })
  }

  return { list, get, create, update, remove }
}
