<template>
  <div class="storage-lendings-view">
    <div class="page-header">
      <div class="title-area">
        <h2>移动存储领用</h2>
        <span class="subtitle">U 盘 / 移动硬盘等领用与归还登记：领用建账、归还回填归期与数量；加密认证标记加密合规</span>
      </div>
      <div class="actions">
        <el-button v-if="isAdmin" type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 登记领用
        </el-button>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="companyId" placeholder="全部公司" clearable filterable style="width: 200px" @change="search">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="领用部门">
          <el-input v-model="department" placeholder="模糊匹配" clearable style="width: 160px" @keyup.enter="search" @clear="search" />
        </el-form-item>
        <el-form-item label="领用人">
          <el-input v-model="borrower" placeholder="模糊匹配" clearable style="width: 140px" @keyup.enter="search" @clear="search" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="search">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="领用人" width="110">
          <template #default="{ row }">
            <strong>{{ row.borrower }}</strong>
            <div class="sub-text">{{ row.department || '部门未登记' }}</div>
          </template>
        </el-table-column>
        <el-table-column label="设备" min-width="180">
          <template #default="{ row }">
            <div>{{ [row.brand, row.spec].filter(Boolean).join(' / ') || '品牌规格未登记' }}</div>
            <div class="sub-text">{{ row.device_code || '无设备编码' }}</div>
          </template>
        </el-table-column>
        <el-table-column label="数量" width="70" align="center">
          <template #default="{ row }">{{ row.quantity }}</template>
        </el-table-column>
        <el-table-column label="领用日期" width="110">
          <template #default="{ row }">{{ formatDate(row.borrow_date) || '—' }}</template>
        </el-table-column>
        <el-table-column label="归还情况" width="170">
          <template #default="{ row }">
            <el-tag v-if="!row.return_date" type="info" size="small">未归还</el-tag>
            <template v-else>
              <span>{{ formatDate(row.return_date) }}</span>
              <span v-if="row.return_qty > 0" class="sub-text">（已还 {{ row.return_qty }}）</span>
            </template>
          </template>
        </el-table-column>
        <el-table-column label="加密认证" width="90" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.sec_certified" type="success" size="small">已认证</el-tag>
            <span v-else class="empty-cell">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '—' }}</template>
        </el-table-column>
        <el-table-column v-if="isAdmin" label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
            <el-button v-if="!row.return_date" link type="success" size="small" @click="openReturnDialog(row)">归还</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无领用记录，右上角登记第一笔" />
        </template>
      </el-table>

      <div class="pagination-area">
        <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total"
          :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
          @size-change="fetchList" @current-change="fetchList" />
      </div>
    </el-card>

    <!-- 登记 / 编辑领用：编辑走全量 PUT，清空的日期字段必须显式送 null -->
    <el-dialog v-model="showDialog" :title="editMode ? '编辑领用记录' : '登记移动存储领用'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="110px">
        <el-form-item v-if="!editMode" label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="领用人" prop="borrower">
          <el-input v-model="form.borrower" placeholder="如：李四" />
        </el-form-item>
        <el-form-item label="领用部门">
          <el-input v-model="form.department" placeholder="如：研发部" />
        </el-form-item>
        <el-form-item label="领用日期">
          <el-date-picker v-model="form.borrow_date" type="date" value-format="YYYY-MM-DD" placeholder="留空可后续补录" style="width: 100%" />
        </el-form-item>
        <el-form-item label="品牌">
          <el-input v-model="form.brand" placeholder="如：金士顿" />
        </el-form-item>
        <el-form-item label="规格">
          <el-input v-model="form.spec" placeholder="如：128G / USB3.0" />
        </el-form-item>
        <el-form-item label="设备编码">
          <el-input v-model="form.device_code" placeholder="如：USB-0001" />
        </el-form-item>
        <el-form-item label="数量">
          <el-input-number v-model="form.quantity" :min="1" :max="99999" style="width: 200px" />
        </el-form-item>
        <el-form-item label="加密认证">
          <el-switch v-model="form.sec_certified" />
          <div class="form-tip">开启表示该设备已通过公司统一加密系统认证（具体产品见管理规范，系统不绑定品牌）</div>
        </el-form-item>
        <template v-if="editMode">
          <el-form-item label="归还日期">
            <el-date-picker v-model="form.return_date" type="date" value-format="YYYY-MM-DD" placeholder="清空即撤销归还登记" style="width: 100%" />
          </el-form-item>
          <el-form-item label="归还数量">
            <el-input-number v-model="form.return_qty" :min="0" :max="99999" style="width: 200px" />
          </el-form-item>
        </template>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <div v-if="editMode" class="dialog-tip">编辑为全量保存：清空日期即清空登记（保存时显式送 null），数量与认证按表单值覆盖</div>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ editMode ? '保存' : '登记' }}</el-button>
      </template>
    </el-dialog>

    <!-- 归还回填：PUT 只送 return_date / return_qty，其余字段保持 -->
    <el-dialog v-model="showReturnDialog" title="归还登记" width="420px" destroy-on-close>
      <el-form label-width="100px">
        <el-form-item label="归还日期">
          <el-date-picker v-model="returnForm.return_date" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
        </el-form-item>
        <el-form-item label="归还数量">
          <el-input-number v-model="returnForm.return_qty" :min="1" :max="99999" style="width: 200px" />
          <div class="form-tip">领用数量 {{ returnForm.total }}，部分归还可调低</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showReturnDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitReturn">确认归还</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage } from 'element-plus'

