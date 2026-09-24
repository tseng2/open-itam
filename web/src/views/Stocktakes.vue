<template>
  <div class="stocktakes-view">
    <div class="page-header">
      <div class="title-area">
        <h2>盘点任务管理</h2>
        <span class="subtitle">圈定范围快照成盘点明细 → 开始后生成一次性盘点码，手机扫码免登录核对；异常结果自动写入资产履历，核对"正常"自动确认待审的硬件变更</span>
      </div>
      <div class="actions">
        <el-button type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 新建盘点任务
        </el-button>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="query" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="query.company_id" placeholder="全部公司" clearable style="width: 180px">
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="query.status" placeholder="全部状态" clearable style="width: 130px">
            <el-option label="草稿" :value="10" />
            <el-option label="盘点中" :value="20" />
            <el-option label="已完成" :value="30" />
            <el-option label="已取消" :value="40" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
          <el-button @click="resetQuery">重置</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="name" label="任务名称" min-width="180" show-overflow-tooltip />
        <el-table-column label="进度（已盘/总数）" width="200">
          <template #default="{ row }">
            <el-progress
              :percentage="progressOf(row)"
              :stroke-width="14"
              :format="() => `${row.item_checked}/${row.item_total}`"
            />
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="开始时间" width="160">
          <template #default="{ row }">{{ formatTime(row.started_at) || '-' }}</template>
        </el-table-column>
        <el-table-column label="结束时间" width="160">
          <template #default="{ row }">{{ formatTime(row.finished_at) || '-' }}</template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="290" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openDetail(row)">明细</el-button>
            <el-button v-if="row.status === 10" link type="success" size="small" @click="confirmStart(row)">开始盘点</el-button>
            <template v-if="row.status === 20">
              <el-button link type="primary" size="small" @click="openTokenDialog(row)">盘点码</el-button>
              <el-button link type="warning" size="small" @click="printLabels(row)">打印标签</el-button>
              <el-button link type="success" size="small" @click="confirmFinish(row)">结束盘点</el-button>
            </template>
            <el-button v-if="row.status === 10 || row.status === 20" link type="danger" size="small" @click="confirmCancel(row)">取消</el-button>
          </template>
        </el-table-column>
        <template #empty><el-empty description="暂无盘点任务，点击右上角「新建盘点任务」圈定范围" /></template>
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

    <!-- 新建盘点任务对话框 -->
    <el-dialog v-model="showCreateDialog" title="新建盘点任务" width="640px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="140px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%" @change="onCompanyChange">
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="任务名称" prop="name">
          <el-input v-model="form.name" placeholder="如：2026 Q4 全量盘点" />
        </el-form-item>
        <el-form-item label="盘点范围">
          <el-radio-group v-model="form.scopeMode">
            <el-radio value="all">全部在册资产（不含已报废）</el-radio>
            <el-radio value="manual">手动圈定</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.scopeMode === 'manual'" label="选择资产">
          <el-select
            v-model="form.asset_ids"
            multiple
            filterable
            remote
            :remote-method="searchAssets"
            :loading="assetSearching"
            placeholder="按编码/品牌/型号搜索后勾选"
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
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" placeholder="盘点背景、责任范围等" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitCreate">创建任务</el-button>
      </template>
    </el-dialog>

    <!-- 盘点码对话框：明文令牌仅开始/轮换后这一次可见 -->
    <el-dialog v-model="showTokenDialog" title="本次盘点码（一次性显示）" width="460px" destroy-on-close>
      <div class="token-body">
        <div class="qr-box"><img v-if="tokenQR" :src="tokenQR" alt="盘点码二维码" /></div>
        <div class="token-text">{{ scanToken }}</div>
        <div class="token-hint">手机扫码或访问以上链接进入免登录核对页。任务结束/取消后盘点码即刻失效；如泄露可点下方按钮重新生成。</div>
        <div class="token-link">{{ scanURL }}</div>
        <div class="token-actions">
          <el-button size="small" @click="copyScanURL">复制链接</el-button>
          <el-button size="small" type="warning" @click="rotateToken" :loading="rotating">重新生成</el-button>
        </div>
      </div>
    </el-dialog>

    <!-- 明细抽屉 -->
    <el-drawer v-model="showDetailDrawer" :title="`盘点明细 — ${detail?.name || ''}`" size="72%">
      <div v-if="detail" class="detail-body">
        <div class="detail-meta">
          <el-tag :type="statusTagType(detail.status)">{{ statusText(detail.status) }}</el-tag>
          <span class="meta-item">建账总数 <b>{{ detailCounts.total }}</b></span>
          <span class="meta-item warn">待盘 <b>{{ detailCounts.pending }}</b></span>
          <span class="meta-item ok">正常 <b>{{ detailCounts.normal }}</b></span>
          <span class="meta-item lost">丢失 <b>{{ detailCounts.lost }}</b></span>
          <span class="meta-item damaged">损坏 <b>{{ detailCounts.damaged }}</b></span>
          <span class="meta-item scrapped">报废 <b>{{ detailCounts.scrapped }}</b></span>
        </div>

        <el-form :inline="true" class="item-filter">
          <el-form-item label="结果">
            <el-select v-model="itemQuery.result" placeholder="全部" clearable style="width: 130px" @change="fetchItems">
              <el-option label="待盘" :value="10" />
              <el-option label="正常" :value="20" />
              <el-option label="丢失" :value="30" />
              <el-option label="损坏" :value="40" />
              <el-option label="报废" :value="50" />
            </el-select>
          </el-form-item>
          <el-form-item label="编码">
            <el-input v-model="itemQuery.keyword" placeholder="资产编码模糊搜索" style="width: 200px" clearable @keyup.enter="fetchItems" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="fetchItems">筛选</el-button>
            <el-button v-if="detail.status === 20" type="primary" plain @click="printLabels(detail)">打印本任务标签</el-button>
          </el-form-item>
        </el-form>

        <el-table :data="itemRows" v-loading="itemLoading" stripe size="small">
          <el-table-column prop="asset_tag" label="资产编码" min-width="150">
            <template #default="{ row }">
              <span class="tag-code">{{ row.asset_tag }}</span>
            </template>
          </el-table-column>
          <el-table-column label="资产规格" min-width="150">
            <template #default="{ row }">
              {{ [row.asset?.brand, row.asset?.model].filter(Boolean).join(' ') || row.asset?.category_name || '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="expected_location" label="建账位置" min-width="120" show-overflow-tooltip>
            <template #default="{ row }">{{ row.expected_location || '-' }}</template>
          </el-table-column>
          <el-table-column label="核对结果" width="100">
            <template #default="{ row }">
              <el-tag :type="resultTagType(row.result)">{{ resultText(row.result) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="actual_location" label="实盘位置" min-width="110" show-overflow-tooltip>
            <template #default="{ row }">{{ row.actual_location || '-' }}</template>
          </el-table-column>
          <el-table-column prop="scanned_by" label="盘点人" width="90">
            <template #default="{ row }">{{ row.scanned_by || '-' }}</template>
          </el-table-column>
          <el-table-column label="核对时间" width="150">
            <template #default="{ row }">{{ formatTime(row.scanned_at) || '-' }}</template>
          </el-table-column>
          <el-table-column prop="remark" label="备注" min-width="110" show-overflow-tooltip>
            <template #default="{ row }">{{ row.remark || '-' }}</template>
          </el-table-column>
          <el-table-column v-if="detail.status === 20" label="操作" width="90" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="openCheckDialog(row)">修正</el-button>
            </template>
          </el-table-column>
        </el-table>

        <div class="pagination-area">
          <el-pagination
            v-model:current-page="itemQuery.page"
            v-model:page-size="itemQuery.page_size"
            :total="itemTotal"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            @size-change="fetchItems"
            @current-change="fetchItems"
          />
        </div>
      </div>
    </el-drawer>

    <!-- 管理端修正核对对话框 -->
    <el-dialog v-model="showCheckDialog" title="修正核对结果" width="480px" destroy-on-close>
      <el-form label-width="100px">
        <el-form-item label="资产">
          <span class="tag-code">{{ checkTarget?.asset_tag }}</span>
        </el-form-item>
        <el-form-item label="核对结果">
          <el-radio-group v-model="checkForm.result">
            <el-radio-button :value="20">正常</el-radio-button>
            <el-radio-button :value="30">丢失</el-radio-button>
            <el-radio-button :value="40">损坏</el-radio-button>
            <el-radio-button :value="50">报废</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="实盘位置">
          <el-input v-model="checkForm.actual_location" placeholder="实际发现位置（空则保持不变）" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="checkForm.remark" placeholder="空则保持不变" />
        </el-form-item>
        <el-form-item label="盘点人">
          <el-input v-model="checkForm.scanned_by" placeholder="默认「管理员」" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCheckDialog = false">取消</el-button>
        <el-button type="primary" :loading="checking" @click="submitCheck">提交修正</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import QRCode from 'qrcode'
import { api, getToken } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

const loading = ref(false)
const submitting = ref(false)
const checking = ref(false)
const rotating = ref(false)

const companies = ref([])
const items = ref([])
const total = ref(0)

const showCreateDialog = ref(false)
const showTokenDialog = ref(false)
const showDetailDrawer = ref(false)
const showCheckDialog = ref(false)
const formRef = ref(null)

const assetOptions = ref([])
const assetSearching = ref(false)

const detail = ref(null)
const itemRows = ref([])
const itemTotal = ref(0)
const itemLoading = ref(false)

const scanToken = ref('')
const tokenQR = ref('')
const checkTarget = ref(null)

const query = reactive({
  company_id: '',
  status: '',
  page: 1,
  page_size: 20,
})

const itemQuery = reactive({
  result: '',
  keyword: '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  company_id: '',
  name: '',
  scopeMode: 'all',
  asset_ids: [],
  remark: '',
})

const checkForm = reactive({
  result: 20,
  actual_location: '',
  remark: '',
  scanned_by: '',
})

const rules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  name: [{ required: true, message: '请输入任务名称', trigger: 'blur' }],
}

const detailCounts = computed(() => {
  const c = detail.value?.counts || {}
  return {
    total: Object.values(c).reduce((a, b) => a + b, 0),
    pending: c[10] || 0,
    normal: c[20] || 0,
    lost: c[30] || 0,
    damaged: c[40] || 0,
    scrapped: c[50] || 0,
  }
})

const scanURL = computed(() => {
  if (!scanToken.value) return ''
  return `${window.location.origin}/#/m/t/${scanToken.value}`
})

function progressOf(row) {
  if (!row.item_total) return 0
  return Math.round((row.item_checked / row.item_total) * 100)
}

function statusText(s) {
  return { 10: '草稿', 20: '盘点中', 30: '已完成', 40: '已取消' }[s] || `未知(${s})`
}

function statusTagType(s) {
  return { 10: 'info', 20: 'primary', 30: 'success', 40: 'warning' }[s] || 'info'
}

function resultText(r) {
  return { 10: '待盘', 20: '正常', 30: '丢失', 40: '损坏', 50: '报废' }[r] || `未知(${r})`
}

function resultTagType(r) {
  return { 10: 'info', 20: 'success', 30: 'danger', 40: 'warning', 50: 'danger' }[r] || 'info'
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
    params.append('page', query.page)
    params.append('page_size', query.page_size)
    const res = await api(`/api/v1/stocktakes?${params.toString()}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载盘点任务失败')
  } finally {
    loading.value = false
  }
}

function resetQuery() {
  query.company_id = ''
  query.status = ''
  query.page = 1
  fetchList()
}

function onCompanyChange() {
  form.asset_ids = []
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
  Object.assign(form, { company_id: '', name: '', scopeMode: 'all', asset_ids: [], remark: '' })
  assetOptions.value = []
  showCreateDialog.value = true
}

async function submitCreate() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    if (form.scopeMode === 'manual' && form.asset_ids.length === 0) {
      ElMessage.warning('手动圈定至少选择一项资产')
      return
    }
    submitting.value = true
    try {
      const body = {
        company_id: form.company_id,
        name: form.name,
        remark: form.remark,
      }
      if (form.scopeMode === 'manual') body.asset_ids = form.asset_ids
      const res = await api('/api/v1/stocktakes', { method: 'POST', body: JSON.stringify(body) })
      ElMessage.success(`盘点任务已创建，圈定 ${res.data?.item_total || 0} 项明细`)
      showCreateDialog.value = false
      fetchList()
      if (res.data?.id) openDetailById(res.data.company_id, res.data.id)
    } catch (err) {
      ElMessage.error(err.message || '创建失败')
    } finally {
      submitting.value = false
    }
  })
}

async function confirmStart(row) {
  try {
    await ElMessageBox.confirm(
      `开始盘点任务「${row.name}」？将生成一次性盘点码，范围快照即刻固定，明细开始接受扫码核对。`,
      '开始盘点',
      { type: 'warning', confirmButtonText: '开始', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    const res = await api(`/api/v1/stocktakes/${row.id}/start`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id }),
    })
    ElMessage.success('盘点已开始，请把盘点码发给盘点人')
    fetchList()
    showScanToken(res.data?.scan_token)
  } catch (err) {
    ElMessage.error(err.message || '开始失败')
  }
}

async function confirmFinish(row) {
  const unchecked = row.item_total - row.item_checked
  try {
    await ElMessageBox.confirm(
      unchecked > 0
        ? `还有 ${unchecked} 项未核对（将计入漏盘清单）。确认结束盘点任务「${row.name}」？结束后盘点码立即失效。`
        : `确认结束盘点任务「${row.name}」？结束后盘点码立即失效。`,
      '结束盘点',
      { type: 'warning', confirmButtonText: '结束', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await api(`/api/v1/stocktakes/${row.id}/finish`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id }),
    })
    ElMessage.success('盘点已结束')
    fetchList()
    if (detail.value?.id === row.id) openDetailById(row.company_id, row.id)
  } catch (err) {
    ElMessage.error(err.message || '结束失败')
  }
}

async function confirmCancel(row) {
  try {
    await ElMessageBox.confirm(
      `取消盘点任务「${row.name}」？已核对的明细将保留作参考，盘点码立即失效。`,
      '取消任务',
      { type: 'warning', confirmButtonText: '确认取消', cancelButtonText: '返回' }
    )
  } catch {
    return
  }
  try {
    await api(`/api/v1/stocktakes/${row.id}/cancel`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id }),
    })
    ElMessage.success('任务已取消')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '取消失败')
  }
}

async function openTokenDialog(row) {
  // 明文令牌只在生成时一次性返回（库里只存 SHA-256，无法找回），
  // 因此"查看盘点码"只能重新生成：先向管理员确认旧码即刻作废
  try {
    await ElMessageBox.confirm(
      '盘点码明文仅在生成时一次性显示（系统只存哈希，无法找回）。重新生成新盘点码？旧盘点码将立即失效。',
      '查看盘点码',
      { type: 'warning', confirmButtonText: '生成新码', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  rotating.value = true
  try {
    currentTokenTask = row
    const res = await api(`/api/v1/stocktakes/${row.id}/rotate-token`, {
      method: 'POST',
      body: JSON.stringify({ company_id: row.company_id }),
    })
    ElMessage.success('已生成新盘点码，旧码已失效')
    showScanToken(res.data?.scan_token)
  } catch (err) {
    ElMessage.error(err.message || '获取盘点码失败')
  } finally {
    rotating.value = false
  }
}

async function rotateToken() {
  if (!detail.value && !currentTokenTask) return
  rotating.value = true
  try {
    const task = currentTokenTask
    const res = await api(`/api/v1/stocktakes/${task.id}/rotate-token`, {
      method: 'POST',
      body: JSON.stringify({ company_id: task.company_id }),
    })
    ElMessage.success('已重新生成，旧盘点码已失效')
    showScanToken(res.data?.scan_token)
  } catch (err) {
    ElMessage.error(err.message || '重新生成失败')
  } finally {
    rotating.value = false
  }
}

let currentTokenTask = null

function showScanToken(token) {
  scanToken.value = token || ''
  tokenQR.value = ''
  if (token) {
    QRCode.toDataURL(`${window.location.origin}/#/m/t/${token}`, { width: 220, margin: 1 })
      .then((url) => { tokenQR.value = url })
      .catch(() => {})
  }
  showTokenDialog.value = true
}

function copyScanURL() {
  if (!scanURL.value) return
  navigator.clipboard?.writeText(scanURL.value).then(
    () => ElMessage.success('链接已复制'),
    () => ElMessage.warning('复制失败，请手动复制')
  )
}

async function openDetail(row) {
  await openDetailById(row.company_id, row.id)
}

async function openDetailById(companyId, id) {
  itemQuery.result = ''
  itemQuery.keyword = ''
  itemQuery.page = 1
  try {
    const res = await api(`/api/v1/stocktakes/${id}?company_id=${companyId}`)
    detail.value = res.data
    showDetailDrawer.value = true
    fetchItems()
  } catch (err) {
    ElMessage.error(err.message || '加载盘点详情失败')
  }
}

async function fetchItems() {
  if (!detail.value) return
  itemLoading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', detail.value.company_id)
    if (itemQuery.result) params.append('result', itemQuery.result)
    if (itemQuery.keyword) params.append('keyword', itemQuery.keyword)
    params.append('page', itemQuery.page)
    params.append('page_size', itemQuery.page_size)
    const res = await api(`/api/v1/stocktakes/${detail.value.id}/items?${params.toString()}`)
    itemRows.value = res.data?.items || []
    itemTotal.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载明细失败')
  } finally {
    itemLoading.value = false
  }
}

function openCheckDialog(row) {
  checkTarget.value = row
  Object.assign(checkForm, {
    result: row.result === 10 ? 20 : row.result,
    actual_location: row.actual_location || '',
    remark: row.remark || '',
    scanned_by: '',
  })
  showCheckDialog.value = true
}

async function submitCheck() {
  if (!checkTarget.value || !detail.value) return
  checking.value = true
  try {
    await api(`/api/v1/stocktakes/${detail.value.id}/items`, {
      method: 'POST',
      body: JSON.stringify({
        company_id: detail.value.company_id,
        scanned_by: checkForm.scanned_by,
        checks: [{
          item_id: checkTarget.value.id,
          result: checkForm.result,
          actual_location: checkForm.actual_location,
          remark: checkForm.remark,
        }],
      }),
    })
    ElMessage.success('核对结果已更新')
    showCheckDialog.value = false
    fetchItems()
    openDetailById(detail.value.company_id, detail.value.id)
  } catch (err) {
    ElMessage.error(err.message || '提交失败')
  } finally {
    checking.value = false
  }
}

// 标签 PDF：响应为二进制，绕开 JSON 封装的 api() 直接取 blob
async function printLabels(row) {
  try {
    const resp = await fetch('/api/v1/stocktakes/labels', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}` },
      body: JSON.stringify({
        company_id: row.company_id,
        stocktake_id: row.id,
        base_url: window.location.origin,
      }),
    })
    if (!resp.ok) {
      let msg = '生成标签失败'
      try {
        const data = await resp.json()
        if (data.message) msg = data.message
      } catch { /* 二进制响应或非 JSON 错误体 */ }
      throw new Error(msg)
    }
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `盘点标签-${row.name || row.id}.pdf`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('标签 PDF 已生成，请注意查认 CJK 字段不出现在标签上，扫码可见中文详情')
  } catch (err) {
    ElMessage.error(err.message || '生成标签失败')
  }
}

onMounted(() => {
  fetchCompanies()
  fetchList()
})
</script>

<style scoped>
.stocktakes-view {
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
.pagination-area {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
.token-body {
  text-align: center;
}
.qr-box img {
  width: 220px;
  height: 220px;
}
.token-text {
  font-family: monospace;
  font-size: 18px;
  font-weight: 700;
  color: #1f2937;
  margin: 12px 0 8px;
  word-break: break-all;
}
.token-hint {
  font-size: 12px;
  color: #9ca3af;
  margin-bottom: 8px;
}
.token-link {
  font-size: 12px;
  color: #2563eb;
  word-break: break-all;
  margin-bottom: 12px;
}
.token-actions {
  display: flex;
  justify-content: center;
  gap: 8px;
}
.detail-body {
  padding: 0 8px;
}
.detail-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 14px;
  margin-bottom: 14px;
  font-size: 13px;
  color: #4b5563;
}
.meta-item b {
  font-size: 15px;
}
.meta-item.ok b { color: #16a34a; }
.meta-item.warn b { color: #d97706; }
.meta-item.lost b { color: #dc2626; }
.meta-item.damaged b { color: #ea580c; }
.meta-item.scrapped b { color: #991b1b; }
.item-filter {
  margin-bottom: 4px;
}
</style>
