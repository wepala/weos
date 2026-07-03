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

<!-- Renders the agent widget contract (docs/_reference/agent-widgets.md).
     Unknown widget types fall back to their raw JSON as preformatted text,
     mirroring the server-side degradation rule. -->
<template>
  <div class="agent-widgets">
    <template v-for="(widget, i) in widgets" :key="i">
      <div v-if="widget.type === 'markdown'" class="widget-markdown">
        <h4 v-if="widget.title">{{ widget.title }}</h4>
        <p class="markdown-body">{{ widget.markdown }}</p>
      </div>

      <div v-else-if="widget.type === 'table'" class="widget-table">
        <h4 v-if="widget.title">{{ widget.title }}</h4>
        <a-table
          :columns="tableColumns(widget)"
          :data-source="tableRows(widget)"
          :pagination="false"
          size="small"
        />
      </div>

      <div v-else-if="widget.type === 'list'" class="widget-list">
        <h4 v-if="widget.title">{{ widget.title }}</h4>
        <ul>
          <li v-for="(item, j) in widget.items" :key="j">{{ item }}</li>
        </ul>
      </div>

      <a-card
        v-else-if="widget.type === 'card'"
        :title="widget.title"
        size="small"
        class="widget-card"
      >
        <p v-if="widget.body">{{ widget.body }}</p>
        <a-descriptions v-if="widget.fields?.length" :column="1" size="small">
          <a-descriptions-item v-for="f in widget.fields" :key="f.label" :label="f.label">
            {{ f.value }}
          </a-descriptions-item>
        </a-descriptions>
        <a v-if="safeUrl(widget.url)" :href="safeUrl(widget.url)" target="_blank" rel="noopener noreferrer">Open</a>
      </a-card>

      <pre v-else class="widget-unknown">{{ JSON.stringify(widget, null, 2) }}</pre>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { AgentWidget } from '~/composables/useAgentApi'

defineProps<{ widgets: AgentWidget[] }>()

// Belt-and-braces with the server-side widgets.safeURL: only http(s)/mailto
// links render — a javascript:/data: URL from a steered model must never
// become a clickable link in the admin origin. URL() sees the same scheme
// the browser will act on (and mirrors the server's url.Parse check);
// relative URLs throw without a base and are rejected, as before.
function safeUrl(url?: string): string {
  const raw = url?.trim() ?? ''
  if (!raw) return ''
  try {
    const { protocol } = new URL(raw)
    return protocol === 'http:' || protocol === 'https:' || protocol === 'mailto:' ? raw : ''
  } catch {
    return ''
  }
}

function tableColumns(widget: AgentWidget) {
  return (widget.columns ?? []).map((title, idx) => ({
    title,
    dataIndex: `c${idx}`,
    key: `c${idx}`,
  }))
}

function tableRows(widget: AgentWidget) {
  return (widget.rows ?? []).map((row, idx) => {
    const record: Record<string, string> = { key: String(idx) }
    row.forEach((cell, cellIdx) => {
      record[`c${cellIdx}`] = cell
    })
    return record
  })
}
</script>

<style scoped>
.agent-widgets > * + * {
  margin-top: 8px;
}
.markdown-body {
  white-space: pre-wrap;
  margin: 0;
}
.widget-unknown {
  background: #f5f5f5;
  padding: 8px;
  border-radius: 4px;
  overflow-x: auto;
}
.widget-list ul {
  margin: 0;
  padding-left: 20px;
}
</style>
