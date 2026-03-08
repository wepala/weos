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
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (raw) {
        hiddenSlugs.value = JSON.parse(raw)
      }
    } catch {
      hiddenSlugs.value = []
    }
    try {
      const raw = localStorage.getItem(GROUPS_STORAGE_KEY)
      if (raw) {
        menuGroups.value = JSON.parse(raw)
      }
    } catch {
      menuGroups.value = {}
    }
  }

  function persist() {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(hiddenSlugs.value))
  }

  function persistGroups() {
    localStorage.setItem(GROUPS_STORAGE_KEY, JSON.stringify(menuGroups.value))
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

  function setParent(childSlug: string, parentSlug: string | null) {
    if (parentSlug) {
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
    setParent,
    resetGroups,
  }
}
