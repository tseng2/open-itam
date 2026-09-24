<template>
  <div class="dispatches-view">
    <div class="page-header">
      <div class="title-area">
        <h2>外派出差终端管理</h2>
        <span class="subtitle">长期出差 / 涉密客户现场终端登记：预计归期与隔离标记是超期告警与失联判定的依据，登记与归还自动写入资产履历</span>
      </div>
      <div class="actions">
        <el-button type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 外派登记
        </el-button>
      </div>
    </div>

    <!-- 筛选面板 -->
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="query" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="query.company_id" placeholder="全部公司" clearable style="width: 180px">
            <el-option
              v-for="item in companies"
              :key="item.id"
              :label="item.name"
              :value="item.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="状态">
          <el-select v-model="query.status" placeholder="全部状态" clearable style="width: 130px">
            <el-option label="外派中" :value="10" />
            <el-option label="已归还" :value="20" />
            <el-option label="已作废" :value="30" />
          </el-select>
        </el-form-item>

        <el-form-item label="超期">
          <el-radio-group v-model="query.overdue">
            <el-radio-button value="">全部</el-radio-button>
            <el-radio-button value="true">仅超期未归</el-radio-button>
            <el-radio-button value="false">未超期</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
          <el-button @click="resetQuery">重置</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 外派记录表格 -->
    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column label="外派资产" min-width="200">
          <template #default="{ row }">
            <div><span class="tag-code">{{ row.asset?.asset_tag || ('资产#' + row.asset_id) }}</span></div>
            <div class="sub-text">{{ [row.asset?.brand, row.asset?.model].filter(Boolean).join(' ') || '-' }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="borrower_name" label="外派负责人" width="110" />
        <el-table-column prop="destination" label="目的地 / 客户现场" min-width="150" show-overflow-tooltip />
        <el-table-column label="外派日期" width="110">
          <template #default="{ row }">{{ formatDate(row.dispatched_at) || '-' }}</template>
        </el-table-column>
        <el-table-column label="预计归期" width="170">
          <template #default="{ row }">
            <span :class="{ 'overdue-text': isOverdue(row) }">{{ formatDate(row.expected_return_at) || '-' }}</span>
            <el-tag v-if="isOverdue(row)" type="danger" size="small" style="margin-left: 6px">超期未归</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="隔离 / 格式化" width="150">
          <template #default="{ row }">
            <el-tag v-if="row.isolation_offline" type="warning" size="small">保密隔离</el-tag>
            <el-tag v-if="row.expect_wipe" type="danger" size="small" style="margin-left: 6px">格式化归还</el-tag>
            <span v-if="!row.isolation_offline && !row.expect_wipe" class="empty-cell">-</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row)">{{ statusText(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="归还时间" width="110">
          <template #default="{ row }">{{ formatDate(row.returned_at) || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <template v-if="row.status === 10">
              <el-button link type="primary" size="small" @click="confirmReturn(row)">归还</el-button>
              <el-button link type="danger" size="small" @click="confirmCancel(row)">作废</el-button>
            </template>
            <span v-else class="empty-cell">-</span>
          </template>
        </el-table-column>
        <template #empty><el-empty description="暂无外派记录，点击右上角「外派登记」新增" /></template>
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

    <!-- 外派登记对话框 -->
    <el-dialog v-model="showCreateDialog" title="外派登记" width="640px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="150px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select
            v-model="form.company_id"
            placeholder="选择资产所属公司"
            style="width: 100%"
            @change="onCompanyChange"
          >
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="外派资产" prop="asset_id">
          <el-select
            v-model="form.asset_id"
            filterable
            remote
            :remote-method="searchAssets"
            :loading="assetSearching"
            placeholder="先选公司，再按编码/品牌/型号搜索"
            style="width: 100%"
          >
            <el-option
              v-for="item in assetOptions"
              :key="item.id"
              :label="`${item.asset_tag}（${[item.brand, item.model].filter(Boolean).join(' ')}）`"
              :value="item.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="外派负责人" prop="borrower_name">
          <el-input v-model="form.borrower_name" placeholder="携带/使用该终端的员工姓名" />
        </el-form-item>
        <el-form-item label="目的地 / 客户现场" prop="destination">
          <el-input v-model="form.destination" placeholder="如：深圳 XX 客户现场" />
        </el-form-item>
        <el-form-item label="外派日期">
          <el-date-picker v-model="form.dispatched_at" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
        </el-form-item>
        <el-form-item label="预计归期" prop="expected_return_at">
          <el-date-picker
            v-model="form.expected_return_at"
            type="date"
            value-format="YYYY-MM-DD"
            placeholder="超期未归将进入高危告警"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="保密现场不可联网">
          <el-switch v-model="form.isolation_offline" />
          <span class="form-tip">开启后离线视为"预期内"，不按失联处理</span>
        </el-form-item>
        <el-form-item label="预期格式化归还">
          <el-switch v-model="form.expect_wipe" />
          <span class="form-tip">涉密客户要求，归还后需走格式化检疫流程</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" placeholder="项目背景、联系方式等" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitCreate">确认登记</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
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

