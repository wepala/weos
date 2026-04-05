// Checklist screen — a task-oriented view showing tasks with status checkboxes.
// This is a self-describing ES module: exports meta for discovery and a Vue
// component as the default export.

export const meta = {
  name: 'Checklist',
  label: 'Checklist',
  icon: 'check-square-outlined',
}

export default {
  props: ['typeSlug'],
  emits: ['navigate'],
  data() {
    return {
      items: [],
      loading: true,
      hasMore: false,
      cursor: '',
    }
  },
  computed: {
    todoItems() {
      return this.items.filter(i => i.status !== 'done')
    },
    doneItems() {
      return this.items.filter(i => i.status === 'done')
    },
  },
  async mounted() {
    await this.load()
  },
  methods: {
    async load() {
      this.loading = true
      try {
        const res = await fetch(
          `/api/${this.typeSlug}?cursor=${this.cursor}&limit=50`
        )
        const json = await res.json()
        this.items = [...this.items, ...(json.data || [])]
        this.hasMore = json.has_more || false
        this.cursor = json.cursor || ''
      } finally {
        this.loading = false
      }
    },
    async toggleStatus(item) {
      const newStatus = item.status === 'done' ? 'todo' : 'done'
      await fetch(`/api/${this.typeSlug}/${item.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...item, status: newStatus }),
      })
      item.status = newStatus
    },
    priorityColor(priority) {
      const colors = { high: '#ff4d4f', medium: '#faad14', low: '#52c41a' }
      return colors[priority] || '#d9d9d9'
    },
  },
  template: `
    <div style="max-width: 720px;">
      <div v-if="loading && !items.length" style="text-align: center; padding: 48px">
        Loading...
      </div>
      <template v-else>
        <div v-if="todoItems.length" style="margin-bottom: 24px">
          <h3 style="margin-bottom: 12px; color: #595959">To Do</h3>
          <div v-for="item in todoItems" :key="item.id"
               style="display: flex; align-items: center; padding: 8px 12px; border: 1px solid #f0f0f0; border-radius: 6px; margin-bottom: 8px; cursor: pointer"
               @click="toggleStatus(item)">
            <span style="width: 20px; height: 20px; border: 2px solid #d9d9d9; border-radius: 4px; margin-right: 12px; flex-shrink: 0"></span>
            <span style="flex: 1">{{ item.name || item.id }}</span>
            <span v-if="item.priority"
                  :style="{ fontSize: '12px', padding: '2px 8px', borderRadius: '4px', backgroundColor: priorityColor(item.priority), color: '#fff' }">
              {{ item.priority }}
            </span>
          </div>
        </div>
        <div v-if="doneItems.length">
          <h3 style="margin-bottom: 12px; color: #8c8c8c">Done</h3>
          <div v-for="item in doneItems" :key="item.id"
               style="display: flex; align-items: center; padding: 8px 12px; border: 1px solid #f0f0f0; border-radius: 6px; margin-bottom: 8px; opacity: 0.6; cursor: pointer"
               @click="toggleStatus(item)">
            <span style="width: 20px; height: 20px; border: 2px solid #52c41a; border-radius: 4px; margin-right: 12px; flex-shrink: 0; background: #52c41a; display: flex; align-items: center; justify-content: center; color: #fff; font-size: 12px">&#10003;</span>
            <span style="flex: 1; text-decoration: line-through">{{ item.name || item.id }}</span>
          </div>
        </div>
        <div v-if="!items.length" style="text-align: center; padding: 48px; color: #8c8c8c">
          No tasks yet
        </div>
        <div v-if="hasMore" style="text-align: center; margin-top: 16px">
          <button @click="load" style="padding: 6px 16px; border: 1px solid #d9d9d9; border-radius: 4px; background: #fff; cursor: pointer">
            Load More
          </button>
        </div>
      </template>
    </div>
  `,
}
