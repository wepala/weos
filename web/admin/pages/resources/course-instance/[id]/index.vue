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
    <a-page-header
      :title="resource?.name || 'Course Instance Details'"
      @back="router.back()"
    >
      <template #extra>
        <NuxtLink :to="`/resources/course-instance/${id}/edit`">
          <a-button type="primary">Edit</a-button>
        </NuxtLink>
      </template>
    </a-page-header>
    <a-spin v-if="loading" />
    <template v-else-if="resource">
      <a-descriptions bordered :column="1">
        <a-descriptions-item label="ID">{{ resource.id }}</a-descriptions-item>
        <a-descriptions-item
          v-for="field in fields"
          :key="field.key"
          :label="field.label"
        >
          <template v-if="field.inputType === 'checkbox'">
            {{ resource[field.key] ? 'Yes' : 'No' }}
          </template>
          <template v-else-if="field.inputType === 'resource-select' && field.resourceType && resource[field.key]">
            {{ resolve(field.resourceType, resource[field.key]) }}
          </template>
          <template v-else>
            {{ resource[field.key] || '-' }}
          </template>
        </a-descriptions-item>
      </a-descriptions>

      <!-- Attendance Grid -->
      <a-divider />
      <h3 style="margin: 0 0 16px 0">Attendance</h3>
      <AttendanceGrid :key="refreshKey" :course-instance-id="id" />

      <!-- Enrolled Students -->
      <a-divider />
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px">
        <h3 style="margin: 0">Enrolled Students</h3>
        <a-space>
          <a-button type="primary" @click="openEnrollModal">Enroll Students</a-button>
        </a-space>
      </div>
      <a-table
        :columns="enrollmentColumns"
        :data-source="enrolledStudents"
        :loading="enrollmentsLoading"
        :pagination="false"
        :scroll="{ x: 'max-content' }"
        row-key="id"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'student'">
            <NuxtLink :to="`/resources/student/${record.studentId}`">
              {{ resolve('student', record.studentId) }}
            </NuxtLink>
          </template>
          <template v-else-if="column.key === 'guardian'">
            {{ record.guardianId ? resolve('guardian', record.guardianId) : '-' }}
          </template>
          <template v-else-if="column.key === 'invoiceStatus'">
            <template v-if="invoiceMap.get(record.id)">
              <NuxtLink :to="`/resources/invoice/${invoiceMap.get(record.id).id}`">
                <a-tag :color="invoiceStatusColor(invoiceMap.get(record.id).status || 'draft')">
                  {{ invoiceMap.get(record.id).status || 'draft' }}
                </a-tag>
              </NuxtLink>
            </template>
            <a-tag v-else color="grey">No Invoice</a-tag>
          </template>
          <template v-else-if="column.key === 'actions'">
            <a-popconfirm
              title="Delete this enrollment?"
              ok-text="Delete"
              ok-type="danger"
              @confirm="handleDeleteEnrollment(record)"
            >
              <a-button danger size="small">Delete</a-button>
            </a-popconfirm>
          </template>
        </template>
      </a-table>

      <!-- Child sections (education events, etc.) -->
      <template v-for="child in childSections" :key="child.slug">
        <a-divider />
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px">
          <h3 style="margin: 0">{{ child.name }}</h3>
          <NuxtLink :to="`/resources/${child.slug}`">
            <a-button size="small">View All</a-button>
          </NuxtLink>
        </div>
        <ResourceTable
          :items="child.items"
          :columns="child.columns"
          :loading="child.loading"
          :has-more="child.hasMore"
          :type-slug="child.slug"
          @load-more="child.loadMore()"
        />
      </template>

      <!-- Enrollment Tasks -->
      <template v-if="enrollmentTasks.length > 0">
        <a-divider />
        <h3 style="margin-bottom: 16px">Enrollment Tasks</h3>
        <a-table
          :columns="taskColumns"
          :data-source="enrollmentTasks"
          :loading="tasksLoading"
          :pagination="false"
          :scroll="{ x: 'max-content' }"
          row-key="id"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'student'">
              <NuxtLink :to="`/resources/student/${record.studentId}`">
                {{ resolve('student', record.studentId) }}
              </NuxtLink>
            </template>
            <template v-else-if="column.key === 'status'">
              <a-checkbox
                :checked="record.actionStatus === 'CompletedActionStatus'"
                @change="(e) => toggleTask(record, e.target.checked)"
              >
                {{ record.actionStatus === 'CompletedActionStatus' ? 'Done' : 'Pending' }}
              </a-checkbox>
            </template>
          </template>
        </a-table>
      </template>
    </template>

    <!-- Bulk Enrollment Modal -->
    <a-modal
      v-model:open="showEnrollModal"
      title="Enroll Students"
      :width="700"
      @ok="handleBulkEnroll"
      :confirm-loading="enrolling"
      :ok-button-props="{ disabled: !canEnroll }"
    >
      <a-form layout="vertical">
        <!-- Students -->
        <a-form-item label="Students" required>
          <a-select
            v-model:value="selectedStudentIds"
            mode="multiple"
            show-search
            placeholder="Search and select students"
            :options="availableStudentOptions"
            :filter-option="filterOption"
            style="width: 100%"
          />
          <a-button type="link" size="small" @click="showNewStudent = !showNewStudent" style="padding: 0; margin-top: 4px">
            + Add new student
          </a-button>
        </a-form-item>

        <template v-if="showNewStudent">
          <a-card size="small" style="margin-bottom: 16px">
            <template #title><span style="font-size: 13px">New Student</span></template>
            <a-row :gutter="16">
              <a-col :span="12">
                <a-form-item label="First Name" required style="margin-bottom: 8px">
                  <a-input v-model:value="newStudentFirstName" />
                </a-form-item>
              </a-col>
              <a-col :span="12">
                <a-form-item label="Last Name" required style="margin-bottom: 8px">
                  <a-input v-model:value="newStudentLastName" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-button size="small" type="primary" :disabled="!newStudentFirstName.trim() || !newStudentLastName.trim()" @click="addNewStudent">
              Add Student
            </a-button>
          </a-card>
        </template>

        <!-- Selected students tags -->
        <div v-if="allSelectedStudents.length > 0" style="margin-bottom: 16px">
          <a-tag
            v-for="s in allSelectedStudents"
            :key="s.id"
            closable
            @close="removeStudent(s.id)"
          >
            {{ s.name }}
          </a-tag>
        </div>

        <a-divider style="margin: 12px 0" />

        <!-- Billing -->
        <a-form-item label="Billing Type" required>
          <a-radio-group v-model:value="billingType">
            <a-radio-button value="guardian">Guardian</a-radio-button>
            <a-radio-button value="institutional">Organization</a-radio-button>
          </a-radio-group>
        </a-form-item>

        <template v-if="billingType === 'guardian'">
          <a-form-item label="Guardian" required>
            <a-select
              v-model:value="selectedGuardianId"
              show-search
              allow-clear
              placeholder="Select existing guardian"
              :options="guardianOptions"
              :filter-option="filterOption"
              style="width: 100%"
            />
            <a-button type="link" size="small" @click="showNewGuardian = !showNewGuardian" style="padding: 0; margin-top: 4px">
              + Create new guardian
            </a-button>
          </a-form-item>

          <a-card v-if="showNewGuardian" size="small" style="margin-bottom: 16px">
            <template #title><span style="font-size: 13px">New Guardian</span></template>
            <a-row :gutter="16">
              <a-col :span="12">
                <a-form-item label="First Name" required style="margin-bottom: 8px">
                  <a-input v-model:value="newGuardianFirstName" />
                </a-form-item>
              </a-col>
              <a-col :span="12">
                <a-form-item label="Last Name" required style="margin-bottom: 8px">
                  <a-input v-model:value="newGuardianLastName" />
                </a-form-item>
              </a-col>
            </a-row>
            <a-row :gutter="16">
              <a-col :span="8">
                <a-form-item label="Relationship" style="margin-bottom: 8px">
                  <a-input v-model:value="newGuardianRelationship" placeholder="e.g. Parent" />
                </a-form-item>
              </a-col>
              <a-col :span="8">
                <a-form-item label="Phone" style="margin-bottom: 8px">
                  <a-input v-model:value="newGuardianPhone" />
                </a-form-item>
              </a-col>
              <a-col :span="8">
                <a-form-item label="Email" style="margin-bottom: 8px">
                  <a-input v-model:value="newGuardianEmail" />
                </a-form-item>
              </a-col>
            </a-row>
          </a-card>
        </template>

        <template v-if="billingType === 'institutional'">
          <a-form-item label="Organization" required>
            <a-select
              v-model:value="selectedOrgId"
              show-search
              placeholder="Select organization"
              :options="orgOptions"
              :filter-option="filterOption"
              style="width: 100%"
            />
          </a-form-item>
        </template>

        <a-divider style="margin: 12px 0" />

        <!-- Price override -->
        <a-row :gutter="16">
          <a-col :span="8">
            <a-form-item label="Payment Cadence">
              <a-tag>{{ resource?.paymentCadence || '-' }}</a-tag>
            </a-form-item>
          </a-col>
          <a-col :span="8">
            <a-form-item label="Agreed Price">
              <a-input-number
                v-model:value="agreedPrice"
                :min="0"
                :placeholder="`${resource?.price ?? 0} (MSRP)`"
                style="width: 100%"
              />
              <span v-if="agreedPrice === null || agreedPrice === undefined" style="font-size: 12px; color: #888">
                Uses MSRP: {{ resource?.priceCurrency }} {{ resource?.price }}
              </span>
            </a-form-item>
          </a-col>
          <a-col :span="8">
            <a-form-item label="Currency">
              <a-input v-model:value="priceCurrency" :placeholder="resource?.priceCurrency || ''" />
            </a-form-item>
          </a-col>
        </a-row>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { message } from 'ant-design-vue'

