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

const cache = new Map<string, Map<string, string>>()

export function useResourceLookup() {
  const version = ref(0)

  async function preloadType(typeSlug: string, force = false) {
    if (cache.has(typeSlug) && !force) return
    const api = useResourceApi(typeSlug)
    const map = new Map<string, string>()
    const maxPages = 10
    try {
      let cursor = ''
      let hasMore = true
      let page = 0
      while (hasMore && page < maxPages) {
        const res = await api.list(cursor, 100)
        for (const item of res.data) {
          map.set(item.id, item.name || item.invoiceNumber || item.id)
        }
        cursor = res.cursor
        hasMore = res.has_more
        page++
      }
      cache.set(typeSlug, map)
      version.value++
    } catch {
      if (map.size > 0) {
        cache.set(typeSlug, map)
        version.value++
      }
    }
  }

  function resolve(typeSlug: string, id: string): string {
    // Access version to make this reactive
    void version.value
    return cache.get(typeSlug)?.get(id) || id
  }

  return { preloadType, resolve }
}
