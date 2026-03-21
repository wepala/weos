<!--
  Copyright (C) 2026 Wepala, LLC

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<template>
  <div>
    <a-page-header title="Settings" />
    <a-card title="Sidebar Menu">
      <template #extra>
        <a-space>
          <a-button :loading="loadingGlobal" @click="handleLoadGlobal">Load Global</a-button>
          <a-button type="primary" :loading="savingGlobal" @click="handleSaveGlobal">Save Globally</a-button>
          <a-button @click="resetGroups">Reset Grouping</a-button>
          <a-button @click="showAll">Show All</a-button>
        </a-space>
      </template>
      <p style="margin-bottom: 16px">
        Choose which resource types appear in the sidebar navigation.
        New resource types are shown by default.
      </p>
      <a-list :data-source="resourceTypes" :loading="!loaded">
        <template #renderItem="{ item }">
          <a-list-item>
            <a-list-item-meta
              :title="item.name"
              :description="item.description"
              :style="{ paddingLeft: `${getDepth(item.slug) * 24}px` }"
            />
            <template #actions>
              <a-select
                :value="getParent(item.slug) || undefined"
                placeholder="Top level"
                allow-clear
                style="width: 150px; margin-right: 8px"
                @change="(val: string) => setParent(item.slug, val || null)"
              >
                <a-select-option
                  v-for="parent in availableParents(item.slug)"
                  :key="parent.slug"
                  :value="parent.slug"
                >
                  {{ parent.name }}
                </a-select-option>
              </a-select>
              <a-switch
                :checked="isVisible(item.slug)"
                @change="(checked: boolean) => setVisibility(item.slug, checked)"
              />
            </template>
          </a-list-item>
        </template>
      </a-list>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'

const { resourceTypes, loaded, fetchResourceTypes } = useResourceTypeStore()
const {
  loadSettings, isVisible, setVisibility, showAll,
  getParent, getAncestors, getDescendants, setParent, resetGroups,
  loadGlobalSettings, saveGlobalSettings,
} = useSidebarSettings()

const savingGlobal = ref(false)
const loadingGlobal = ref(false)

function availableParents(slug: string) {
  const descendants = new Set(getDescendants(slug))
  return resourceTypes.value.filter((rt) => {
    if (rt.slug === slug) return false
    if (descendants.has(rt.slug)) return false
    return true
  })
}

function getDepth(slug: string): number {
  return getAncestors(slug).length
}

async function handleSaveGlobal() {
  savingGlobal.value = true
  try {
    await saveGlobalSettings()
    message.success('Settings saved globally')
  } catch {
    message.error('Failed to save global settings')
  } finally {
    savingGlobal.value = false
  }
}

async function handleLoadGlobal() {
  loadingGlobal.value = true
  try {
    await loadGlobalSettings()
    message.success('Global settings loaded')
  } catch {
    message.error('Failed to load global settings')
  } finally {
    loadingGlobal.value = false
  }
}

onMounted(() => {
  loadSettings()
  if (!loaded.value) {
    fetchResourceTypes()
  }
})
</script>
