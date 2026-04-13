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
  <div>
    <h2>Dashboard</h2>

    <!-- Billing Summary -->
    <a-row v-if="canAccessInvoices" :gutter="[16, 16]" style="margin-bottom: 24px">
      <a-col :xs="24" :sm="8">
        <a-card :bordered="false" size="small">
          <a-statistic
            title="Outstanding"
            :value="totalOutstanding"
            :precision="2"
            :prefix="currency"
            :value-style="{ color: totalOutstanding > 0 ? '#cf1322' : '#3f8600' }"
          />
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="8">
        <a-card :bordered="false" size="small">
          <a-statistic
            title="Invoiced This Month"
            :value="totalInvoicedThisMonth"
            :precision="2"
            :prefix="currency"
          />
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="8">
        <a-card :bordered="false" size="small">
          <a-statistic
            title="Payments This Month"
            :value="totalPaymentsThisMonth"
            :precision="2"
            :prefix="currency"
            :value-style="{ color: '#3f8600' }"
          />
        </a-card>
      </a-col>
    </a-row>

    <!-- Classes With Sessions This Week -->
    <a-card title="Classes This Week" :bordered="false" style="margin-bottom: 24px">
      <a-spin v-if="classesLoading" />
      <a-table
        v-else-if="classesThisWeek.length > 0"
        :columns="classColumns"
        :data-source="classesThisWeek"
        :pagination="false"
        :scroll="{ x: 'max-content' }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <NuxtLink :to="`/resources/course-instance/${record.id}`">
              {{ record.name }}
            </NuxtLink>
          </template>
          <template v-else-if="column.key === 'nextSession'">
            {{ formatDate(record._nextSessionDate) }}
          </template>
          <template v-else-if="column.key === 'time'">
            {{ formatTime(record.classStartTime) }} – {{ formatTime(record.classEndTime) }}
          </template>
          <template v-else-if="column.key === 'location'">
            {{ locationNames[record.locationId] || '-' }}
          </template>
          <template v-else-if="column.key === 'sessions'">
            {{ record._sessionCount }}
          </template>
        </template>
      </a-table>
      <a-empty v-else description="No classes scheduled this week" />
    </a-card>

    <!-- Outstanding Invoices -->
    <a-card v-if="canAccessInvoices" title="Outstanding Invoices" :bordered="false">
      <a-spin v-if="invoicesLoading" />
      <a-table
        v-else-if="outstandingInvoices.length > 0"
        :columns="invoiceColumns"
        :data-source="outstandingInvoices"
        :pagination="false"
        :scroll="{ x: 'max-content' }"
        row-key="id"
        size="small"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'invoiceNumber'">
            <NuxtLink :to="`/resources/invoice/${record.id}`">{{ record.invoiceNumber }}</NuxtLink>
          </template>
          <template v-else-if="column.key === 'student'">
            {{ record._studentName || '-' }}
          </template>
          <template v-else-if="column.key === 'guardian'">
            {{ record._guardianName || '-' }}
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
          </template>
        </template>
      </a-table>
      <a-empty v-else description="No outstanding invoices" />
    </a-card>
  </div>
</template>

<script setup lang="ts">
const { fetchResourceTypes, loaded } = useResourceTypeStore()
const { list: listEvents } = useResourceApi('education-event')
const { list: listCourseInstances } = useResourceApi('course-instance')
const { list: listLocations } = useResourceApi('location')
const { list: listInvoices } = useResourceApi('invoice')
const { list: listPayments } = useResourceApi('payment')
const { preloadType, resolve } = useResourceLookup()

const classesLoading = ref(true)
const classesThisWeek = ref<any[]>([])
const locationNames = ref<Record<string, string>>({})

const canAccessInvoices = ref(true)

const invoicesLoading = ref(true)
const allInvoices = ref<any[]>([])
const allPayments = ref<any[]>([])

const outstandingInvoices = computed(() =>
  allInvoices.value.filter((inv) => inv.status !== 'paid' && inv.status !== 'cancelled'),
)

const currency = computed(() => {
  const first = allInvoices.value[0]
  return first?.currency || ''
})

const totalOutstanding = computed(() =>
  outstandingInvoices.value.reduce((sum, inv) => sum + (inv.totalAmount || 0), 0),
)

const totalInvoicedThisMonth = computed(() => {
  const now = new Date()
  const y = now.getFullYear()
  const m = now.getMonth()
  return allInvoices.value
    .filter((inv) => {
      if (!inv.invoiceDate) return false
      const d = new Date(inv.invoiceDate)
      return d.getFullYear() === y && d.getMonth() === m
    })
    .reduce((sum, inv) => sum + (inv.totalAmount || 0), 0)
})

const totalPaymentsThisMonth = computed(() => {
  const now = new Date()
  const y = now.getFullYear()
  const m = now.getMonth()
  return allPayments.value
    .filter((p) => {
      if (p.status !== 'completed') return false
      const dateStr = p.paymentDate || p.created_at
      if (!dateStr) return false
      const d = new Date(dateStr)
      return d.getFullYear() === y && d.getMonth() === m
    })
    .reduce((sum, p) => sum + (p.amount || 0), 0)
})

