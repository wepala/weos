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
      <h2>Sections</h2>
      <NuxtLink to="/sections/create">
        <a-button type="primary">Create Section</a-button>
      </NuxtLink>
    </div>
    <SectionTable
      :items="items"
      :loading="loading"
      :has-more="hasMore"
      @delete="handleDelete"
      @load-more="loadMore"
    />
  </div>
</template>

<script setup lang="ts">
const { listSections, deleteSection } = useSectionApi()

const items = ref<any[]>([])
const loading = ref(false)
const hasMore = ref(false)
const cursor = ref('')

async function load() {
  loading.value = true
  try {
    const res = await listSections(cursor.value)
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
  await deleteSection(id)
  items.value = items.value.filter((i) => i.id !== id)
}

onMounted(load)
</script>
