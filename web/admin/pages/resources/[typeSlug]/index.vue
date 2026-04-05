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
    <div class="page-header">
      <h2>{{ resourceType?.name || typeSlug }}</h2>
      <a-space>
        <a-dropdown v-if="screens.length">
          <a-button>Views</a-button>
          <template #overlay>
            <a-menu>
              <a-menu-item v-for="s in screens" :key="s.file">
                <NuxtLink :to="`/resources/${typeSlug}/screens/${s.file.replace('.mjs', '')}`">
                  {{ s.label }}
                </NuxtLink>
              </a-menu-item>
            </a-menu>
          </template>
        </a-dropdown>
        <NuxtLink :to="`/resources/${typeSlug}/create`">
          <a-button type="primary">Create {{ resourceType?.name || typeSlug }}</a-button>
        </NuxtLink>
      </a-space>
    </div>
    <div v-if="referenceFilters.length" style="margin-bottom: 16px; display: flex; gap: 12px; flex-wrap: wrap">
      <div v-for="rf in referenceFilters" :key="rf.field" style="min-width: 200px">
        <a-select
          v-model:value="activeFilters[rf.field]"
          :placeholder="`Filter by ${rf.label}`"
          allow-clear
          show-search
          :filter-option="filterOption"
          style="width: 100%"
          :aria-label="`Filter by ${rf.label}`"
          @change="applyFilters"
        >
          <a-select-option v-for="opt in rf.options" :key="opt.value" :value="opt.value">
            {{ opt.label }}
          </a-select-option>
        </a-select>
      </div>
    </div>
    <ResourceTable
      :items="items"
      :columns="columns"
      :loading="loading"
      :has-more="hasMore"
      :type-slug="typeSlug"
      @delete="handleDelete"
      @load-more="loadMore"
    />
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const typeSlug = route.params.typeSlug as string

const { getBySlug, fetchResourceTypes, loaded } = useResourceTypeStore()
const { list, remove } = useResourceApi(typeSlug)
const { schemaToColumns } = useSchemaUtils()
const { fetchManifest, getAvailableScreens } = usePresetScreens()

const items = ref<any[]>([])
const loading = ref(false)
const hasMore = ref(false)
const cursor = ref('')
const screens = ref<{ file: string; label: string }[]>([])

const resourceType = computed(() => getBySlug(typeSlug))

const columns = computed(() => {
  const schema = resourceType.value?.schema
  if (schema) return schemaToColumns(schema)
  return [{ title: 'Name', dataIndex: 'name', key: 'name' }]
})

// --- Reference field filters ---
interface RefFilter {
  field: string
  label: string
  resourceType: string
  options: { value: string; label: string }[]
}

const referenceFilters = ref<RefFilter[]>([])
const activeFilters = reactive<Record<string, string | undefined>>({})

function filterOption(input: string, option: any) {
  return (option?.children?.[0]?.children || option?.label || '')
    .toString()
    .toLowerCase()
    .includes(input.toLowerCase())
}

async function initReferenceFilters() {
  const schema = resourceType.value?.schema
  if (!schema?.properties) return

  const filters: RefFilter[] = []
  for (const [key, prop] of Object.entries(schema.properties) as [string, any][]) {
    if (!prop['x-resource-type']) continue
    const refSlug = prop['x-resource-type']
    const refApi = useResourceApi(refSlug)
    try {
      const res = await refApi.list('', 100)
      const label = key.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/\b\w/g, c => c.toUpperCase())
      filters.push({
        field: key,
        label,
        resourceType: refSlug,
        options: res.data.map((item: any) => ({
          value: item.id,
          label: item.name || item.id,
        })),
      })
    } catch {
      // Skip filters for types that can't be loaded
    }
  }
  referenceFilters.value = filters
}

async function applyFilters() {
  // Reset pagination and reload with filters
  items.value = []
  cursor.value = ''
  hasMore.value = false
  await load()
}

async function load() {
  loading.value = true
  try {
    const filters: Record<string, Record<string, string>> = {}
    for (const [field, value] of Object.entries(activeFilters)) {
      if (value) {
        filters[field] = { eq: value }
      }
    }
    const hasFilters = Object.keys(filters).length > 0
    const res = await list(cursor.value, 20, '', '', hasFilters ? filters : undefined)
    items.value = [...items.value, ...res.data]
    hasMore.value = res.has_more
    cursor.value = res.cursor
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  await load()
}

async function handleDelete(id: string) {
  await remove(id)
  items.value = items.value.filter((i) => i.id !== id)
}

onMounted(async () => {
  if (!loaded.value) await fetchResourceTypes()
  await initReferenceFilters()
  await load()
  await fetchManifest()
  screens.value = getAvailableScreens(typeSlug).map(s => ({
    file: s.file,
    label: s.file.replace('.mjs', ''),
  }))
})
</script>
