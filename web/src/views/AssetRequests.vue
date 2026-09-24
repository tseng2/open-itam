<template>
  <div class="asset-requests-view">
    <div class="page-header">
      <div class="title-area">
        <h2>设备申请审批</h2>
        <span class="subtitle">从库存中申请设备：提交后等待 IT 审批，通过即绑定领用人并写入资产履历；短期借用请填写预计归还时间</span>
      </div>
      <div class="actions">
        <el-button type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 提交申请
        </el-button>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="query" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="query.company_id" placeholder="选择公司" style="width: 180px" @change="fetchList">
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="query.status" placeholder="全部状态" clearable style="width: 130px" @change="fetchList">
            <el-option label="待审批" :value="10" />
            <el-option label="已通过" :value="20" />
            <el-option label="已驳回" :value="30" />
            <el-option label="已取消" :value="40" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="isPrivileged" label="申请人">
          <el-select
            v-model="query.applicant_id"
            placeholder="全部申请人"
            clearable
            filterable
            remote
            :remote-method="searchUsers"
            :loading="userSearching"
            style="width: 180px"
            @change="fetchList"
          >
            <el-option v-for="u in userOptions" :key="u.id" :label="u.real_name || u.username" :value="u.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="申请资产" min-width="200">
          <template #default="{ row }">
            <div><span class="tag-code">{{ row.asset?.asset_tag || ('资产#' + row.asset_id) }}</span></div>
            <div class="sub-text">{{ [row.asset?.brand, row.asset?.model].filter(Boolean).join(' ') || row.asset?.category_name || '-' }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="applicant_name" label="申请人" width="100" />
        <el-table-column label="类型" width="170">
          <template #default="{ row }">
            <el-tag v-if="row.is_long_term" type="success" size="small">长期领用</el-tag>
            <el-tag v-else type="warning" size="small">短期借用</el-tag>
            <span v-if="!row.is_long_term" class="sub-text" style="margin-left: 6px">{{ formatDate(row.expected_return_at) }} 归还</span>
          </template>
        </el-table-column>
        <el-table-column prop="reason" label="申请事由" min-width="160" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="审批时间" width="160">
          <template #default="{ row }">{{ formatTime(row.approved_at) || '-' }}</template>
        </el-table-column>
        <el-table-column prop="decision_remark" label="批注" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">{{ row.decision_remark || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <template v-if="row.status === 10">
              <el-button v-if="isPrivileged" link type="success" size="small" @click="confirmApprove(row)">通过</el-button>
              <el-button v-if="isPrivileged" link type="danger" size="small" @click="confirmReject(row)">驳回</el-button>
              <el-button v-if="isPrivileged || row.applicant_id === currentUserId" link type="warning" size="small" @click="confirmCancel(row)">撤回</el-button>
            </template>
            <span v-else class="empty-cell">-</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="emptyText" />
        </template>
      </el-table>

      <div class="pagination-area">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="fetchList"
          @current-change="fetchList"
        />
      </div>
    </el-card>

    <!-- 提交申请对话框 -->
    <el-dialog v-model="showCreateDialog" title="提交设备申请" width="620px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="140px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%" @change="onCompanyChange">
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="isPrivileged" label="申请人">
          <el-select
            v-model="form.applicant_id"
            clearable
            filterable
            remote
            :remote-method="searchUsers"
            :loading="userSearching"
            placeholder="代员工录入时选择；留空则以本人名义"
            style="width: 100%"
          >
            <el-option v-for="u in userOptions" :key="u.id" :label="`${u.real_name || u.username}（${u.username}）`" :value="u.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="申请资产" prop="asset_id">
          <el-select
            v-model="form.asset_id"
            filterable
            remote
            :remote-method="searchAssets"
            :loading="assetSearching"
            placeholder="仅库存中且未被领用的资产，按编码/品牌/型号搜索"
            style="width: 100%"
          >
            <el-option
              v-for="item in assetOptions"
              :key="item.id"
              :label="`${item.asset_tag}（${[item.brand, item.model].filter(Boolean).join(' ') || item.category_name}）`"
              :value="item.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="借用性质">
          <el-radio-group v-model="form.is_long_term">
            <el-radio :value="true">长期领用</el-radio>
            <el-radio :value="false">短期借用</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="!form.is_long_term" label="预计归还时间" prop="expected_return_at">
          <el-date-picker
            v-model="form.expected_return_at"
            type="date"
            value-format="YYYY-MM-DD"
            placeholder="审批通过后归期将写入资产履历"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="申请事由" prop="reason">
          <el-input v-model="form.reason" type="textarea" :rows="2" placeholder="如：新项目开发用机 / 外勤备用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitCreate">提交申请</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

const loading = ref(false)
const submitting = ref(false)
const showCreateDialog = ref(false)
const formRef = ref(null)

const companies = ref([])
const items = ref([])
const total = ref(0)

const assetOptions = ref([])
const assetSearching = ref(false)
const userOptions = ref([])
const userSearching = ref(false)

const query = reactive({
  company_id: '',
  status: '',
  applicant_id: '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  company_id: '',
  applicant_id: '',
  asset_id: '',
  is_long_term: true,
  expected_return_at: '',
  reason: '',
})

const rules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  asset_id: [{ required: true, message: '请选择申请资产', trigger: 'change' }],
  reason: [{ required: true, message: '请填写申请事由', trigger: 'blur' }],
  expected_return_at: [{ required: true, message: '短期借用必须填写预计归还时间', trigger: 'change' }],
}

