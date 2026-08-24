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
  <!--
    `multiple` follows the schema: a property declared `type: "array"` with
    x-resource-type holds a LIST of references, and a single-select silently
    collapsed it to one entry on every save (issue #513).
  -->
  <a-select
    :value="multiple ? asList(value) : (value || undefined)"
    :mode="multiple ? 'multiple' : undefined"
    show-search
    allow-clear
    :placeholder="`Select ${typeSlug}`"
    :loading="loading"
    :options="options"
    :filter-option="filterOption"
    style="width: 100%"
    @update:value="$emit('update:value', normalize($event))"
  />
</template>

<script setup lang="ts">
const props = defineProps<{
  typeSlug: string
  value?: string | string[]
  multiple?: boolean
}>()

defineEmits<{
  'update:value': [value: string | string[]]
}>()

// A multi-select must emit an array even when nothing is chosen, or a cleared
// field would submit `""` where the schema requires a list and the write would
// be rejected.
// A row written before list references were stored as arrays still holds a
// scalar. Coercing that to [] would render the field empty and wipe the
// reference on the next save, so a scalar becomes a one-element list instead.
function asList(current: string | string[] | undefined): string[] {
  if (Array.isArray(current)) return current
  return current ? [current] : []
}

function normalize(next: unknown): string | string[] {
  if (props.multiple) {
    return Array.isArray(next) ? (next as string[]) : []
  }
  return (next as string) ?? ''
}

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
