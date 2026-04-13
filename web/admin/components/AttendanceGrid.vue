<!--
  Copyright (C) 2026 Wepala, LLC
  AGPL-3.0-or-later (see LICENSES)
-->

<template>
  <div>
    <!-- Period Navigation -->
    <div style="display: flex; justify-content: center; align-items: center; gap: 16px; margin-bottom: 12px">
      <a-button :disabled="loading" size="small" @click="shiftPeriod(-1)">&larr;</a-button>
      <strong style="min-width: 160px; text-align: center">{{ periodLabel }}</strong>
      <a-button :disabled="loading" size="small" @click="shiftPeriod(1)">&rarr;</a-button>
    </div>

    <a-spin v-if="loading" />

    <template v-else-if="visibleEvents.length > 0 && enrollments.length > 0">
      <div style="overflow-x: auto; -webkit-overflow-scrolling: touch">
        <table class="attendance-grid">
          <thead>
            <tr>
              <th class="student-col">Student</th>
              <th v-for="evt in visibleEvents" :key="evt.id" class="event-col">
                <a-tooltip :title="fullDate(evt.eventDate || evt.startTime)">
                  {{ shortDate(evt.eventDate || evt.startTime) }}
                </a-tooltip>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="enr in enrollments" :key="enr.id">
              <td class="student-col">
                <NuxtLink :to="`/resources/student/${enr.studentId}`">
                  {{ studentName(enr.studentId) }}
                </NuxtLink>
              </td>
              <td v-for="evt in visibleEvents" :key="evt.id" class="event-col">
                <a-checkbox
                  :checked="isPresent(enr.studentId, evt.id)"
                  @change="toggleAttendance(enr.studentId, evt.id)"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <a-empty v-else-if="visibleEvents.length === 0" description="No classes scheduled in this period" />
    <a-empty v-else description="No students enrolled" />

    <div v-if="toggleError" style="margin-top: 8px; padding: 6px 12px; border-radius: 4px; background: #fff2f0; border: 1px solid #ffccc7; color: #cf1322; font-size: 12px">
      {{ toggleError }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'

const props = defineProps<{
  courseInstanceId: string
}>()

const { list: listEvents } = useResourceApi('education-event')
const { list: listEnrollments } = useResourceApi('enrollment')
const { list: listRecords, create: createRecord, update: updateRecord } = useResourceApi('attendance-record')
const { preloadType, resolve } = useResourceLookup()

const loading = ref(true)
const toggleError = ref<string | null>(null)
const events = ref<any[]>([])
const enrollments = ref<any[]>([])
const records = ref<Record<string, any>>({})
const periodStart = ref<Date>(new Date())
const periodEnd = ref<Date>(new Date())

const periodLabel = computed(() => {
  const start = periodStart.value
  const end = periodEnd.value
  const opts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }
  return `${start.toLocaleDateString(undefined, opts)} – ${end.toLocaleDateString(undefined, opts)}`
})

const visibleEvents = computed(() =>
  events.value.filter(e => {
    const d = parseLocalDate(e.eventDate || e.startTime)
    return d >= periodStart.value && d <= periodEnd.value
  })
)

function studentName(id: string) {
  return resolve('student', id)
}

// Parse date string as local date to avoid timezone-shift issues.
// "2026-04-11T00:00:00Z" in UTC-4 would show as Friday Apr 10 if
// parsed as UTC — extracting the YYYY-MM-DD and constructing a local
// date preserves the intended calendar day.
function parseLocalDate(iso: string): Date {
  const dateOnly = iso.substring(0, 10) // "2026-04-11"
  const [y, m, d] = dateOnly.split('-').map(Number)
  return new Date(y, m - 1, d)
}

function shortDate(iso: string) {
  if (!iso) return ''
  return parseLocalDate(iso).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric' })
}

function fullDate(iso: string) {
  if (!iso) return ''
  return parseLocalDate(iso).toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' })
}

function setCurrentWeek() {
  const now = new Date()
  const day = now.getDay()
  const start = new Date(now)
  start.setDate(now.getDate() - day)
  start.setHours(0, 0, 0, 0)
  const end = new Date(start)
  end.setDate(start.getDate() + 6)
  end.setHours(23, 59, 59, 999)
  periodStart.value = start
  periodEnd.value = end
}

function shiftPeriod(weeks: number) {
  const ms = weeks * 7 * 24 * 60 * 60 * 1000
  periodStart.value = new Date(periodStart.value.getTime() + ms)
  periodEnd.value = new Date(periodEnd.value.getTime() + ms)
}

function isPresent(studentId: string, eventId: string): boolean {
  const rec = records.value[`${studentId}:${eventId}`]
  return rec && (rec.attendanceStatus === 'Present' || rec.attendanceStatus === 'Late')
}

async function toggleAttendance(studentId: string, eventId: string) {
  toggleError.value = null
  const key = `${studentId}:${eventId}`
  const existing = records.value[key]

  if (existing?.id) {
    const newStatus = existing.attendanceStatus === 'Present' ? 'Absent' : 'Present'
    const old = existing.attendanceStatus
    existing.attendanceStatus = newStatus
    try {
      await updateRecord(existing.id, {
        educationEventId: existing.educationEventId,
        studentId: existing.studentId,
        attendanceStatus: newStatus,
      })
    } catch {
      existing.attendanceStatus = old
      message.error('Failed to update attendance')
    }
  } else {
    const rec = { educationEventId: eventId, studentId, attendanceStatus: 'Present' }
    records.value[key] = rec
    try {
      const created = await createRecord(rec)
      records.value[key] = created
    } catch {
      delete records.value[key]
      message.error('Failed to create attendance record')
    }
  }
}

async function load() {
  loading.value = true
  try {
    const [evtRes, enrRes] = await Promise.all([
      listEvents('', 200, '', '', { courseInstanceId: { eq: props.courseInstanceId } }),
      listEnrollments('', 200, '', '', { courseInstanceId: { eq: props.courseInstanceId } }),
    ])
    events.value = (evtRes.data || []).sort((a: any, b: any) =>
      parseLocalDate(a.eventDate || a.startTime).getTime() - parseLocalDate(b.eventDate || b.startTime).getTime()
    )
    enrollments.value = enrRes.data || []

    await preloadType('student', true)

    // Load attendance records in parallel
    const results = await Promise.all(
      events.value.map(evt =>
        listRecords('', 200, '', '', { educationEventId: { eq: evt.id } })
          .then(r => r.data || [])
          .catch(() => [])
      )
    )
    const recs: Record<string, any> = {}
    for (const batch of results) {
      for (const rec of batch) {
        recs[`${rec.studentId}:${rec.educationEventId}`] = rec
      }
    }
    records.value = recs
  } catch (err) {
    console.error('[AttendanceGrid] load failed:', err)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  setCurrentWeek()
  load()
})
</script>

<style scoped>
.attendance-grid {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.attendance-grid th,
.attendance-grid td {
  border: 1px solid #f0f0f0;
  padding: 6px 8px;
}

.attendance-grid thead th {
  background: #fafafa;
  font-weight: 500;
  white-space: nowrap;
}

.student-col {
  min-width: 120px;
  max-width: 180px;
  position: sticky;
  left: 0;
  background: #fff;
  z-index: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.attendance-grid thead .student-col {
  background: #fafafa;
  z-index: 2;
}

.event-col {
  min-width: 48px;
  text-align: center;
  white-space: nowrap;
}
</style>
