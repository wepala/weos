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
    <h2>Dashboard</h2>
    <a-row :gutter="[16, 16]">
      <a-col
        v-for="rt in resourceTypes"
        :key="rt.slug"
        :xs="24"
        :sm="12"
        :md="8"
      >
        <a-card :title="rt.name" :bordered="false">
          <p v-if="rt.description" style="color: #888; margin-bottom: 8px">
            {{ rt.description }}
          </p>
          <NuxtLink :to="`/resources/${rt.slug}`">
            <a-button type="link" style="padding: 0">Manage {{ rt.name }}</a-button>
          </NuxtLink>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :md="8">
        <a-card title="Persons" :bordered="false">
          <NuxtLink to="/persons">
            <a-button type="link" style="padding: 0">Manage Persons</a-button>
          </NuxtLink>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :md="8">
        <a-card title="Organizations" :bordered="false">
          <NuxtLink to="/organizations">
            <a-button type="link" style="padding: 0">Manage Organizations</a-button>
          </NuxtLink>
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<script setup lang="ts">
const { resourceTypes, fetchResourceTypes, loaded } = useResourceTypeStore()

onMounted(async () => {
  if (!loaded.value) await fetchResourceTypes()
})
</script>
