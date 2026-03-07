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
    <a-row :gutter="16">
      <a-col :span="8">
        <a-card title="Websites" :bordered="false">
          <p style="font-size: 32px; margin: 0">{{ websiteCount }}</p>
          <NuxtLink to="/websites">
            <a-button type="link" style="padding: 0">Manage Websites</a-button>
          </NuxtLink>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card title="Pages" :bordered="false">
          <p style="font-size: 32px; margin: 0">{{ pageCount }}</p>
          <NuxtLink to="/pages">
            <a-button type="link" style="padding: 0">Manage Pages</a-button>
          </NuxtLink>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card title="Sections" :bordered="false">
          <p style="font-size: 32px; margin: 0">{{ sectionCount }}</p>
          <NuxtLink to="/sections">
            <a-button type="link" style="padding: 0">Manage Sections</a-button>
          </NuxtLink>
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<script setup lang="ts">
const { listWebsites } = useWebsiteApi()
const { listPages } = usePageApi()
const { listSections } = useSectionApi()

const websiteCount = ref(0)
const pageCount = ref(0)
const sectionCount = ref(0)

onMounted(async () => {
  try {
    const [w, p, s] = await Promise.all([
      listWebsites('', 1),
      listPages('', 1),
      listSections('', 1),
    ])
    websiteCount.value = w.data.length + (w.has_more ? '+' as any : 0)
    pageCount.value = p.data.length + (p.has_more ? '+' as any : 0)
    sectionCount.value = s.data.length + (s.has_more ? '+' as any : 0)
  } catch {
    // API may not be running yet
  }
})
</script>
