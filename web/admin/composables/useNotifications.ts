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

export interface ApiMessage {
  type: 'success' | 'info' | 'warning' | 'error'
  text: string
}

export interface Notification {
  id: number
  type: ApiMessage['type']
  text: string
}

let nextId = 0

export function useNotifications() {
  const notifications = useState<Notification[]>('notifications', () => [])

  function addNotification(msg: ApiMessage) {
    const id = nextId++
    notifications.value.push({ id, type: msg.type, text: msg.text })
    if (msg.type === 'success' || msg.type === 'info') {
      setTimeout(() => removeNotification(id), 3000)
    }
  }

  function removeNotification(id: number) {
    notifications.value = notifications.value.filter((n) => n.id !== id)
  }

  function processApiMessages(messages?: ApiMessage[]) {
    if (!messages || messages.length === 0) return
    for (const msg of messages) {
      addNotification(msg)
    }
  }

  return { notifications, addNotification, removeNotification, processApiMessages }
}