const route = useRoute()
const router = useRouter()
const id = computed(() => route.params.id as string)
const typeSlug = 'course-instance'

const { getBySlug, fetchResourceTypes, loaded, resourceTypes } = useResourceTypeStore()
const { get } = useResourceApi(typeSlug)
const { list: listEnrollments, create: createEnrollment, remove: removeEnrollment } = useResourceApi('enrollment')
const { list: listInvoices } = useResourceApi('invoice')
const { list: listTasks, update: updateTask } = useResourceApi('enrollment-task')
const { list: listStudents, create: createStudent } = useResourceApi('student')
const { list: listGuardians, create: createGuardian } = useResourceApi('guardian')
const { createPerson } = usePersonApi()
const { listOrganizations } = useOrganizationApi()
const { schemaToFields, schemaToColumns } = useSchemaUtils()
const { getChildren } = useSidebarSettings()
const { preloadType, resolve } = useResourceLookup()

const resource = ref<any>(null)
const loading = ref(true)

const resourceType = computed(() => getBySlug(typeSlug))

const fields = computed(() => {
  const schema = resourceType.value?.schema
  if (schema) return schemaToFields(schema)
  return []
})

const refreshKey = ref(0)

// Enrolled students
const enrolledStudents = ref<any[]>([])
const enrollmentsLoading = ref(false)
const invoiceMap = ref(new Map<string, any>())

