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
  <a-select
    :value="value || undefined"
    show-search
    allow-clear
    :placeholder="`Select ${typeSlug}`"
    :loading="loading"
    :options="options"
    :filter-option="filterOption"
    style="width: 100%"
    @update:value="$emit('update:value', $event ?? '')"
  />
</template>

<script setup lang="ts">
const props = defineProps<{
  typeSlug: string
  value?: string
}>()

defineEmits<{
  'update:value': [value: string]
}>()

const items = ref<any[]>([])
const loading = ref(false)

const options = computed(() =>
  items.value.map((item) => ({
    value: item.id,
    label: item.name || item.id,
  })),
)

function filterOption(input: string, option: any) {
  return String(option.label || '').toLowerCase().includes(input.toLowerCase())
}

onMounted(async () => {
  loading.value = true
  try {
    const { list } = useResourceApi(props.typeSlug)
    const res = await list('', 100)
    items.value = res.data
  } finally {
    loading.value = false
  }
})
</script>
