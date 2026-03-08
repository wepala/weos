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
  <a-layout style="min-height: 100vh">
    <a-layout-sider v-model:collapsed="collapsed" collapsible>
      <div
        style="
          height: 32px;
          margin: 16px;
          color: #fff;
          font-size: 18px;
          font-weight: bold;
          text-align: center;
          line-height: 32px;
        "
      >
        {{ collapsed ? 'W' : 'WeOS' }}
      </div>
      <a-menu v-model:selectedKeys="selectedKeys" theme="dark" mode="inline">
        <a-menu-item key="dashboard">
          <NuxtLink to="/">Dashboard</NuxtLink>
        </a-menu-item>
        <a-menu-item
          v-for="rt in visibleResourceTypes"
          :key="`rt-${rt.slug}`"
        >
          <NuxtLink :to="`/resources/${rt.slug}`">{{ rt.name }}</NuxtLink>
        </a-menu-item>
        <a-menu-item key="persons">
          <NuxtLink to="/persons">Persons</NuxtLink>
        </a-menu-item>
        <a-menu-item key="organizations">
          <NuxtLink to="/organizations">Organizations</NuxtLink>
        </a-menu-item>
        <a-menu-divider />
        <a-menu-item key="settings">
          <NuxtLink to="/settings">Settings</NuxtLink>
        </a-menu-item>
      </a-menu>
    </a-layout-sider>
    <a-layout>
      <a-layout-header style="background: #fff; padding: 0 24px">
        <a-breadcrumb style="line-height: 64px">
          <a-breadcrumb-item>
            <NuxtLink to="/">Home</NuxtLink>
          </a-breadcrumb-item>
        </a-breadcrumb>
      </a-layout-header>
      <a-layout-content style="margin: 24px 16px; padding: 24px; background: #fff">
        <slot />
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<script setup lang="ts">
const collapsed = ref(false)
const route = useRoute()
const { resourceTypes, fetchResourceTypes } = useResourceTypeStore()
const { loadSettings, isVisible } = useSidebarSettings()

const visibleResourceTypes = computed(() =>
  resourceTypes.value.filter((rt) => isVisible(rt.slug))
)

const selectedKeys = computed(() => {
  const path = route.path
  if (path.startsWith('/persons')) return ['persons']
  if (path.startsWith('/organizations')) return ['organizations']
  if (path.startsWith('/settings')) return ['settings']
  if (path.startsWith('/resources/')) {
    const slug = route.params.typeSlug as string
    if (slug) return [`rt-${slug}`]
  }
  return ['dashboard']
})

onMounted(() => {
  loadSettings()
  fetchResourceTypes()
})
</script>