const enrollmentColumns = [
  { title: 'Student', key: 'student', dataIndex: 'studentId' },
  { title: 'Payment Cadence', key: 'paymentCadence', dataIndex: 'paymentCadence' },
  { title: 'Agreed Price', key: 'agreedPrice', dataIndex: 'agreedPrice' },
  { title: 'Currency', key: 'priceCurrency', dataIndex: 'priceCurrency' },
  { title: 'Billing Type', key: 'billingType', dataIndex: 'billingType' },
  { title: 'Guardian', key: 'guardian', dataIndex: 'guardianId' },
  { title: 'Invoice', key: 'invoiceStatus' },
  { title: 'Actions', key: 'actions', width: 80 },
]

async function fetchEnrollments() {
  enrollmentsLoading.value = true
  try {
    const all: any[] = []
    let cursor = ''
    let hasMore = true
    while (hasMore) {
      const res = await listEnrollments(cursor, 100, '', '', { courseInstanceId: { eq: id.value } })
      all.push(...res.data)
      cursor = res.cursor
      hasMore = res.has_more
    }
    enrolledStudents.value = all
    fetchEnrollmentInvoices()
  } finally {
    enrollmentsLoading.value = false
  }
}

async function fetchEnrollmentInvoices() {
  try {
    const enrollmentIds = new Set(enrolledStudents.value.map((e: any) => e.id))
    const res = await listInvoices('', 1000)
    const map = new Map<string, any>()
    for (const inv of res.data) {
      // Check direct enrollmentId
      if (inv.enrollmentId && enrollmentIds.has(inv.enrollmentId)) {
        map.set(inv.enrollmentId, inv)
      }
      // Check line items for grouped invoices
      let lineItems = inv.lineItems
      if (typeof lineItems === 'string') {
        try { lineItems = JSON.parse(lineItems) } catch { lineItems = null }
      }
      if (Array.isArray(lineItems)) {
        for (const item of lineItems) {
          if (item.enrollmentId && enrollmentIds.has(item.enrollmentId)) {
            map.set(item.enrollmentId, inv)
          }
        }
      }
    }
    invoiceMap.value = map
  } catch {
    // invoice access may be restricted
  }
}

