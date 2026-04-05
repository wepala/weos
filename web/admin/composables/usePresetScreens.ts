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

import type { Component } from 'vue'

export interface ScreenMeta {
  name: string
  label: string
  icon?: string
}

export interface LoadedScreen {
  fileName: string
  component: Component
  meta: ScreenMeta
}

interface PresetListEntry {
  name: string
  description: string
  types: string[]
  screens?: Record<string, string[]>
}

interface SlugMapping {
  preset: string
  files: string[]
}

export function usePresetScreens() {
  const manifest = useState<Record<string, SlugMapping>>('presetScreenManifest', () => ({}))
  const manifestLoaded = useState<boolean>('presetScreenManifestLoaded', () => false)
  const loadedScreens = useState<Record<string, LoadedScreen>>('presetLoadedScreens', () => ({}))

  async function fetchManifest() {
    if (manifestLoaded.value) return
    try {
      const res = await $fetch<{ data: PresetListEntry[] }>('/api/resource-types/presets')
      const mapping: Record<string, SlugMapping> = {}
      for (const preset of res.data) {
        if (!preset.screens) continue
        for (const [slug, files] of Object.entries(preset.screens)) {
          // Last preset wins if multiple presets provide screens for the same slug.
          mapping[slug] = { preset: preset.name, files }
        }
      }
      manifest.value = mapping
      manifestLoaded.value = true
    } catch (err) {
      console.error('[usePresetScreens] fetchManifest failed:', err)
    }
  }

  function hasScreens(typeSlug: string): boolean {
    const entry = manifest.value[typeSlug]
    return !!entry && entry.files.length > 0
  }

  function getAvailableScreens(typeSlug: string): { preset: string; file: string }[] {
    const entry = manifest.value[typeSlug]
    if (!entry) return []
    return entry.files.map(file => ({ preset: entry.preset, file }))
  }

  async function loadScreen(typeSlug: string, fileName: string): Promise<LoadedScreen | null> {
    const cacheKey = `${typeSlug}/${fileName}`
    if (loadedScreens.value[cacheKey]) return loadedScreens.value[cacheKey]

    const entry = manifest.value[typeSlug]
    if (!entry) return null

    let blobUrl: string | null = null
    try {
      const url = `/api/resource-types/presets/${entry.preset}/screens/${typeSlug}/${fileName}`
      const text = await $fetch<string>(url, { responseType: 'text' }).catch((err: any) => {
        console.warn(`[usePresetScreens] loadScreen failed for ${url}:`, err?.statusCode || err)
        return null
      })
      if (!text) return null
      const blob = new Blob([text], { type: 'text/javascript' })
      blobUrl = URL.createObjectURL(blob)

      const mod = await import(/* @vite-ignore */ blobUrl)
      // Defer revocation to avoid racing with lazy module evaluation.
      setTimeout(() => URL.revokeObjectURL(blobUrl!), 5000)
      const baseName = fileName.replace('.mjs', '')
      const screen: LoadedScreen = {
        fileName,
        component: mod.default,
        meta: mod.meta || { name: baseName, label: baseName },
      }
      loadedScreens.value[cacheKey] = screen
      return screen
    } catch (err) {
      if (blobUrl) URL.revokeObjectURL(blobUrl)
      console.error(`[usePresetScreens] loadScreen failed for ${typeSlug}/${fileName}:`, err)
      return null
    }
  }

  async function loadAllScreens(typeSlug: string): Promise<LoadedScreen[]> {
    const available = getAvailableScreens(typeSlug)
    const results: LoadedScreen[] = []
    for (const { file } of available) {
      const screen = await loadScreen(typeSlug, file)
      if (screen) results.push(screen)
    }
    return results
  }

  return {
    fetchManifest,
    hasScreens,
    getAvailableScreens,
    loadScreen,
    loadAllScreens,
    manifestLoaded,
  }
}
