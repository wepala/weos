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
  <a-sub-menu v-if="item.children?.length" :key="item.key">
    <template #title>{{ item.name }}</template>
    <a-menu-item :key="`${item.key}-index`">
      <NuxtLink :to="`/resources/${item.slug}`">All {{ item.name }}</NuxtLink>
    </a-menu-item>
    <SidebarMenuItem v-for="child in item.children" :key="child.key" :item="child" />
  </a-sub-menu>
  <a-menu-item v-else :key="item.key">
    <NuxtLink :to="`/resources/${item.slug}`">{{ item.name }}</NuxtLink>
  </a-menu-item>
</template>

<script setup lang="ts">
interface MenuItem {
  key: string
  slug: string
  name: string
  children?: MenuItem[]
}

defineProps<{ item: MenuItem }>()
</script>
