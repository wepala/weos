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
      @back="router.push(`/resources/${typeSlug}`)"
    >
      <template #extra>
        <NuxtLink :to="`/resources/${typeSlug}/${id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
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
        <ResourceTable
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
const typeSlug = route.params.typeSlug as string
const id = route.params.id as string

const { getBySlug, fetchResourceTypes, loaded } = useResourceTypeStore()
const { get } = useResourceApi(typeSlug)
const { schemaToFields, schemaToColumns } = useSchemaUtils()
const { getChildren } = useSidebarSettings()

const resource = ref<any>(null)
const loading = ref(true)

const resourceType = computed(() => getBySlug(typeSlug))

const fields = computed(() => {
  const schema = resourceType.value?.schema
  if (schema) return schemaToFields(schema)
  return []
})

interface ChildSection {
  slug: string
  name: string
  columns: { title: string; dataIndex: string; key: string }[]
  items: any[]
  loading: boolean
  hasMore: boolean
  cursor: string
  parentField: string | null
  loadMore: () => void
}

function findParentField(childSchema: any, parentSlug: string): string | null {
  const properties = childSchema?.properties
  if (!properties) return null

  const match = Object.entries(properties)
    .find(([_, prop]) => (prop as any)['x-resource-type'] === parentSlug)
  if (match) return match[0]

  const camelSlug = parentSlug.replace(/-([a-z])/g, (_: string, c: string) => c.toUpperCase())
  const camelKey = `${camelSlug}Id`
  if (camelKey in properties) return camelKey

  const snakeKey = `${parentSlug.replace(/-/g, '_')}_id`
  if (snakeKey in properties) return snakeKey

  return null
}

const childSections = ref<ChildSection[]>([])

function initChildSections() {
  const childSlugs = getChildren(typeSlug)
  if (!childSlugs.length) return

  const sections: ChildSection[] = []
  for (const slug of childSlugs) {
    const childType = getBySlug(slug)
    const columns = childType?.schema ? schemaToColumns(childType.schema) : []
    const parentField = childType?.schema ? findParentField(childType.schema, typeSlug) : null
    if (!parentField) {
      console.warn(
        `Child type "${slug}" has no parent field referencing "${typeSlug}". ` +
        `Child resources will not be filtered by parent.`,
      )
    }
    const section: ChildSection = reactive({
      slug,
      name: childType?.name || slug,
      columns,
      items: [],
      loading: false,
      hasMore: false,
      cursor: '',
      parentField,
      loadMore: () => fetchChildResources(section),
    })
    sections.push(section)
  }
  childSections.value = sections

  for (const section of sections) {
    fetchChildResources(section)
  }
}

async function fetchChildResources(section: ChildSection) {
  section.loading = true
  try {
    const api = useResourceApi(section.slug)
    const parentField = section.parentField
    let cursor = section.cursor
    let hasMore = true
    const accumulated: any[] = []

    // When filtering client-side, keep fetching pages until we find matching
    // items or exhaust the server's data. Without this loop a page of
    // non-matching items would show an empty list with a misleading "Load More".
    do {
      const res = await api.list(cursor)
      const items = parentField
        ? res.data.filter((item: any) => String(item[parentField] ?? '') === String(id))
        : res.data
      accumulated.push(...items)
      cursor = res.cursor
      hasMore = res.has_more
    } while (parentField && accumulated.length === 0 && hasMore)

    section.items = [...section.items, ...accumulated]
    section.cursor = cursor
    section.hasMore = hasMore
  } catch (err) {
    console.error(`Failed to fetch child resources for "${section.slug}":`, err)
  } finally {
    section.loading = false
  }
}

onMounted(async () => {
  if (!loaded.value) await fetchResourceTypes()
  try {
    resource.value = await get(id)
  } finally {
    loading.value = false
  }
  if (resource.value) {
    initChildSections()
  }
})
</script>