// 移动存储领用（阶段一遗留清欠）：GET/POST /api/v1/storage-lendings +
// PUT /:id。PUT 指针字段契约：未传保持、显式 null 清空——编辑对话框
// 全量送字段，清空的日期必须带 null，绝不能漏字段（漏了语义就变成保持）。
// 写面（登记/编辑/归还）服务端 RBAC 收口 admin（2026-09-26），前端同步 gate
const companies = ref([])
const companyId = ref('')
const department = ref('')
const borrower = ref('')
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const submitting = ref(false)

const showDialog = ref(false)
const editMode = ref(false)
const editingId = ref(0)
const formRef = ref(null)

const showReturnDialog = ref(false)
const returningRow = ref(null)
const returnForm = reactive({ return_date: '', return_qty: 1, total: 0 })

const isAdmin = computed(() => {
  try {
    const u = JSON.parse(localStorage.getItem('itagent_user') || 'null')
    return !!u && (u.role === 'admin' || u.role === 'super_admin')
  } catch {
    return false
  }
})

const emptyForm = () => ({
  company_id: '', borrower: '', department: '', borrow_date: '',
  brand: '', spec: '', device_code: '', quantity: 1,
  sec_certified: false, return_date: '', return_qty: 0, remark: '',
})
const form = reactive(emptyForm())

const formRules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  borrower: [{ required: true, message: '请输入领用人', trigger: 'blur' }],
}

function formatDate(t) {
  if (!t) return ''
  const d = new Date(t)
  return isNaN(d.getTime()) ? '' : d.toISOString().slice(0, 10)
}

// 日期送 ISO 全格式（time.Time 绑定要求 RFC3339）；清空必须显式 null
function toISOPayload(v) {
  return v ? new Date(v).toISOString() : null
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch { /* 下拉失败不阻塞列表 */ }
}

function search() {
  page.value = 1
  fetchList()
}

async function fetchList() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (companyId.value) params.append('company_id', companyId.value)
    if (department.value.trim()) params.append('department', department.value.trim())
    if (borrower.value.trim()) params.append('borrower', borrower.value.trim())
    params.append('page', page.value)
    params.append('page_size', pageSize.value)
    const res = await api(`/api/v1/storage-lendings?${params}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载领用记录失败')
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  Object.assign(form, emptyForm())
  form.company_id = companyId.value || ''
  editMode.value = false
  showDialog.value = true
}

function openEditDialog(row) {
  Object.assign(form, emptyForm(), {
    company_id: row.company_id,
    borrower: row.borrower || '',
    department: row.department || '',
    borrow_date: formatDate(row.borrow_date),
    brand: row.brand || '',
    spec: row.spec || '',
    device_code: row.device_code || '',
    quantity: row.quantity || 1,
    sec_certified: !!row.sec_certified,
    return_date: formatDate(row.return_date),
    return_qty: row.return_qty || 0,
    remark: row.remark || '',
  })
  editingId.value = row.id
  editMode.value = true
  showDialog.value = true
}

async function submit() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      if (editMode.value) {
        // 全量 PUT：清空的日期显式 null，其余字段按表单值逐一覆盖
        const body = {
          department: form.department.trim(),
          borrower: form.borrower.trim(),
          borrow_date: toISOPayload(form.borrow_date),
          brand: form.brand.trim(),
          spec: form.spec.trim(),
          device_code: form.device_code.trim(),
          quantity: form.quantity,
          return_date: toISOPayload(form.return_date),
          return_qty: form.return_qty,
          sec_certified: form.sec_certified,
          remark: form.remark,
        }
        await api(`/api/v1/storage-lendings/${editingId.value}`, { method: 'PUT', body: JSON.stringify(body) })
        ElMessage.success('领用记录已保存')
      } else {
        const body = {
          company_id: form.company_id,
          department: form.department.trim(),
          borrower: form.borrower.trim(),
          borrow_date: toISOPayload(form.borrow_date),
          brand: form.brand.trim(),
          spec: form.spec.trim(),
          device_code: form.device_code.trim(),
          quantity: form.quantity,
          sec_certified: form.sec_certified,
          remark: form.remark,
        }
        await api('/api/v1/storage-lendings', { method: 'POST', body: JSON.stringify(body) })
        ElMessage.success('领用已登记')
      }
      showDialog.value = false
      fetchList()
    } catch (err) {
      ElMessage.error(err.message || '保存失败')
    } finally {
      submitting.value = false
    }
  })
}

function openReturnDialog(row) {
  returningRow.value = row
  returnForm.return_date = new Date().toISOString().slice(0, 10)
  returnForm.return_qty = row.quantity || 1
  returnForm.total = row.quantity || 0
  showReturnDialog.value = true
}

async function submitReturn() {
  if (!returningRow.value) return
  submitting.value = true
  try {
    const body = {
      return_date: toISOPayload(returnForm.return_date),
      return_qty: returnForm.return_qty,
    }
    await api(`/api/v1/storage-lendings/${returningRow.value.id}`, { method: 'PUT', body: JSON.stringify(body) })
    ElMessage.success('归还已登记')
    showReturnDialog.value = false
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '归还登记失败')
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  fetchCompanies()
  fetchList()
})
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.title-area h2 { margin: 0; font-size: 20px; color: #1f2937; }
.subtitle { font-size: 12px; color: #94a3b8; }
.filter-card { margin-bottom: 16px; }
.filter-form { margin-bottom: -18px; }
.table-card { border-radius: 10px; }
.pagination-area { display: flex; justify-content: flex-end; margin-top: 16px; }
.sub-text { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.empty-cell { color: #cbd5e1; }
.form-tip { font-size: 12px; color: #94a3b8; line-height: 1.4; margin-top: 4px; }
.dialog-tip { font-size: 12px; color: #d97706; background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; padding: 8px 12px; margin-top: 8px; }
</style>
