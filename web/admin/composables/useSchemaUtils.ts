// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import dayjs from 'dayjs'

export interface FieldDescriptor {
  key: string
  label: string
  inputType: 'input' | 'number' | 'date' | 'time' | 'checkbox' | 'textarea' | 'select' | 'resource-select'
  required: boolean
  format?: string
  options?: string[]
  resourceType?: string
}

export interface ColumnDescriptor {
  title: string
  dataIndex: string
  key: string
  resourceType?: string
  format?: string
  sorter?: (a: any, b: any) => number
  filters?: { text: string; value: string }[]
  onFilter?: (value: string, record: any) => boolean
}

export function useSchemaUtils() {
  function humanizeKey(key: string): string {
    return key
      .replace(/([a-z])([A-Z])/g, '$1 $2')
      .replace(/[_-]/g, ' ')
      .replace(/\b\w/g, (c) => c.toUpperCase())
  }

  function schemaToColumns(schema: any, max = 4): ColumnDescriptor[] {
    if (!schema?.properties) return []

    const props = schema.properties
    const keys = Object.keys(props).filter(
      (k) => !k.startsWith('@') && k !== 'id' && k !== 'type',
    )

    return keys.slice(0, max).map((key) => {
      const prop = props[key]
      const col: ColumnDescriptor = {
        title: prop['x-resource-type'] ? humanizeKey(key.replace(/Id$/, '')) : humanizeKey(key),
        dataIndex: key,
        key,
      }

      if (prop['x-resource-type']) {
        col.resourceType = prop['x-resource-type']
      }

      if (prop.format) {
        col.format = prop.format
      }

      if (prop.type === 'number' || prop.type === 'integer') {
        col.sorter = (a: any, b: any) => (a[key] ?? 0) - (b[key] ?? 0)
      } else {
        col.sorter = (a: any, b: any) =>
          String(a[key] ?? '').localeCompare(String(b[key] ?? ''))
      }

      if (prop.enum) {
        col.filters = prop.enum.map((v: string) => ({ text: v, value: v }))
        col.onFilter = (value: string, record: any) => record[key] === value
      }

      return col
    })
  }

  function schemaToFields(schema: any): FieldDescriptor[] {
    if (!schema?.properties) return []

    const props = schema.properties
    const required = schema.required || []
    const textareaKeys = ['description', 'body', 'content', 'notes', 'bio', 'summary']

    return Object.keys(props)
      .filter((k) => !k.startsWith('@') && k !== 'id' && k !== 'type')
      .map((key) => {
        const prop = props[key]
        let inputType: FieldDescriptor['inputType'] = 'input'

        if (prop['x-resource-type']) {
          inputType = 'resource-select'
        } else if (prop.enum) {
          inputType = 'select'
        } else if (prop.type === 'number' || prop.type === 'integer') {
          inputType = 'number'
        } else if (prop.type === 'boolean') {
          inputType = 'checkbox'
        } else if (prop.format === 'date' || prop.format === 'date-time') {
          inputType = 'date'
        } else if (prop.format === 'time') {
          inputType = 'time'
        } else if (textareaKeys.includes(key.toLowerCase()) || prop.maxLength > 200) {
          inputType = 'textarea'
        }

        const label = inputType === 'resource-select'
          ? humanizeKey(key.replace(/Id$/, ''))
          : humanizeKey(key)

        return {
          key,
          label,
          inputType,
          required: required.includes(key),
          format: prop.format,
          options: prop.enum,
          resourceType: prop['x-resource-type'],
        }
      })
  }

  function buildFormModel(schema: any, initialData?: Record<string, any>): Record<string, any> {
    const model: Record<string, any> = {}
    if (!schema?.properties) return model

    for (const [key, prop] of Object.entries(schema.properties) as [string, any][]) {
      if (key.startsWith('@') || key === 'id' || key === 'type') continue

      if (initialData && key in initialData) {
        const val = initialData[key]
        if (val && (prop.format === 'date' || prop.format === 'date-time')) {
          model[key] = dayjs(val)
        } else if (val && prop.format === 'time') {
          model[key] = dayjs(val, 'HH:mm')
        } else {
          model[key] = val
        }
      } else if (prop.default !== undefined) {
        model[key] = prop.default
      } else if (prop.type === 'number' || prop.type === 'integer') {
        model[key] = null
      } else if (prop.type === 'boolean') {
        model[key] = false
      } else {
        model[key] = ''
      }
    }

    return model
  }

  function serializeFormModel(schema: any, data: Record<string, any>): Record<string, any> {
    const result: Record<string, any> = {}
    if (!schema?.properties) return { ...data }

    for (const [key, val] of Object.entries(data)) {
      const prop = schema.properties[key]
      if (prop && val && dayjs.isDayjs(val)) {
        if (prop.format === 'time') {
          result[key] = val.format('HH:mm')
        } else {
          result[key] = val.toISOString()
        }
      } else if (val === '' || val === null || val === undefined) {
        // Omit empty values to avoid enum validation failures
      } else {
        result[key] = val
      }
    }
    return result
  }

  return { humanizeKey, schemaToColumns, schemaToFields, buildFormModel, serializeFormModel }
}
