<!--
  TaskChecklist — displays tasks as a checkable list with inline creation.
  Used on the project detail page to show tasks linked to a project.
-->
<template>
  <div class="task-checklist">
    <a-spin v-if="loading && tasks.length === 0" />
    <a-list v-else :data-source="tasks" :locale="{ emptyText: 'No tasks yet' }">
      <template #renderItem="{ item }">
        <a-list-item>
          <div style="display: flex; align-items: center; width: 100%">
            <a-checkbox
              :checked="item.status === 'done'"
              @change="() => toggleTask(item)"
            >
              <span class="task-name" :class="{ 'task-done': item.status === 'done' }">
                {{ item.name }}
              </span>
              <a-tag v-if="item.priority" :color="priorityColor(item.priority)" size="small" style="margin-left: 8px">
                {{ item.priority }}
              </a-tag>
            </a-checkbox>
            <NuxtLink :to="`/resources/task/${item.id}/edit`" style="margin-left: auto">
              <a-button type="link" size="small">Edit</a-button>
            </NuxtLink>
          </div>
        </a-list-item>
      </template>
    </a-list>
    <a-input
      v-model:value="newTaskName"
      placeholder="Add a task..."
      :disabled="creating"
      @press-enter="createTask"
      style="margin-top: 8px"
    />
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  projectId: string
  items: any[]
  loading: boolean
}>()

const emit = defineEmits<{
  'task-created': [task: any]
  'task-updated': [task: any]
}>()

const { create, update } = useResourceApi('task')

const tasks = ref<any[]>([])
const newTaskName = ref('')
const creating = ref(false)

watch(() => props.items, (val) => {
  tasks.value = [...val]
}, { immediate: true })

function priorityColor(priority: string): string {
  switch (priority) {
    case 'high': return 'red'
    case 'medium': return 'orange'
    case 'low': return 'blue'
    default: return 'default'
  }
}

async function createTask() {
  const name = newTaskName.value.trim()
  if (!name) return

  creating.value = true
  try {
    const task = await create({
      name,
      status: 'open',
      priority: 'medium',
      project: props.projectId,
    })
    // The API returns the full resource wrapper — extract flat fields
    const flat = { id: task.id, name, status: 'open', priority: 'medium', project: props.projectId }
    tasks.value.push(flat)
    newTaskName.value = ''
    emit('task-created', flat)
  } finally {
    creating.value = false
  }
}

async function toggleTask(item: any) {
  const newStatus = item.status === 'done' ? 'open' : 'done'
  try {
    await update(item.id, {
      name: item.name,
      status: newStatus,
      priority: item.priority || 'medium',
      project: props.projectId,
    })
    item.status = newStatus
    emit('task-updated', item)
  } catch {
    // Revert on failure — status wasn't changed yet since we update after API call
  }
}
</script>

<style scoped>
.task-done {
  text-decoration: line-through;
  color: #999;
}
</style>