// 角色视角：admin/super_admin 看审批队列并可代录；user 只看自己的申请
const currentUser = computed(() => {
  try {
    const raw = localStorage.getItem('itagent_user')
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
})
const currentUserId = computed(() => currentUser.value?.id || 0)
const isPrivileged = computed(() => ['admin', 'super_admin'].includes(currentUser.value?.role))
const emptyText = computed(() =>
  isPrivileged.value ? '暂无设备申请' : '暂无申请记录，点击右上角提交申请'
)

function statusText(s) {
  return { 10: '待审批', 20: '已通过', 30: '已驳回', 40: '已取消' }[s] || `未知(${s})`
}

function statusTagType(s) {
  return { 10: 'warning', 20: 'success', 30: 'danger', 40: 'info' }[s] || 'info'
}

function formatDate(t) {
  if (!t) return ''
  const d = new Date(t)
  return isNaN(d.getTime()) ? '' : d.toISOString().slice(0, 10)
}

function formatTime(t) {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d.getTime())) return ''
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
    if (!query.company_id && companies.value.length) {
      query.company_id = companies.value[0].id
      fetchList()
    }
  } catch (err) {
    console.error('failed to fetch companies', err)
  }
}

async function fetchList() {
  if (!query.company_id) return
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', query.company_id)
    if (query.status) params.append('status', query.status)
    if (query.applicant_id) params.append('applicant_id', query.applicant_id)
    params.append('page', query.page)
    params.append('page_size', query.page_size)
    const res = await api(`/api/v1/asset-requests?${params.toString()}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载设备申请失败')
  } finally {
    loading.value = false
  }
}

function onCompanyChange() {
  form.asset_id = ''
  form.applicant_id = ''
  assetOptions.value = []
  userOptions.value = []
  if (form.company_id) {
    searchAssets('')
    if (isPrivileged.value) searchUsers('')
  }
}

async function searchAssets(keyword) {
  if (!form.company_id) return
  assetSearching.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', form.company_id)
    params.append('status', 10) // 仅库存中资产可申请
    if (keyword) params.append('keyword', keyword)
    params.append('page', 1)
    params.append('page_size', 20)
    const res = await api(`/api/v1/assets?${params.toString()}`)
    assetOptions.value = res.data?.items || []
  } catch (err) {
    ElMessage.error(err.message || '搜索资产失败')
  } finally {
    assetSearching.value = false
  }
}

async function searchUsers(keyword) {
  if (!query.company_id && !form.company_id) return
  userSearching.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', form.company_id || query.company_id)
    if (keyword) params.append('keyword', keyword)
    params.append('page', 1)
    params.append('page_size', 20)
    const res = await api(`/api/v1/users?${params.toString()}`)
    userOptions.value = res.data?.items || []
  } catch (err) {
    ElMessage.error(err.message || '搜索用户失败')
  } finally {
    userSearching.value = false
  }
}

function openCreateDialog() {
  Object.assign(form, {
    company_id: query.company_id,
    applicant_id: '',
    asset_id: '',
    is_long_term: true,
    expected_return_at: '',
    reason: '',
  })
  assetOptions.value = []
  userOptions.value = []
  if (form.company_id) {
    searchAssets('')
    if (isPrivileged.value) searchUsers('')
  }
  showCreateDialog.value = true
}

async function submitCreate() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      const body = {
        company_id: form.company_id,
        asset_id: form.asset_id,
        is_long_term: form.is_long_term,
        reason: form.reason,
      }
      if (isPrivileged.value && form.applicant_id) body.applicant_id = form.applicant_id
      if (!form.is_long_term && form.expected_return_at) {
        body.expected_return_at = new Date(form.expected_return_at).toISOString()
      }
      await api('/api/v1/asset-requests', { method: 'POST', body: JSON.stringify(body) })
      ElMessage.success('申请已提交，等待审批')
      showCreateDialog.value = false
      fetchList()
    } catch (err) {
      ElMessage.error(err.message || '提交失败')
    } finally {
      submitting.value = false
    }
  })
}

async function confirmApprove(row) {
  let remark = ''
  try {
    const { value } = await ElMessageBox.prompt(
      `通过 ${row.applicant_name} 对 ${row.asset?.asset_tag || '资产#' + row.asset_id} 的申请？审批通过即绑定领用人并写入资产履历。批注可留空。`,
      '审批通过',
      { confirmButtonText: '通过', cancelButtonText: '取消', inputPlaceholder: '审批批注（选填）' }
    )
    remark = value || ''
  } catch {
    return
  }
  try {
    await api(`/api/v1/asset-requests/${row.id}/approve`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id, decision_remark: remark }),
    })
    ElMessage.success('已通过，资产已绑定领用人')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '审批失败')
  }
}

async function confirmReject(row) {
  let remark = ''
  try {
    const { value } = await ElMessageBox.prompt(
      `驳回该申请？驳回原因将反馈给申请人。`,
      '驳回申请',
      { confirmButtonText: '驳回', cancelButtonText: '取消', inputPlaceholder: '驳回原因' }
    )
    remark = value || ''
  } catch {
    return
  }
  try {
    await api(`/api/v1/asset-requests/${row.id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id, decision_remark: remark }),
    })
    ElMessage.success('已驳回')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '驳回失败')
  }
}

async function confirmCancel(row) {
  try {
    await ElMessageBox.confirm('撤回该申请？撤回后资产不发生任何变化。', '撤回申请', { type: 'warning' })
  } catch {
    return
  }
  try {
    await api(`/api/v1/asset-requests/${row.id}/cancel`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id }),
    })
    ElMessage.success('已撤回')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '撤回失败')
  }
}

onMounted(() => {
  fetchCompanies()
})
</script>

<style scoped>
.asset-requests-view {
  padding: 4px;
}
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.title-area h2 {
  margin: 0 0 4px 0;
  font-size: 20px;
  color: #1f2937;
}
.subtitle {
  font-size: 13px;
  color: #6b7280;
}
.filter-card {
  margin-bottom: 16px;
  border-radius: 8px;
}
.filter-form {
  margin-bottom: -18px;
}
.table-card {
  border-radius: 8px;
}
.tag-code {
  font-family: monospace;
  font-weight: 600;
  color: #2563eb;
}
.sub-text {
  font-size: 12px;
  color: #9ca3af;
}
.empty-cell {
  color: #d1d5db;
}
.pagination-area {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