function invoiceStatusColor(status: string) {
  switch (status) {
    case 'paid': return 'green'
    case 'sent': return 'blue'
    case 'draft': return 'default'
    case 'overdue': return 'red'
    case 'cancelled': return 'grey'
    default: return 'default'
  }
}

async function handleDeleteEnrollment(enrollment: any) {
  try {
    await removeEnrollment(enrollment.id)
    enrolledStudents.value = enrolledStudents.value.filter((e: any) => e.id !== enrollment.id)
    message.success('Enrollment deleted')
  } catch (err: any) {
    message.error(err?.data?.error || 'Failed to delete enrollment')
  }
}

// --- Bulk Enrollment Modal ---
const showEnrollModal = ref(false)
const enrolling = ref(false)

const allStudents = ref<any[]>([])
const selectedStudentIds = ref<string[]>([])
const createdStudents = ref<{ id: string; name: string }[]>([])
const showNewStudent = ref(false)
const newStudentFirstName = ref('')
const newStudentLastName = ref('')

const allGuardians = ref<any[]>([])
const selectedGuardianId = ref<string>()
const showNewGuardian = ref(false)
const newGuardianFirstName = ref('')
const newGuardianLastName = ref('')
const newGuardianRelationship = ref('')
const newGuardianPhone = ref('')
const newGuardianEmail = ref('')

const allOrgs = ref<any[]>([])
const selectedOrgId = ref<string>()

const billingType = ref<string>('guardian')
const agreedPrice = ref<number | null>(null)
const priceCurrency = ref('')

const enrolledIds = computed(() => new Set(enrolledStudents.value.map((e) => e.studentId)))
const createdIds = computed(() => new Set(createdStudents.value.map((s) => s.id)))

const availableStudentOptions = computed(() =>
  allStudents.value
    .filter((s) => !enrolledIds.value.has(s.id) && !createdIds.value.has(s.id))
    .map((s) => ({ value: s.id, label: s.name || s.id })),
)

const allSelectedStudents = computed(() => {
  const fromSelect = selectedStudentIds.value.map((sid) => {
    const s = allStudents.value.find((st) => st.id === sid)
    return { id: sid, name: s?.name || sid }
  })
  return [...fromSelect, ...createdStudents.value]
})

const guardianOptions = computed(() =>
  allGuardians.value.map((g) => ({
    value: g.id,
    label: `${g.name}${g.relationship ? ` (${g.relationship})` : ''}`,
  })),
)

const orgOptions = computed(() =>
  allOrgs.value.map((o: any) => ({ value: o.id, label: o.name || o.id })),
)

const needsNewGuardian = computed(() =>
  billingType.value === 'guardian' && showNewGuardian.value && !selectedGuardianId.value,
)

const canEnroll = computed(() => {
  if (allSelectedStudents.value.length === 0) return false
  if (billingType.value === 'guardian') {
    if (selectedGuardianId.value) return true
    if (needsNewGuardian.value && newGuardianFirstName.value.trim() && newGuardianLastName.value.trim()) return true
    return false
  }
  if (billingType.value === 'institutional') return !!selectedOrgId.value
  return false
})

function filterOption(input: string, option: any) {
  return String(option.label || '').toLowerCase().includes(input.toLowerCase())
}

function removeStudent(sid: string) {
  selectedStudentIds.value = selectedStudentIds.value.filter((s) => s !== sid)
  createdStudents.value = createdStudents.value.filter((s) => s.id !== sid)
}

async function addNewStudent() {
  const first = newStudentFirstName.value.trim()
  const last = newStudentLastName.value.trim()
  if (!first || !last) return
  try {
    const person = await createPerson({ given_name: first, family_name: last, email: '' })
    const fullName = `${first} ${last}`
    const student = await createStudent({ name: fullName, personId: person.id })
    createdStudents.value.push({ id: student.id, name: fullName })
    newStudentFirstName.value = ''
    newStudentLastName.value = ''
    showNewStudent.value = false
    message.success(`Student "${fullName}" created`)
  } catch (err: any) {
    message.error(err?.data?.error || 'Failed to create student')
  }
}

