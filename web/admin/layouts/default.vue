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
    <!-- Desktop sider -->
    <a-layout-sider
      v-if="!isMobile"
      v-model:collapsed="collapsed"
      collapsible
      breakpoint="lg"
      :collapsed-width="0"
    >
      <div class="sider-logo">{{ collapsed ? 'W' : 'WeOS' }}</div>
      <a-menu v-model:selectedKeys="selectedKeys" v-model:openKeys="openKeys" theme="dark" mode="inline">
        <a-menu-item key="dashboard">
          <NuxtLink to="/">Dashboard</NuxtLink>
        </a-menu-item>
        <SidebarMenuItem v-for="item in menuStructure" :key="item.key" :item="item" />
        <template v-if="isAdminOrOwner">
          <a-menu-item key="persons">
            <NuxtLink to="/persons">Persons</NuxtLink>
          </a-menu-item>
          <a-menu-item key="organizations">
            <NuxtLink to="/organizations">Organizations</NuxtLink>
          </a-menu-item>
          <a-menu-item key="users">
            <NuxtLink to="/users">Users</NuxtLink>
          </a-menu-item>
          <a-menu-divider />
          <a-menu-item key="settings">
            <NuxtLink to="/settings">Settings</NuxtLink>
          </a-menu-item>
        </template>
      </a-menu>
    </a-layout-sider>

    <!-- Mobile drawer -->
    <a-drawer
      v-if="isMobile"
      :open="mobileMenuOpen"
      placement="left"
      :closable="false"
      :width="256"
      :body-style="{ padding: 0, background: '#001529' }"
      @close="mobileMenuOpen = false"
    >
      <div class="sider-logo">WeOS</div>
      <a-menu
        v-model:selectedKeys="selectedKeys"
        v-model:openKeys="openKeys"
        theme="dark"
        mode="inline"
        @click="mobileMenuOpen = false"
      >
        <a-menu-item key="dashboard">
          <NuxtLink to="/">Dashboard</NuxtLink>
        </a-menu-item>
        <SidebarMenuItem v-for="item in menuStructure" :key="item.key" :item="item" />
        <template v-if="isAdminOrOwner">
          <a-menu-item key="persons">
            <NuxtLink to="/persons">Persons</NuxtLink>
          </a-menu-item>
          <a-menu-item key="organizations">
            <NuxtLink to="/organizations">Organizations</NuxtLink>
          </a-menu-item>
          <a-menu-item key="users">
            <NuxtLink to="/users">Users</NuxtLink>
          </a-menu-item>
          <a-menu-divider />
          <a-menu-item key="settings">
            <NuxtLink to="/settings">Settings</NuxtLink>
          </a-menu-item>
        </template>
      </a-menu>
    </a-drawer>

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
        <MenuOutlined v-if="isMobile" class="mobile-menu-trigger" @click="mobileMenuOpen = true" />
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
      <a-alert
        v-if="isImpersonating"
        type="warning"
        banner
        style="text-align: center"
      >
        <template #message>
          Viewing as <strong>{{ user?.name || user?.email }}</strong>
          <a-button size="small" type="link" danger style="margin-left: 8px" @click="stopImpersonation">
            Stop Impersonating
          </a-button>
        </template>
      </a-alert>
      <a-layout-content style="margin: 24px 16px; padding: 24px; background: #fff">
        <slot />
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<script setup lang="ts">
import { MenuOutlined } from '@ant-design/icons-vue'

interface MenuItem {
  key: string
  slug: string
  name: string
  children?: MenuItem[]
}

const { user, logout, isImpersonating, stopImpersonation } = useAuth()
const isAdminOrOwner = computed(() => {
  const role = user.value?.role
  return role === 'admin' || role === 'owner' || !role
})
const collapsed = ref(false)
const openKeys = ref<string[]>([])
const isMobile = ref(false)
const mobileMenuOpen = ref(false)
const MOBILE_BREAKPOINT = 992

function updateIsMobile() {
  isMobile.value = window.innerWidth < MOBILE_BREAKPOINT
  if (!isMobile.value) mobileMenuOpen.value = false
}
const route = useRoute()
const { resourceTypes, fetchResourceTypes } = useResourceTypeStore()
const { loadSettings, isVisible, getParent, getChildren, getAncestors } = useSidebarSettings()

// Re-fetch resource types and sidebar settings when user role changes
// (e.g., impersonation start/stop).
watch(() => user.value?.role, async (newRole, oldRole) => {
  if (newRole !== oldRole && oldRole !== undefined) {
    await fetchResourceTypes()
    await loadSettings()
  }
})

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
  if (path.startsWith('/users')) return ['users']
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
  updateIsMobile()
  window.addEventListener('resize', updateIsMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', updateIsMobile)
})
</script>

<style scoped>
.sider-logo {
  height: 32px;
  margin: 16px;
  color: #fff;
  font-size: 18px;
  font-weight: bold;
  text-align: center;
  line-height: 32px;
}

.mobile-menu-trigger {
  font-size: 20px;
  cursor: pointer;
  margin-right: 16px;
}
</style>
