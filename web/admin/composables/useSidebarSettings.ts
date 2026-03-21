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

const STORAGE_KEY = 'weos-sidebar-hidden-types'
const GROUPS_STORAGE_KEY = 'weos-sidebar-menu-groups'

export function useSidebarSettings() {
  const hiddenSlugs = useState<string[]>('sidebarHiddenTypes', () => [])
  const menuGroups = useState<Record<string, string>>('sidebarMenuGroups', () => ({}))

  function loadSettings() {
    let hasLocal = false
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (raw) {
        hiddenSlugs.value = JSON.parse(raw)
        hasLocal = true
      }
    } catch {
      hiddenSlugs.value = []
    }
    try {
      const raw = localStorage.getItem(GROUPS_STORAGE_KEY)
      if (raw) {
        menuGroups.value = JSON.parse(raw)
        hasLocal = true
      }
    } catch {
      menuGroups.value = {}
    }
    if (!hasLocal) {
      loadGlobalSettings().catch(() => {
        // Server unavailable or no global settings on initial load — keep defaults
      })
    }
  }

  function persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(hiddenSlugs.value))
    } catch (e) {
      console.warn('Failed to persist sidebar hidden types to localStorage:', e)
    }
  }

  function persistGroups() {
    try {
      localStorage.setItem(GROUPS_STORAGE_KEY, JSON.stringify(menuGroups.value))
    } catch (e) {
      console.warn('Failed to persist sidebar menu groups to localStorage:', e)
    }
  }

  function isVisible(slug: string): boolean {
    return !hiddenSlugs.value.includes(slug)
  }

  function setVisibility(slug: string, visible: boolean) {
    if (visible) {
      hiddenSlugs.value = hiddenSlugs.value.filter((s) => s !== slug)
    } else {
      if (!hiddenSlugs.value.includes(slug)) {
        hiddenSlugs.value = [...hiddenSlugs.value, slug]
      }
    }
    persist()
  }

  function showAll() {
    hiddenSlugs.value = []
    persist()
  }

  function getParent(slug: string): string | undefined {
    return menuGroups.value[slug] || undefined
  }

  function getChildren(parentSlug: string): string[] {
    return Object.entries(menuGroups.value)
      .filter(([, parent]) => parent === parentSlug)
      .map(([child]) => child)
  }

  function isParent(slug: string): boolean {
    return Object.values(menuGroups.value).includes(slug)
  }

  function getDescendants(slug: string): string[] {
    const descendants: string[] = []
    const visited = new Set<string>()
    const stack = getChildren(slug)
    while (stack.length > 0) {
      const current = stack.pop()!
      if (visited.has(current)) continue
      visited.add(current)
      descendants.push(current)
      stack.push(...getChildren(current))
    }
    return descendants
  }

  function getAncestors(slug: string): string[] {
    const ancestors: string[] = []
    const visited = new Set<string>()
    let current = getParent(slug)
    while (current && !visited.has(current)) {
      visited.add(current)
      ancestors.push(current)
      current = getParent(current)
    }
    return ancestors
  }

  function setParent(childSlug: string, parentSlug: string | null) {
    if (parentSlug) {
      // Prevent cycles: reject if parentSlug is a descendant of childSlug
      if (parentSlug === childSlug || getDescendants(childSlug).includes(parentSlug)) {
        return
      }
      menuGroups.value = { ...menuGroups.value, [childSlug]: parentSlug }
    } else {
      const { [childSlug]: _, ...rest } = menuGroups.value
      menuGroups.value = rest
    }
    persistGroups()
  }

  function resetGroups() {
    menuGroups.value = {}
    persistGroups()
  }

  async function loadGlobalSettings() {
    const { getGlobalSettings } = useSidebarSettingsApi()
    const data = await getGlobalSettings()
    hiddenSlugs.value = data.hidden_slugs || []
    menuGroups.value = data.menu_groups || {}
    persist()
    persistGroups()
  }

  async function saveGlobalSettings() {
    const { saveGlobalSettings: save } = useSidebarSettingsApi()
    await save({
      hidden_slugs: hiddenSlugs.value,
      menu_groups: menuGroups.value,
    })
  }

  return {
    hiddenSlugs,
    menuGroups,
    loadSettings,
    isVisible,
    setVisibility,
    showAll,
    getParent,
    getChildren,
    isParent,
    getDescendants,
    getAncestors,
    setParent,
    resetGroups,
    loadGlobalSettings,
    saveGlobalSettings,
  }
}