async function openEnrollModal() {
  showEnrollModal.value = true
  selectedStudentIds.value = []
  createdStudents.value = []
  showNewStudent.value = false
  newStudentFirstName.value = ''
  newStudentLastName.value = ''
  selectedGuardianId.value = undefined
  showNewGuardian.value = false
  newGuardianFirstName.value = ''
  newGuardianLastName.value = ''
  newGuardianRelationship.value = ''
  newGuardianPhone.value = ''
  newGuardianEmail.value = ''
  selectedOrgId.value = undefined
  billingType.value = 'guardian'
  agreedPrice.value = null
  priceCurrency.value = resource.value?.priceCurrency || 'TTD'

  try {
    const [studRes, guardRes, orgRes] = await Promise.all([
      listStudents('', 100),
      listGuardians('', 100),
      listOrganizations('', 100),
    ])
    allStudents.value = studRes.data
    allGuardians.value = guardRes.data
    allOrgs.value = orgRes.data
  } catch {
    // best effort
  }
}

async function handleBulkEnroll() {
  if (!canEnroll.value) return
  enrolling.value = true
  try {
    let guardianId = selectedGuardianId.value
    let billingOrgId = selectedOrgId.value

    if (billingType.value === 'guardian' && needsNewGuardian.value) {
      const person = await createPerson({
        given_name: newGuardianFirstName.value.trim(),
        family_name: newGuardianLastName.value.trim(),
        email: newGuardianEmail.value || '',
      })
      const firstStudentId = allSelectedStudents.value[0]?.id
      const fullName = `${newGuardianFirstName.value.trim()} ${newGuardianLastName.value.trim()}`
      const guardian = await createGuardian({
        name: fullName,
        studentId: firstStudentId,
        personId: person.id,
        relationship: newGuardianRelationship.value || undefined,
        phone: newGuardianPhone.value || undefined,
        email: newGuardianEmail.value || undefined,
        isPrimaryBilling: true,
      })
      guardianId = guardian.id

      for (let i = 1; i < allSelectedStudents.value.length; i++) {
        await createGuardian({
          name: fullName,
          studentId: allSelectedStudents.value[i].id,
          personId: person.id,
          relationship: newGuardianRelationship.value || undefined,
          phone: newGuardianPhone.value || undefined,
          email: newGuardianEmail.value || undefined,
          isPrimaryBilling: true,
        })
      }
    }

    let created = 0
    for (const student of allSelectedStudents.value) {
      const enrollmentData: Record<string, any> = {
        studentId: student.id,
        courseInstanceId: id.value,
        billingType: billingType.value,
        priceCurrency: priceCurrency.value || undefined,
        paymentCadence: resource.value?.paymentCadence || undefined,
      }
      const price = agreedPrice.value ?? resource.value?.price
      if (price !== null && price !== undefined) {
        enrollmentData.agreedPrice = price
      }
      if (billingType.value === 'guardian' && guardianId) {
        enrollmentData.guardianId = guardianId
      }
      if (billingType.value === 'institutional' && billingOrgId) {
        enrollmentData.billingOrganizationId = billingOrgId
      }
      await createEnrollment(enrollmentData)
      created++
    }

    showEnrollModal.value = false
    message.success(`${created} student(s) enrolled`)
    await preloadType('student', true)
    await preloadType('guardian', true)
    await fetchEnrollments()
    refreshKey.value++
  } catch (err: any) {
    message.error(err?.data?.error || err?.message || 'Enrollment failed')
  } finally {
    enrolling.value = false
  }
}

// Child sections
interface ChildSection {
  slug: string
  name: string
  columns: Record<string, any>[]
  items: any[]
  loading: boolean
  hasMore: boolean
  cursor: string
  loadMore: () => void
}

const childSections = ref<ChildSection[]>([])

