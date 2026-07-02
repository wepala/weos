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
  <div class="agent-page">
    <div class="page-header">
      <h2>Agent</h2>
      <a-button @click="newConversation">New conversation</a-button>
    </div>

    <a-alert
      v-if="unavailable"
      type="info"
      show-icon
      message="The in-app agent is not configured"
      description="Set the Gemini API key on this instance to enable it."
    />

    <template v-else>
      <div ref="scroller" class="chat-scroll">
        <div v-for="(m, i) in messages" :key="i" :class="['chat-message', m.role]">
          <template v-if="m.role === 'user'">
            <p class="user-text">{{ m.text }}</p>
          </template>
          <template v-else>
            <AgentWidgets v-if="m.widgets" :widgets="m.widgets.widgets" />
            <p v-else class="streaming-text">{{ m.text }}<span class="cursor">▋</span></p>
          </template>
        </div>

        <a-card v-if="pending" size="small" class="confirmation-card" :title="'The agent wants to run: ' + pending.tool">
          <p v-if="pending.hint">{{ pending.hint }}</p>
          <pre v-if="pending.args" class="pending-args">{{ JSON.stringify(pending.args, null, 2) }}</pre>
          <a-space>
            <a-button type="primary" :loading="busy" @click="answer(true)">Approve</a-button>
            <a-button danger :loading="busy" @click="answer(false)">Reject</a-button>
          </a-space>
        </a-card>
      </div>

      <div class="composer">
        <a-textarea
          v-model:value="draft"
          :auto-size="{ minRows: 1, maxRows: 5 }"
          placeholder="Ask your app anything…"
          :disabled="busy || !!pending"
          @keydown.enter.exact.prevent="send"
        />
        <a-button type="primary" :loading="busy" :disabled="!draft.trim() || !!pending" @click="send">
          Send
        </a-button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { AgentEvent, AgentWidgetsResponse } from '~/composables/useAgentApi'

interface ChatMessage {
  role: 'user' | 'agent'
  text?: string
  widgets?: AgentWidgetsResponse
}

interface PendingConfirmation {
  callId: string
  tool: string
  hint?: string
  args?: Record<string, unknown>
}

const { sendMessage, confirm, history } = useAgentApi()

const conversationId = ref('')
const messages = ref<ChatMessage[]>([])
const draft = ref('')
const busy = ref(false)
const unavailable = ref(false)
const pending = ref<PendingConfirmation | null>(null)
const scroller = ref<HTMLElement | null>(null)

const storageKey = 'weos-agent-conversation'

onMounted(async () => {
  conversationId.value = localStorage.getItem(storageKey) || newId()
  localStorage.setItem(storageKey, conversationId.value)
  await loadHistory()
})

function newId(): string {
  return `web-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

async function loadHistory() {
  try {
    const past = await history(conversationId.value)
    messages.value = past.map((m) => ({
      role: m.role === 'user' ? 'user' : 'agent',
      text: m.text,
      widgets: m.widgets,
    }))
    scrollDown()
  } catch (err: any) {
    if (err?.status === 503 || err?.response?.status === 503) {
      unavailable.value = true
    }
  }
}

function newConversation() {
  conversationId.value = newId()
  localStorage.setItem(storageKey, conversationId.value)
  messages.value = []
  pending.value = null
}

function handleEvent(agentReply: ChatMessage) {
  return (e: AgentEvent) => {
    if (e.type === 'text' && e.text) {
      agentReply.text = (agentReply.text ?? '') + e.text
      scrollDown()
    } else if (e.type === 'widgets' && e.widgets) {
      agentReply.widgets = e.widgets
      agentReply.text = undefined
      scrollDown()
    } else if (e.type === 'input_requested' && e.callId) {
      pending.value = { callId: e.callId, tool: e.tool ?? 'a tool', hint: e.hint, args: e.args }
    } else if (e.type === 'error' && e.error) {
      agentReply.widgets = {
        schemaVersion: 1,
        widgets: [{ type: 'markdown', markdown: `⚠️ ${e.error}` }],
      }
      if (e.error.includes('not configured')) unavailable.value = true
    }
  }
}

async function send() {
  const message = draft.value.trim()
  if (!message || busy.value) return
  draft.value = ''
  busy.value = true
  messages.value.push({ role: 'user', text: message })
  const agentReply = reactive<ChatMessage>({ role: 'agent', text: '' })
  messages.value.push(agentReply)
  scrollDown()
  try {
    await sendMessage(conversationId.value, message, handleEvent(agentReply))
  } catch (err: any) {
    agentReply.widgets = {
      schemaVersion: 1,
      widgets: [{ type: 'markdown', markdown: `⚠️ ${err?.message ?? 'request failed'}` }],
    }
    if (err?.message?.includes('not configured')) unavailable.value = true
  } finally {
    busy.value = false
  }
}

async function answer(confirmed: boolean) {
  if (!pending.value || busy.value) return
  const callId = pending.value.callId
  pending.value = null
  busy.value = true
  const agentReply = reactive<ChatMessage>({ role: 'agent', text: '' })
  messages.value.push(agentReply)
  try {
    await confirm(conversationId.value, callId, confirmed, handleEvent(agentReply))
  } catch (err: any) {
    agentReply.widgets = {
      schemaVersion: 1,
      widgets: [{ type: 'markdown', markdown: `⚠️ ${err?.message ?? 'request failed'}` }],
    }
  } finally {
    busy.value = false
  }
}

function scrollDown() {
  nextTick(() => {
    scroller.value?.scrollTo({ top: scroller.value.scrollHeight, behavior: 'smooth' })
  })
}
</script>

<style scoped>
.agent-page {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 160px);
}
.chat-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 8px 4px;
}
.chat-message {
  margin-bottom: 12px;
  max-width: 85%;
}
.chat-message.user {
  margin-left: auto;
  background: #e6f4ff;
  border-radius: 8px;
  padding: 8px 12px;
}
.chat-message.agent {
  margin-right: auto;
  background: #fafafa;
  border-radius: 8px;
  padding: 8px 12px;
}
.user-text,
.streaming-text {
  margin: 0;
  white-space: pre-wrap;
}
.cursor {
  animation: blink 1s step-end infinite;
}
@keyframes blink {
  50% {
    opacity: 0;
  }
}
.confirmation-card {
  border-color: #faad14;
  margin: 8px 0;
}
.pending-args {
  background: #f5f5f5;
  padding: 8px;
  border-radius: 4px;
  overflow-x: auto;
  max-height: 160px;
}
.composer {
  display: flex;
  gap: 8px;
  padding-top: 12px;
  align-items: flex-end;
}
.composer .ant-btn {
  height: 32px;
}
</style>
