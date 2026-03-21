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
        <SidebarMenuItem v-for="item in menuStructure" :key="item.key" :item="item" />
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
      <a-layout-header
        style="
          background: #fff;
          padding: 0 24px;
          display: flex;
          justify-content: space-between;
          align-items: center;
        "
      >
        <a-breadcrumb style="line-height: 64px">
          <a-breadcrumb-item>
            <NuxtLink to="/">Home</NuxtLink>
          </a-breadcrumb-item>
        </a-breadcrumb>
        <div v-if="user" style="display: flex; align-items: center; gap: 12px">
          <span>{{ user.name || user.email }}</span>
          <a-button size="small" @click="logout">Logout</a-button>
        </div>
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

const { user, logout } = useAuth()
const collapsed = ref(false)
const openKeys = ref<string[]>([])
const route = useRoute()
const { resourceTypes, fetchResourceTypes } = useResourceTypeStore()
const { loadSettings, isVisible, getParent, getChildren, getAncestors } = useSidebarSettings()

const visibleResourceTypes = computed(() =>
  resourceTypes.value.filter((rt) => isVisible(rt.slug))
)

const menuStructure = computed<MenuItem[]>(() => {
  const visible = visibleResourceTypes.value
  const visibleSlugs = new Set(visible.map((rt) => rt.slug))

  function buildTree(parentSlug: string | null, visited = new Set<string>()): MenuItem[] {
    const items: MenuItem[] = []
    for (const rt of visible) {
      if (visited.has(rt.slug)) continue
      const rtParent = getParent(rt.slug)
      const belongsHere = parentSlug
        ? rtParent === parentSlug
        : !rtParent || !visibleSlugs.has(rtParent)
      if (!belongsHere) continue

      const nextVisited = new Set(visited)
      nextVisited.add(rt.slug)
      const children = buildTree(rt.slug, nextVisited)
      items.push({
        key: `rt-${rt.slug}`,
        slug: rt.slug,
        name: rt.name,
        children: children.length > 0 ? children : undefined,
      })
    }
    return items
  }

  return buildTree(null)
})

function findInTree(items: MenuItem[], slug: string): MenuItem | undefined {
  for (const item of items) {
    if (item.slug === slug) return item
    if (item.children) {
      const found = findInTree(item.children, slug)
      if (found) return found
    }
  }
  return undefined
}

const selectedKeys = computed(() => {
  const path = route.path
  if (path.startsWith('/persons')) return ['persons']
  if (path.startsWith('/organizations')) return ['organizations']
  if (path.startsWith('/settings')) return ['settings']
  if (path.startsWith('/resources/')) {
    const slug = route.params.typeSlug as string
    if (slug) {
      const item = findInTree(menuStructure.value, slug)
      if (item?.children?.length) return [`rt-${slug}-index`]
      return [`rt-${slug}`]
    }
  }
  return ['dashboard']
})

watch(selectedKeys, (keys) => {
  if (!keys.length) return
  const key = keys[0]
  if (!key.startsWith('rt-')) return
  const slug = key.slice(3).replace(/-index$/, '')
  const ancestors = getAncestors(slug)
  const newOpenKeys = [...openKeys.value]
  for (const ancestor of ancestors) {
    const ancestorKey = `rt-${ancestor}`
    if (!newOpenKeys.includes(ancestorKey)) {
      newOpenKeys.push(ancestorKey)
    }
  }
  if (newOpenKeys.length !== openKeys.value.length) {
    openKeys.value = newOpenKeys
  }
}, { immediate: true })

onMounted(() => {
  loadSettings()
  fetchResourceTypes()
})
</script>
