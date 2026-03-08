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
  <a-form
    :model="form"
    layout="vertical"
    @finish="handleSubmit"
  >
    <a-form-item
      v-for="field in fields"
      :key="field.key"
      :label="field.label"
      :name="field.key"
      :rules="field.required ? [{ required: true, message: `${field.label} is required` }] : []"
    >
      <a-input-number
        v-if="field.inputType === 'number'"
        v-model:value="form[field.key]"
        style="width: 100%"
      />
      <a-checkbox
        v-else-if="field.inputType === 'checkbox'"
        v-model:checked="form[field.key]"
      />
      <a-date-picker
        v-else-if="field.inputType === 'date'"
        v-model:value="form[field.key]"
        style="width: 100%"
      />
      <a-select
        v-else-if="field.inputType === 'select'"
        v-model:value="form[field.key]"
      >
        <a-select-option
          v-for="opt in field.options"
          :key="opt"
          :value="opt"
        >
          {{ opt }}
        </a-select-option>
      </a-select>
      <a-textarea
        v-else-if="field.inputType === 'textarea'"
        v-model:value="form[field.key]"
        :rows="3"
      />
      <a-input
        v-else
        v-model:value="form[field.key]"
      />
    </a-form-item>

    <a-form-item>
      <a-space>
        <a-button type="primary" html-type="submit" :loading="submitting">
          {{ isEdit ? 'Update' : 'Create' }}
        </a-button>
        <NuxtLink :to="`/resources/${typeSlug}`">
          <a-button>Cancel</a-button>
        </NuxtLink>
      </a-space>
    </a-form-item>
  </a-form>
</template>

<script setup lang="ts">
import type { FieldDescriptor } from '~/composables/useSchemaUtils'

const props = defineProps<{
  schema: any
  typeSlug: string
  initialData?: Record<string, any>
  isEdit?: boolean
  submitting?: boolean
}>()

const emit = defineEmits<{
  submit: [data: Record<string, any>]
}>()

const { schemaToFields, buildFormModel } = useSchemaUtils()

const fields = computed<FieldDescriptor[]>(() => schemaToFields(props.schema))

const form = reactive(buildFormModel(props.schema, props.initialData))

function handleSubmit() {
  emit('submit', { ...form })
}
</script>
