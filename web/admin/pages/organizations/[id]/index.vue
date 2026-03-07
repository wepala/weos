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
    <a-page-header title="Organization Details" @back="() => $router.push('/organizations')">
      <template #extra>
        <NuxtLink :to="`/organizations/${route.params.id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-descriptions v-else-if="org" bordered :column="1">
      <a-descriptions-item label="ID">{{ org.id }}</a-descriptions-item>
      <a-descriptions-item label="Name">{{ org.name }}</a-descriptions-item>
      <a-descriptions-item label="Slug">{{ org.slug }}</a-descriptions-item>
      <a-descriptions-item label="Description">{{ org.description || '—' }}</a-descriptions-item>
      <a-descriptions-item label="URL">{{ org.url || '—' }}</a-descriptions-item>
      <a-descriptions-item label="Logo URL">{{ org.logo_url || '—' }}</a-descriptions-item>
      <a-descriptions-item label="Status">
        <a-tag :color="org.status === 'active' ? 'green' : 'red'">{{ org.status }}</a-tag>
      </a-descriptions-item>
      <a-descriptions-item label="Created At">{{ org.created_at }}</a-descriptions-item>
    </a-descriptions>
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const { getOrganization } = useOrganizationApi()

const org = ref<any>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    org.value = await getOrganization(route.params.id as string)
  } finally {
    loading.value = false
  }
})
</script>
