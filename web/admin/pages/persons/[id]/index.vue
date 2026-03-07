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
    <a-page-header title="Person Details" @back="() => $router.push('/persons')">
      <template #extra>
        <NuxtLink :to="`/persons/${route.params.id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <a-descriptions v-else-if="person" bordered :column="1">
      <a-descriptions-item label="ID">{{ person.id }}</a-descriptions-item>
      <a-descriptions-item label="Given Name">{{ person.given_name }}</a-descriptions-item>
      <a-descriptions-item label="Family Name">{{ person.family_name }}</a-descriptions-item>
      <a-descriptions-item label="Name">{{ person.name }}</a-descriptions-item>
      <a-descriptions-item label="Email">{{ person.email }}</a-descriptions-item>
      <a-descriptions-item label="Avatar URL">{{ person.avatar_url || '—' }}</a-descriptions-item>
      <a-descriptions-item label="Status">
        <a-tag :color="person.status === 'active' ? 'green' : 'red'">{{ person.status }}</a-tag>
      </a-descriptions-item>
      <a-descriptions-item label="Created At">{{ person.created_at }}</a-descriptions-item>
    </a-descriptions>
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const { getPerson } = usePersonApi()

const person = ref<any>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    person.value = await getPerson(route.params.id as string)
  } finally {
    loading.value = false
  }
})
</script>
