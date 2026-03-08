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
      <a-menu v-model:selectedKeys="selectedKeys" v-model:openKeys="openKeys" theme="dark" mode="inline">
        <a-menu-item key="dashboard">
          <NuxtLink to="/">Dashboard</NuxtLink>
        </a-menu-item>
        <template v-for="item in menuStructure" :key="item.key">
          <a-sub-menu v-if="item.children?.length" :key="item.key">
            <template #title>
              <NuxtLink :to="`/resources/${item.slug}`">{{ item.name }}</NuxtLink>
            </template>
            <a-menu-item v-for="child in item.children" :key="child.key">
              <NuxtLink :to="`/resources/${child.slug}`">{{ child.name }}</NuxtLink>
            </a-menu-item>
          </a-sub-menu>
          <a-menu-item v-else :key="item.key">
            <NuxtLink :to="`/resources/${item.slug}`">{{ item.name }}</NuxtLink>
          </a-menu-item>
        </template>
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
interface MenuItem {
  key: string
  slug: string
  name: string
  children?: MenuItem[]
}

const collapsed = ref(false)
const openKeys = ref<string[]>([])
const route = useRoute()
const { resourceTypes, fetchResourceTypes } = useResourceTypeStore()
const { loadSettings, isVisible, getParent, getChildren } = useSidebarSettings()

const visibleResourceTypes = computed(() =>
  resourceTypes.value.filter((rt) => isVisible(rt.slug))
)

const menuStructure = computed<MenuItem[]>(() => {
  const visible = visibleResourceTypes.value
  const visibleSlugs = new Set(visible.map((rt) => rt.slug))
  const childSlugs = new Set<string>()

  for (const rt of visible) {
    const parent = getParent(rt.slug)
    if (parent && visibleSlugs.has(parent)) {
      childSlugs.add(rt.slug)
    }
  }

  const items: MenuItem[] = []
  for (const rt of visible) {
    if (childSlugs.has(rt.slug)) continue

    const children = getChildren(rt.slug)
      .filter((s) => visibleSlugs.has(s))
      .map((s) => {
        const child = visible.find((r) => r.slug === s)!
        return { key: `rt-${s}`, slug: s, name: child.name }
      })

    items.push({
      key: `rt-${rt.slug}`,
      slug: rt.slug,
      name: rt.name,
      children: children.length > 0 ? children : undefined,
    })
  }
  return items
})

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

watch(selectedKeys, (keys) => {
  if (!keys.length) return
  const key = keys[0]
  if (!key.startsWith('rt-')) return
  const slug = key.slice(3)
  const parent = getParent(slug)
  if (parent && isVisible(parent)) {
    const parentKey = `rt-${parent}`
    if (!openKeys.value.includes(parentKey)) {
      openKeys.value = [...openKeys.value, parentKey]
    }
  }
}, { immediate: true })

onMounted(() => {
  loadSettings()
  fetchResourceTypes()
})
</script>
