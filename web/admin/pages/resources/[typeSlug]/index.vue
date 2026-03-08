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
      <NuxtLink :to="`/resources/${typeSlug}/create`">
        <a-button type="primary">Create {{ resourceType?.name || typeSlug }}</a-button>
      </NuxtLink>
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

const items = ref<any[]>([])
const loading = ref(false)
const hasMore = ref(false)
const cursor = ref('')

const resourceType = computed(() => getBySlug(typeSlug))

const columns = computed(() => {
  const schema = resourceType.value?.schema
  if (schema) return schemaToColumns(schema)
  return [{ title: 'Name', dataIndex: 'name', key: 'name' }]
})

async function load() {
  loading.value = true
  try {
    const res = await list(cursor.value)
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
  await load()
})
</script>
