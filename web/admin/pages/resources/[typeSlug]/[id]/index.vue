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
    <a-page-header
      :title="`${resourceType?.name || typeSlug} Details`"
      @back="router.back()"
    >
      <template #extra>
        <a-space>
          <a-dropdown v-if="screens.length">
            <a-button>Views</a-button>
            <template #overlay>
              <a-menu>
                <a-menu-item v-for="s in screens" :key="s.file">
                  <NuxtLink :to="`/resources/${typeSlug}/${id}/screens/${s.file.replace('.mjs', '')}`">
                    {{ s.label }}
                  </NuxtLink>
                </a-menu-item>
              </a-menu>
            </template>
          </a-dropdown>
          <NuxtLink :to="`/resources/${typeSlug}/${id}/edit`">
            <a-button type="primary">Edit</a-button>
          </NuxtLink>
        </a-space>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-result
      v-else-if="error"
      status="error"
      :title="error"
      sub-title="The requested resource could not be loaded."
    >
      <template #extra>
        <NuxtLink :to="`/resources/${typeSlug}`">
          <a-button type="primary">Back to List</a-button>
        </NuxtLink>
      </template>
    </a-result>
    <template v-else-if="resource">
      <a-descriptions bordered :column="1">
        <a-descriptions-item label="ID">{{ resource.id }}</a-descriptions-item>
        <a-descriptions-item
          v-for="field in fields"
          :key="field.key"
          :label="field.label"
        >
          <template v-if="field.inputType === 'checkbox'">
            {{ resource[field.key] ? 'Yes' : 'No' }}
          </template>
          <template v-else-if="field.inputType === 'resource-select' && field.resourceType && resource[field.key]">
            {{ resource[field.key + 'Display'] || resource[field.key] }}
          </template>
          <template v-else>
            {{ resource[field.key] || '-' }}
          </template>
        </a-descriptions-item>
      </a-descriptions>

      <template v-for="child in childSections" :key="child.slug">
        <a-divider />
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px">
          <h3 style="margin: 0">{{ child.name }}</h3>
          <NuxtLink :to="`/resources/${child.slug}`">
            <a-button size="small">View All</a-button>
          </NuxtLink>
        </div>
        <TaskChecklist
          v-if="child.slug === 'task'"
          :project-id="id"
          :items="child.items"
          :loading="child.loading"
        />
        <ResourceTable
          v-else
          :items="child.items"
          :columns="child.columns"
          :loading="child.loading"
          :has-more="child.hasMore"
          :type-slug="child.slug"
          @load-more="child.loadMore()"
        />
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const typeSlug = computed(() => route.params.typeSlug as string)
const id = computed(() => route.params.id as string)

const { resourceTypes, getBySlug, fetchResourceTypes, loaded } = useResourceTypeStore()
const { schemaToFields, schemaToColumns } = useSchemaUtils()
const { getChildren } = useSidebarSettings()
const { fetchManifest, getAvailableScreens } = usePresetScreens()

const resource = ref<any>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const screens = ref<{ file: string; label: string }[]>([])

const resourceType = computed(() => getBySlug(typeSlug.value))

const fields = computed(() => {
  const schema = resourceType.value?.schema
  if (schema) return schemaToFields(schema)
  return []
})

interface ChildSection {
  slug: string
  name: string
  columns: Record<string, any>[]
  items: any[]
  loading: boolean
  hasMore: boolean
  cursor: string
  loadMore: () => void
}

const childSections = ref<ChildSection[]>([])

function discoverChildSlugs(): string[] {
  const slug = typeSlug.value
  // First try sidebar settings
  const configured = getChildren(slug)
  if (configured.length) return configured
  // Fallback: scan all resource types for x-resource-type references to this type
  const discovered: string[] = []
  for (const rt of resourceTypes.value) {
    if (rt.slug === slug || !rt.schema?.properties) continue
    for (const prop of Object.values(rt.schema.properties) as any[]) {
      if (prop['x-resource-type'] === slug) {
        discovered.push(rt.slug)
        break
      }
    }
  }
  return discovered
}

function initChildSections() {
  const childSlugs = discoverChildSlugs()
  if (!childSlugs.length) return

  const sections: ChildSection[] = []
  for (const slug of childSlugs) {
    const childType = getBySlug(slug)
    const columns = childType?.schema ? schemaToColumns(childType.schema) : []
    const section: ChildSection = reactive({
      slug,
      name: childType?.name || slug,
      columns,
      items: [],
      loading: false,
      hasMore: false,
      cursor: '',
      loadMore: () => fetchChildResources(section),
    })
    sections.push(section)
  }
  childSections.value = sections

  for (const section of sections) {
    fetchChildResources(section)
  }
}

function findReferenceField(childSlug: string): string | undefined {
  const slug = typeSlug.value
  const childType = getBySlug(childSlug)
  if (!childType?.schema?.properties) return undefined
  // Check for x-resource-type annotation first
  for (const [key, prop] of Object.entries(childType.schema.properties) as [string, any][]) {
    if (prop['x-resource-type'] === slug) return key
  }
  // Fallback: look for a field named {camelCaseSlug}Id
  const camelSlug = slug.replace(/-([a-z])/g, (_: string, c: string) => c.toUpperCase())
  const fallbackKey = camelSlug + 'Id'
  if (fallbackKey in childType.schema.properties) return fallbackKey
  return undefined
}

async function fetchChildResources(section: ChildSection) {
  section.loading = true
  try {
    const api = useResourceApi(section.slug)
    const refField = findReferenceField(section.slug)
    const filters = refField ? { [refField]: { eq: id.value } } : undefined
    const res = await api.list(section.cursor, 100, '', '', filters)
    section.items = [...section.items, ...res.data]
    section.cursor = res.cursor
    section.hasMore = res.has_more
  } catch {
    // Loading state is cleared in finally; empty section signals failure
  } finally {
    section.loading = false
  }
}

async function loadResource() {
  loading.value = true
  resource.value = null
  error.value = null
  childSections.value = []
  screens.value = []
  if (!loaded.value) await fetchResourceTypes()
  try {
    const { get } = useResourceApi(typeSlug.value)
    resource.value = await get(id.value)
  } catch (err) {
    error.value = 'Failed to load resource'
    return
  } finally {
    loading.value = false
  }
  if (resource.value) {
    initChildSections()
  }
  const manifestOk = await fetchManifest()
  if (manifestOk) {
    screens.value = getAvailableScreens(typeSlug.value).map(s => ({
      file: s.file,
      label: s.file.replace('.mjs', ''),
    }))
  }
}

watch(
  () => [typeSlug.value, id.value],
  () => loadResource(),
  { immediate: true },
)
</script>
