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
    <a-page-header title="Section Details" @back="router.push('/sections')">
      <template #extra>
        <NuxtLink :to="`/sections/${id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-descriptions v-else-if="section" bordered :column="1">
      <a-descriptions-item label="ID">{{ section.id }}</a-descriptions-item>
      <a-descriptions-item label="Name">{{ section.name }}</a-descriptions-item>
      <a-descriptions-item label="Slot">{{ section.slot }}</a-descriptions-item>
      <a-descriptions-item label="Entity Type">
        <a-tag v-if="section.entity_type" color="purple">{{ section.entity_type }}</a-tag>
        <span v-else>-</span>
      </a-descriptions-item>
      <a-descriptions-item label="Content">{{ section.content || '-' }}</a-descriptions-item>
      <a-descriptions-item label="Position">{{ section.position }}</a-descriptions-item>
      <a-descriptions-item label="Created At">{{ section.created_at }}</a-descriptions-item>
    </a-descriptions>
  </div>
</template>

<script setup lang="ts">
const { getSection } = useSectionApi()
const route = useRoute()
const router = useRouter()

const id = route.params.id as string
const section = ref<any>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    section.value = await getSection(id)
  } finally {
    loading.value = false
  }
})
</script>
