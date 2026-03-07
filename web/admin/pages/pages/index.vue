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
      <h2>Pages</h2>
      <NuxtLink to="/pages/create">
        <a-button type="primary">Create Page</a-button>
      </NuxtLink>
    </div>
    <PageTable
      :items="items"
      :loading="loading"
      :has-more="hasMore"
      @delete="handleDelete"
      @load-more="loadMore"
    />
  </div>
</template>

<script setup lang="ts">
const { listPages, deletePage } = usePageApi()

const items = ref<any[]>([])
const loading = ref(false)
const hasMore = ref(false)
const cursor = ref('')

async function load() {
  loading.value = true
  try {
    const res = await listPages(cursor.value)
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
  await deletePage(id)
  items.value = items.value.filter((i) => i.id !== id)
}

onMounted(load)
</script>
