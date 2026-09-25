<template>
  <div class="software-view">
    <div class="page-header">
      <div class="title-area">
        <h2>软件资产与授权管理</h2>
        <span class="subtitle">商业正版授权池：席位总数与资产挂接实时计数、到期/终止状态由日期自动派生、席位超用合规监控</span>
      </div>
      <div class="actions">
        <el-button v-if="isAdmin" type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 登记软件授权
        </el-button>
      </div>
    </div>

    <!-- 筛选面板 -->
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="companyId" placeholder="选择公司" clearable filterable style="width: 200px" @change="fetchList">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="关键字">
          <el-input v-model="keyword" placeholder="软件名称 / 厂商" clearable style="width: 200px" @keyup.enter="fetchList" @clear="fetchList" />
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="expiringOnly" @change="fetchList">仅看 30 天内到期</el-checkbox>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column label="软件名称" min-width="200">
          <template #default="{ row }">
            <strong>{{ row.name }}</strong>
            <div class="sub-text">{{ row.vendor || '厂商未登记' }}<span v-if="row.category"> · {{ row.category }}</span></div>
          </template>
        </el-table-column>
        <el-table-column label="席位占用" width="200">
          <template #default="{ row }">
            <template v-if="row.total_seats > 0">
              <el-progress
                :percentage="Math.min(100, Math.round((row.used_seats / row.total_seats) * 100))"
                :status="row.used_seats > row.total_seats ? 'exception' : (row.used_seats >= row.total_seats ? 'warning' : 'success')"
              />
              <div class="sub-text">{{ row.used_seats }} / {{ row.total_seats }} 席</div>
            </template>
            <span v-else>不限席位（已用 {{ row.used_seats }}）</span>
          </template>
        </el-table-column>
        <el-table-column label="授权密钥" min-width="160">
          <template #default="{ row }">
            <span v-if="row.license_key" class="license-key">{{ maskKey(row.license_key) }}</span>
            <span v-else class="empty-cell">未登记</span>
          </template>
        </el-table-column>
        <el-table-column label="到期日" width="120">
          <template #default="{ row }">
            {{ row.expiration_date ? row.expiration_date.slice(0, 10) : '永久授权' }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="isAdmin" label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="confirmDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无软件授权，右上角登记第一份授权" />
        </template>
      </el-table>

      <div class="pagination-area">
        <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total"
          :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
          @size-change="fetchList" @current-change="fetchList" />
      </div>
    </el-card>

    <!-- 登记 / 编辑授权对话框 -->
    <el-dialog v-model="showDialog" :title="editMode ? '编辑软件授权' : '登记软件授权'" width="640px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="110px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="软件名称" prop="name">
          <el-input v-model="form.name" placeholder="如：Microsoft 365 商业高级版" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="厂商">
              <el-input v-model="form.vendor" placeholder="Microsoft / Autodesk" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="分类">
              <el-input v-model="form.category" placeholder="办公套件 / 工程设计" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="授权密钥">
          <el-input v-model="form.license_key" placeholder="可留空（补录）" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="席位总数">
              <el-input-number v-model="form.total_seats" :min="0" style="width: 100%" />
              <div class="form-tip">0 = 不限席位</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="采购日期">
              <el-date-picker v-model="form.purchase_date" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="到期日">
              <el-date-picker v-model="form.expiration_date" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
              <div class="form-tip">留空 = 永久授权</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="终止日">
              <el-date-picker v-model="form.termination_date" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
              <div class="form-tip">非空即视为合同已终止（最高优先级）</div>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ editMode ? '保存' : '登记' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

// 软件许可（P2）：状态由服务端按日期派生（在用/已到期/已终止），
// 席位占用按资产挂接实时计数；写面仅 admin（服务端 RBAC 收口）
const companies = ref([])
const companyId = ref('')
const keyword = ref('')
const expiringOnly = ref(false)
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const submitting = ref(false)
const showDialog = ref(false)
const editMode = ref(false)
const formRef = ref(null)

const isAdmin = computed(() => {
  try {
    const u = JSON.parse(localStorage.getItem('itagent_user') || 'null')
    return !!u && (u.role === 'admin' || u.role === 'super_admin')
  } catch {
    return false
  }
})

const emptyForm = () => ({
  id: 0,
  company_id: '',
  name: '',
  vendor: '',
  category: '',
  license_key: '',
  total_seats: 0,
  purchase_date: '',
  expiration_date: '',
  termination_date: '',
  remark: '',
})
const form = reactive(emptyForm())

const formRules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  name: [{ required: true, message: '请输入软件名称', trigger: 'blur' }],
}

function statusText(s) {
  return { 10: '在用', 20: '已到期', 30: '已终止' }[s] || '未知'
}
function statusTag(s) {
  return { 10: 'success', 20: 'danger', 30: 'info' }[s] || 'info'
}
// 密钥脱敏：只亮出末 4 位
function maskKey(key) {
  if (key.length <= 4) return key
  return '••••••••' + key.slice(-4)
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch { /* 下拉失败不阻塞列表 */ }
}

async function fetchList() {
  if (!companyId.value) {
    items.value = []
    total.value = 0
    return
  }
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', companyId.value)
    if (keyword.value.trim()) params.append('keyword', keyword.value.trim())
    if (expiringOnly.value) params.append('expiring_days', 30)
    params.append('page', page.value)
    params.append('page_size', pageSize.value)
    const res = await api(`/api/v1/licenses?${params}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载软件许可失败')
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
    id: row.id,
    company_id: row.company_id,
    name: row.name,
    vendor: row.vendor || '',
    category: row.category || '',
    license_key: row.license_key || '',
    total_seats: row.total_seats || 0,
    purchase_date: row.purchase_date ? row.purchase_date.slice(0, 10) : '',
    expiration_date: row.expiration_date ? row.expiration_date.slice(0, 10) : '',
    termination_date: row.termination_date ? row.termination_date.slice(0, 10) : '',
    remark: row.remark || '',
  })
  editMode.value = true
  showDialog.value = true
}

async function submit() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      // date-picker 空串统一置 null，服务端 *time.Time 才能解析
      const payload = { ...form, purchase_date: form.purchase_date || null, expiration_date: form.expiration_date || null, termination_date: form.termination_date || null }
      if (editMode.value) {
        await api(`/api/v1/licenses/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
        ElMessage.success('授权已更新')
      } else {
        await api('/api/v1/licenses', { method: 'POST', body: JSON.stringify(payload) })
        ElMessage.success('授权登记成功')
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

async function confirmDelete(row) {
  try {
    await ElMessageBox.confirm(
      `确定删除「${row.name}」？席位仍被资产挂接时会被服务端拦截`,
      '删除软件授权', { type: 'warning' },
    )
  } catch { return }
  try {
    await api(`/api/v1/licenses/${row.id}?company_id=${row.company_id}`, { method: 'DELETE' })
    ElMessage.success('已删除')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '删除失败')
  }
}

onMounted(() => {
  fetchCompanies()
  fetchList()
})
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.title-area h2 {
  margin: 0;
  font-size: 20px;
  color: #1f2937;
}
.subtitle {
  font-size: 12px;
  color: #94a3b8;
}
.filter-card { margin-bottom: 16px; }
.filter-form { margin-bottom: -18px; }
.table-card { border-radius: 10px; }
.pagination-area { display: flex; justify-content: flex-end; margin-top: 16px; }
.sub-text { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.license-key { font-family: monospace; font-size: 12px; color: #475569; }
.empty-cell { color: #c0c4cc; }
.form-tip { font-size: 12px; color: #94a3b8; line-height: 1.4; margin-top: 4px; }
</style>