const query = reactive({
  company_id: '',
  status: '',
  overdue: '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  company_id: '',
  asset_id: '',
  borrower_name: '',
  destination: '',
  dispatched_at: '',
  expected_return_at: '',
  isolation_offline: false,
  expect_wipe: false,
  remark: '',
})

const rules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  asset_id: [{ required: true, message: '请选择外派资产', trigger: 'change' }],
  borrower_name: [{ required: true, message: '请输入外派负责人', trigger: 'blur' }],
  destination: [{ required: true, message: '请输入目的地/客户现场', trigger: 'blur' }],
  expected_return_at: [{ required: true, message: '请选择预计归期', trigger: 'change' }],
}

// 超期为计算属性：外派中且已过预计归期即视为超期未归（高危）
function isOverdue(row) {
  return row.status === 10 && new Date(row.expected_return_at).getTime() < Date.now()
}

function statusText(row) {
  if (row.status === 20) return '已归还'
  if (row.status === 30) return '已作废'
  return '外派中'
}

function statusTagType(row) {
  if (row.status === 20) return 'success'
  if (row.status === 30) return 'info'
  return isOverdue(row) ? 'danger' : 'primary'
}

function formatDate(t) {
  if (!t) return ''
  const date = new Date(t)
  if (isNaN(date.getTime())) return ''
  return date.toISOString().slice(0, 10)
}

// date-picker 产出 YYYY-MM-DD，转 UTC ISO 以匹配后端 time.Time 的 RFC3339 解析
function toUTCISO(dateStr) {
  if (!dateStr) return null
  return new Date(dateStr).toISOString()
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch (err) {
    console.error('failed to fetch companies', err)
  }
}

async function fetchList() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (query.company_id) params.append('company_id', query.company_id)
    if (query.status) params.append('status', query.status)
    if (query.overdue) params.append('overdue', query.overdue)
    params.append('page', query.page)
    params.append('page_size', query.page_size)

    const res = await api(`/api/v1/dispatches?${params.toString()}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载外派记录失败')
  } finally {
    loading.value = false
  }
}

function resetQuery() {
  query.company_id = ''
  query.status = ''
  query.overdue = ''
  query.page = 1
  fetchList()
}

function onCompanyChange() {
  form.asset_id = ''
  assetOptions.value = []
  if (form.company_id) searchAssets('')
}

async function searchAssets(keyword) {
  if (!form.company_id) return
  assetSearching.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', form.company_id)
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

function openCreateDialog() {
  Object.assign(form, {
    company_id: '',
    asset_id: '',
    borrower_name: '',
    destination: '',
    dispatched_at: new Date().toISOString().slice(0, 10),
    expected_return_at: '',
    isolation_offline: false,
    expect_wipe: false,
    remark: '',
  })
  assetOptions.value = []
  showCreateDialog.value = true
}

async function submitCreate() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      await api('/api/v1/dispatches', {
        method: 'POST',
        body: JSON.stringify({
          company_id: form.company_id,
          asset_id: form.asset_id,
          borrower_name: form.borrower_name,
          destination: form.destination,
          dispatched_at: toUTCISO(form.dispatched_at),
          expected_return_at: toUTCISO(form.expected_return_at),
          isolation_offline: form.isolation_offline,
          expect_wipe: form.expect_wipe,
          remark: form.remark,
        }),
      })
      ElMessage.success('外派登记成功，已写入资产履历')
      showCreateDialog.value = false
      fetchList()
    } catch (err) {
      ElMessage.error(err.message || '登记失败')
    } finally {
      submitting.value = false
    }
  })
}

async function confirmReturn(row) {
  const tag = row.asset?.asset_tag || ('资产#' + row.asset_id)
  try {
    await ElMessageBox.confirm(
      `确认终端 ${tag} 已归还？归还时间将记为现在，并写入资产履历。`,
      '外派归还确认',
      { type: 'warning', confirmButtonText: '确认归还', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await api(`/api/v1/dispatches/${row.id}/return`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id }),
    })
    ElMessage.success('已登记归还')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '归还失败')
  }
}

async function confirmCancel(row) {
  const tag = row.asset?.asset_tag || ('资产#' + row.asset_id)
  try {
    await ElMessageBox.confirm(
      `作废 ${tag} 的外派登记？仅用于误登记修正，不影响资产其他状态，也不写履历。`,
      '外派作废确认',
      { type: 'warning', confirmButtonText: '确认作废', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await api(`/api/v1/dispatches/${row.id}/cancel`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id }),
    })
    ElMessage.success('已作废该外派记录')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '作废失败')
  }
}

onMounted(() => {
  fetchCompanies()
  fetchList()
})
</script>

<style scoped>
.dispatches-view {
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
.overdue-text {
  color: #dc2626;
  font-weight: 600;
}
.form-tip {
  margin-left: 10px;
  font-size: 12px;
  color: #9ca3af;
}
.pagination-area {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