function discoverChildSlugs(): string[] {
  const configured = getChildren(typeSlug)
  if (configured.length) return configured
  const discovered: string[] = []
  for (const rt of resourceTypes.value) {
    if (rt.slug === typeSlug || !rt.schema?.properties) continue
    for (const prop of Object.values(rt.schema.properties) as any[]) {
      if (prop['x-resource-type'] === typeSlug) {
        // Skip enrollment — it's shown in its own section above
        if (rt.slug !== 'enrollment') discovered.push(rt.slug)
        break
      }
    }
  }
  return discovered
}

function initChildSections() {
  const childSlugs = discoverChildSlugs()
  if (!childSlugs.length) return

  const sections: ChildSection[] = []
  for (const slug of childSlugs) {
    const childType = getBySlug(slug)
    const columns = childType?.schema ? schemaToColumns(childType.schema) : []
    const section: ChildSection = reactive({
      slug,
      name: childType?.name || slug,
      columns,
      items: [],
      loading: false,
      hasMore: false,
      cursor: '',
      loadMore: () => fetchChildResources(section),
    })
    sections.push(section)
  }
  childSections.value = sections

  for (const section of sections) {
    fetchChildResources(section)
  }
}

function findReferenceField(childSlug: string): string | undefined {
  const childType = getBySlug(childSlug)
  if (!childType?.schema?.properties) return undefined
  for (const [key, prop] of Object.entries(childType.schema.properties) as [string, any][]) {
    if (prop['x-resource-type'] === typeSlug) return key
  }
  const camelSlug = typeSlug.replace(/-([a-z])/g, (_: string, c: string) => c.toUpperCase())
  const fallbackKey = camelSlug + 'Id'
  if (fallbackKey in childType.schema.properties) return fallbackKey
  return undefined
}

async function fetchChildResources(section: ChildSection) {
  section.loading = true
  try {
    const api = useResourceApi(section.slug)
    const refField = findReferenceField(section.slug)
    const filters = refField ? { [refField]: { eq: id.value } } : undefined
    const res = await api.list(section.cursor, 100, '', '', filters)
    section.items = [...section.items, ...res.data]
    section.cursor = res.cursor
    section.hasMore = res.has_more
  } catch {
    // Loading state is cleared in finally; empty section signals failure
  } finally {
    section.loading = false
  }
}

// Enrollment Tasks
const enrollmentTasks = ref<any[]>([])
const tasksLoading = ref(false)

const taskColumns = [
  { title: 'Student', key: 'student' },
  { title: 'Task', key: 'name', dataIndex: 'name' },
  { title: 'Required', key: 'required', dataIndex: 'required', customRender: ({ text }: any) => text ? 'Yes' : '' },
  { title: 'Status', key: 'status' },
]

async function fetchEnrollmentTasks() {
  tasksLoading.value = true
  try {
    const res = await listTasks('', 200, '', '', { courseInstanceId: { eq: id.value } })
    enrollmentTasks.value = (res.data || []).sort((a: any, b: any) => {
      const aComplete = a.actionStatus === 'CompletedActionStatus' ? 1 : 0
      const bComplete = b.actionStatus === 'CompletedActionStatus' ? 1 : 0
      if (aComplete !== bComplete) return aComplete - bComplete
      return (a.position || 0) - (b.position || 0)
    })
  } finally {
    tasksLoading.value = false
  }
}

async function toggleTask(task: any, completed: boolean) {
  const status = completed ? 'CompletedActionStatus' : 'PotentialActionStatus'
  const oldStatus = task.actionStatus
  const updateData: Record<string, any> = { ...task, actionStatus: status }
  if (completed) {
    updateData.completedDate = new Date().toISOString()
  } else {
    delete updateData.completedDate
    delete updateData.completedBy
  }
  delete updateData.id
  delete updateData.type
  task.actionStatus = status
  try {
    await updateTask(task.id, updateData)
  } catch {
    task.actionStatus = oldStatus
    message.error('Failed to update task')
  }
}

async function loadCourseInstance() {
  loading.value = true
  resource.value = null
  enrolledStudents.value = []
  if (!loaded.value) await fetchResourceTypes()
  try {
    resource.value = await get(id.value)
  } finally {
    loading.value = false
  }
  if (resource.value) {
    for (const field of fields.value) {
      if (field.resourceType) preloadType(field.resourceType)
    }
    preloadType('student')
    preloadType('guardian')
    fetchEnrollments()
    initChildSections()
    fetchEnrollmentTasks()
  }
}

watch(() => id.value, () => loadCourseInstance(), { immediate: true })
</script>
