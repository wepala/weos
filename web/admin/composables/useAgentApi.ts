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

// Talks to the in-app agent (/api/agent). Message sends and confirmation
// answers stream server-sent events (see docs/_reference/agent-widgets.md);
// EventSource cannot POST, so the stream is read off a fetch body.

export interface AgentWidgetField {
  label: string
  value: string
}

export interface AgentWidget {
  type: 'markdown' | 'table' | 'list' | 'card' | string
  markdown?: string
  title?: string
  columns?: string[]
  rows?: string[][]
  items?: string[]
  body?: string
  url?: string
  fields?: AgentWidgetField[]
}

export interface AgentWidgetsResponse {
  schemaVersion: number
  widgets: AgentWidget[]
}

export interface AgentEvent {
  type: 'text' | 'widgets' | 'input_requested' | 'done' | 'error' | string
  text?: string
  widgets?: AgentWidgetsResponse
  callId?: string
  tool?: string
  args?: Record<string, unknown>
  hint?: string
  error?: string
}

export interface AgentMessage {
  role: 'user' | 'agent' | string
  text?: string
  widgets?: AgentWidgetsResponse
}

async function streamAgentEvents(
  url: string,
  payload: unknown,
  onEvent: (e: AgentEvent) => void,
): Promise<void> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok || !res.body) {
    let message = `agent request failed (${res.status})`
    try {
      const body = await res.json()
      if (body?.error) message = body.error
    } catch {
      // keep the status message
    }
    throw new Error(message)
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    // SSE frames are separated by a blank line; the data line carries the
    // full event JSON.
    let sep = buffer.indexOf('\n\n')
    while (sep >= 0) {
      const frame = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)
      const dataLine = frame.split('\n').find((l) => l.startsWith('data: '))
      if (dataLine) {
        try {
          onEvent(JSON.parse(dataLine.slice(6)) as AgentEvent)
        } catch {
          // Malformed frame — skip; the server closes streams with `done`.
        }
      }
      sep = buffer.indexOf('\n\n')
    }
  }
}

export function useAgentApi() {
  function sendMessage(
    conversationId: string,
    message: string,
    onEvent: (e: AgentEvent) => void,
  ) {
    return streamAgentEvents(
      `/api/agent/conversations/${encodeURIComponent(conversationId)}/messages`,
      { message },
      onEvent,
    )
  }

  function confirm(
    conversationId: string,
    callId: string,
    confirmed: boolean,
    onEvent: (e: AgentEvent) => void,
  ) {
    return streamAgentEvents(
      `/api/agent/conversations/${encodeURIComponent(conversationId)}/confirmations/${encodeURIComponent(callId)}`,
      { confirmed },
      onEvent,
    )
  }

  async function history(conversationId: string): Promise<AgentMessage[]> {
    const { request } = useApi()
    const messages = await request<AgentMessage[] | null>(
      `/api/agent/conversations/${encodeURIComponent(conversationId)}`,
    )
    return messages ?? []
  }

  return { sendMessage, confirm, history }
}
