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

export interface FieldDescriptor {
  key: string
  label: string
  inputType: 'input' | 'number' | 'date' | 'checkbox' | 'textarea' | 'select'
  required: boolean
  format?: string
  options?: string[]
}

export interface ColumnDescriptor {
  title: string
  dataIndex: string
  key: string
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

    return keys.slice(0, max).map((key) => ({
      title: humanizeKey(key),
      dataIndex: key,
      key,
    }))
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

        if (prop.enum) {
          inputType = 'select'
        } else if (prop.type === 'number' || prop.type === 'integer') {
          inputType = 'number'
        } else if (prop.type === 'boolean') {
          inputType = 'checkbox'
        } else if (prop.format === 'date' || prop.format === 'date-time') {
          inputType = 'date'
        } else if (textareaKeys.includes(key.toLowerCase()) || prop.maxLength > 200) {
          inputType = 'textarea'
        }

        return {
          key,
          label: humanizeKey(key),
          inputType,
          required: required.includes(key),
          format: prop.format,
          options: prop.enum,
        }
      })
  }

  function buildFormModel(schema: any, initialData?: Record<string, any>): Record<string, any> {
    const model: Record<string, any> = {}
    if (!schema?.properties) return model

    for (const [key, prop] of Object.entries(schema.properties) as [string, any][]) {
      if (key.startsWith('@') || key === 'id' || key === 'type') continue

      if (initialData && key in initialData) {
        model[key] = initialData[key]
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

  return { humanizeKey, schemaToColumns, schemaToFields, buildFormModel }
}