const invoiceColumns = [
  { title: 'Invoice #', key: 'invoiceNumber', dataIndex: 'invoiceNumber' },
  { title: 'Student', key: 'student' },
  { title: 'Guardian', key: 'guardian' },
  { title: 'Amount', key: 'totalAmount', dataIndex: 'totalAmount' },
  { title: 'Status', key: 'status', dataIndex: 'status' },
]

function statusColor(status: string) {
  switch (status) {
    case 'paid': return 'green'
    case 'sent': return 'blue'
    case 'draft': return 'default'
    case 'overdue': return 'red'
    default: return 'default'
  }
}

const classColumns = [
  { title: 'Class', key: 'name', dataIndex: 'name' },
  { title: 'Next Session', key: 'nextSession' },
  { title: 'Time', key: 'time' },
  { title: 'Location', key: 'location' },
  { title: 'Sessions', key: 'sessions' },
]

function formatDate(val: string) {
  if (!val) return '-'
  const dateOnly = val.substring(0, 10)
  const [y, m, d] = dateOnly.split('-').map(Number)
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
}

function formatTime(val: string) {
  if (!val) return '-'
  // Plain time string (e.g. "09:00" or "14:30")
  if (val.length <= 5 || val.match(/^\d{2}:\d{2}$/)) {
    const [h, m] = val.split(':')
    const date = new Date()
    date.setHours(Number(h), Number(m), 0, 0)
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  const d = new Date(val)
  if (!isNaN(d.getTime())) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return val
}

function parseLineItems(inv: any): any[] {
  let items = inv.lineItems
  if (typeof items === 'string') {
    try { items = JSON.parse(items) } catch { return [] }
  }
  return Array.isArray(items) ? items : []
}

async function fetchBilling() {
  invoicesLoading.value = true
  try {
    const invoices: any[] = []
    let invCursor = ''
    let invMore = true
    while (invMore) {
      const invRes = await listInvoices(invCursor, 100)
      for (const inv of invRes.data) {
        inv._guardianName = inv.guardianId ? resolve('guardian', inv.guardianId) : ''
        const items = parseLineItems(inv)
        inv._studentName = items.length > 0 && items[0].studentId
          ? resolve('student', items[0].studentId) : ''
        invoices.push(inv)
      }
      invCursor = invRes.cursor
      invMore = invRes.has_more
    }
    allInvoices.value = invoices

    const payments: any[] = []
    let payCursor = ''
    let payMore = true
    while (payMore) {
      const payRes = await listPayments(payCursor, 100)
      payments.push(...payRes.data)
      payCursor = payRes.cursor
      payMore = payRes.has_more
    }
    allPayments.value = payments
  } catch {
    // Invoice/payment types might not be installed
    canAccessInvoices.value = false
  } finally {
    invoicesLoading.value = false
  }
}

async function fetchClassesThisWeek() {
  classesLoading.value = true
  try {
    const now = new Date()
    const day = now.getDay()
    const weekStart = new Date(now)
    weekStart.setDate(now.getDate() - day)
    weekStart.setHours(0, 0, 0, 0)
    const weekEnd = new Date(weekStart)
    weekEnd.setDate(weekStart.getDate() + 7)

    const evRes = await listEvents('', 1000, '', '', {
      eventDate: { gte: weekStart.toISOString(), lt: weekEnd.toISOString() },
    })
    const thisWeekEvents = evRes.data

    const courseMap = new Map<string, any[]>()
    for (const ev of thisWeekEvents) {
      if (!ev.courseInstanceId) continue
      if (!courseMap.has(ev.courseInstanceId)) courseMap.set(ev.courseInstanceId, [])
      courseMap.get(ev.courseInstanceId)!.push(ev)
    }

    if (courseMap.size === 0) {
      classesThisWeek.value = []
      return
    }

    const ciById = new Map<string, any>()
    let ciCursor = ''
    let ciMore = true
    while (ciMore && ciById.size < courseMap.size) {
      const ciRes = await listCourseInstances(ciCursor, 100)
      for (const ci of ciRes.data) ciById.set(ci.id, ci)
      ciCursor = ciRes.cursor
      ciMore = ciRes.has_more
    }

    const classes: any[] = []
    for (const [ciId, events] of courseMap) {
      const ci = ciById.get(ciId)
      if (!ci) continue
      const sorted = events.sort((a: any, b: any) => new Date(a.eventDate).getTime() - new Date(b.eventDate).getTime())
      const nextSession = sorted.find((e: any) => new Date(e.eventDate) >= now) || sorted[0]
      classes.push({
        ...ci,
        _nextSessionDate: nextSession.eventDate,
        _sessionCount: events.length,
      })
    }

    classes.sort((a, b) => new Date(a._nextSessionDate).getTime() - new Date(b._nextSessionDate).getTime())
    classesThisWeek.value = classes
  } catch {
    // education-event type might not be installed
  } finally {
    classesLoading.value = false
  }
}

onMounted(async () => {
  if (!loaded.value) await fetchResourceTypes()
  await Promise.all([
    preloadType('course-instance'),
    preloadType('student'),
    preloadType('guardian'),
  ])

  fetchBilling()
  fetchClassesThisWeek()

  try {
    const res = await listLocations('', 100)
    const names: Record<string, string> = {}
    for (const l of res.data) names[l.id] = l.name || l.id
    locationNames.value = names
  } catch {
    // Location lookup is best-effort
  }
})
</script>
