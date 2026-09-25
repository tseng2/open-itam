<template>
  <div class="consumables-view">
    <div class="page-header">
      <div class="title-area">
        <h2>耗材管理</h2>
        <span class="subtitle">库存物料台账 + 追加式出入库流水：库存只经入库/出库/调整变更（每笔都有流水），触达预警线自动标记</span>
      </div>
      <div class="actions">
        <el-button v-if="isAdmin" type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 新增耗材
        </el-button>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="companyId" placeholder="选择公司" clearable filterable style="width: 200px" @change="fetchList">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="关键字">
          <el-input v-model="keyword" placeholder="名称 / 规格" clearable style="width: 180px" @keyup.enter="fetchList" @clear="fetchList" />
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="lowStockOnly" @change="fetchList">仅看库存预警</el-checkbox>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column label="耗材" min-width="200">
          <template #default="{ row }">
            <strong>{{ row.name }}</strong>
            <div class="sub-text">{{ row.spec || '规格未登记' }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="unit" label="单位" width="80" />
        <el-table-column label="当前库存" width="150">
          <template #default="{ row }">
            <span :class="{ 'stock-warn': row.low_stock }">{{ row.stock }}</span>
            <el-tag v-if="row.low_stock" type="danger" size="small" class="low-tag">库存预警</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="预警线" width="90">
          <template #default="{ row }">{{ row.min_quantity > 0 ? row.min_quantity : '—' }}</template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
        <el-table-column v-if="isAdmin" label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openStockDialog(row, 'stock_in')">入库</el-button>
            <el-button link type="success" size="small" @click="openStockDialog(row, 'stock_out')">出库</el-button>
            <el-button link type="warning" size="small" @click="openStockDialog(row, 'adjust')">调整</el-button>
            <el-button link size="small" @click="openTxnDrawer(row)">流水</el-button>
            <el-button link type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="confirmDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无耗材，右上角新增第一项" />
        </template>
      </el-table>

      <div class="pagination-area">
        <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total"
          :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
          @size-change="fetchList" @current-change="fetchList" />
      </div>
    </el-card>

    <!-- 新增 / 编辑耗材（库存不入表单：只经流水变更）-->
    <el-dialog v-model="showDialog" :title="editMode ? '编辑耗材' : '新增耗材'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="110px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如：A4 打印纸" />
        </el-form-item>
        <el-form-item label="规格型号">
          <el-input v-model="form.spec" placeholder="如：70g/500张/包" />
        </el-form-item>
        <el-form-item label="计量单位">
          <el-input v-model="form.unit" placeholder="包 / 盒 / 个" style="width: 200px" />
        </el-form-item>
        <el-form-item label="最低库存预警">
          <el-input-number v-model="form.min_quantity" :min="0" style="width: 200px" />
          <div class="form-tip">0 = 不预警；库存 ≤ 预警线时列表自动标记</div>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <div v-if="!editMode" class="dialog-tip">建账后库存为 0，请通过「入库」登记期初库存</div>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ editMode ? '保存' : '新增' }}</el-button>
      </template>
    </el-dialog>

    <!-- 出入库 / 调整对话框 -->
    <el-dialog v-model="showStockDialog" :title="stockDialogTitle" width="460px" destroy-on-close>
      <el-form label-width="90px">
        <el-form-item :label="stockAction === 'adjust' ? '调整增量' : '数量'">
          <el-input-number
            v-model="stockForm.value"
            :min="stockAction === 'adjust' ? -999999 : 1"
            :max="999999"
            style="width: 200px"
          />
          <div class="form-tip">{{ stockFormTip }}</div>
        </el-form-item>
        <el-form-item v-if="stockAction === 'stock_out'" label="领用人">
          <el-input v-model="stockForm.recipient" placeholder="发放给谁（可留空）" style="width: 100%" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="stockForm.remark" placeholder="如：季度采购 / 工位领用 / 盘点盘亏" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showStockDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitStock">确认</el-button>
      </template>
    </el-dialog>

    <!-- 出入库流水抽屉 -->
    <el-drawer v-model="showTxnDrawer" :title="`出入库流水：${txnConsumable?.name || ''}`" size="720px">
      <el-table :data="txns" v-loading="txnLoading" stripe>
        <el-table-column label="时间" width="160">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag :type="txnTag(row.type)" size="small">{{ txnText(row.type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="变动" width="90" align="center">
          <template #default="{ row }">
            <span :class="row.delta > 0 ? 'delta-in' : 'delta-out'">{{ row.delta > 0 ? '+' : '' }}{{ row.delta }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="recipient" label="领用人" width="100" />
        <el-table-column prop="operator_name" label="操作人" width="100" />
        <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip />
        <template #empty>
          <el-empty description="暂无流水" />
        </template>
      </el-table>
      <div class="pagination-area">
        <el-pagination v-model:current-page="txnPage" :page-size="20" :total="txnTotal"
          layout="total, prev, pager, next" @current-change="fetchTxns" />
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

// 耗材管理（P2）：库存只经 stock-in/stock-out/adjust 三端点变更，
// 服务端原子扣减（出库不得击穿零库存）；操作自动进审计日志
const companies = ref([])
const companyId = ref('')
const keyword = ref('')
const lowStockOnly = ref(false)
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const submitting = ref(false)

const showDialog = ref(false)
const editMode = ref(false)
const formRef = ref(null)

const showStockDialog = ref(false)
const stockAction = ref('stock_in')
const stockConsumable = ref(null)
const stockForm = reactive({ value: 1, recipient: '', remark: '' })

const showTxnDrawer = ref(false)
const txnConsumable = ref(null)
const txns = ref([])
const txnTotal = ref(0)
const txnPage = ref(1)
const txnLoading = ref(false)

const isAdmin = computed(() => {
  try {
    const u = JSON.parse(localStorage.getItem('itagent_user') || 'null')
    return !!u && (u.role === 'admin' || u.role === 'super_admin')
  } catch {
    return false
  }
})

const emptyForm = () => ({
  id: 0, company_id: '', name: '', spec: '', unit: '', min_quantity: 0, remark: '',
})
const form = reactive(emptyForm())

const formRules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  name: [{ required: true, message: '请输入耗材名称', trigger: 'blur' }],
}

const stockDialogTitle = computed(() =>
  ({ stock_in: '耗材入库', stock_out: '耗材出库（领用）', adjust: '库存调整' }[stockAction.value] || ''),
)
const stockFormTip = computed(() => {
  if (stockAction.value === 'stock_out') return '出库数量不得超过当前库存（服务端 409 拦截）'
  if (stockAction.value === 'adjust') return '带符号：盘亏填负数、盘盈填正数，不能为 0'
  return '入库数量必须为正数'
})

function txnText(t) {
  return { stock_in: '入库', stock_out: '出库', adjust: '调整' }[t] || t
}
function txnTag(t) {
  return { stock_in: 'success', stock_out: 'warning', adjust: 'info' }[t] || 'info'
}
function fmtTime(ts) {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN', { hour12: false })
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
    if (lowStockOnly.value) params.append('low_stock', 'true')
    params.append('page', page.value)
    params.append('page_size', pageSize.value)
    const res = await api(`/api/v1/consumables?${params}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载耗材失败')
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
    id: row.id, company_id: row.company_id, name: row.name,
    spec: row.spec || '', unit: row.unit || '',
    min_quantity: row.min_quantity || 0, remark: row.remark || '',
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
      if (editMode.value) {
        await api(`/api/v1/consumables/${form.id}`, { method: 'PUT', body: JSON.stringify({ ...form }) })
        ElMessage.success('耗材已更新（库存不经编辑面变更）')
      } else {
        await api('/api/v1/consumables', { method: 'POST', body: JSON.stringify({ ...form }) })
        ElMessage.success('耗材已新增，请入库期初库存')
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

function openStockDialog(row, action) {
  stockAction.value = action
  stockConsumable.value = row
  stockForm.value = action === 'adjust' ? 0 : 1
  stockForm.recipient = ''
  stockForm.remark = ''
  showStockDialog.value = true
}

async function submitStock() {
  if (!stockConsumable.value) return
  const c = stockConsumable.value
  submitting.value = true
  try {
    let path, body
    if (stockAction.value === 'stock_in') {
      path = `/api/v1/consumables/${c.id}/stock-in`
      body = { company_id: c.company_id, quantity: stockForm.value, remark: stockForm.remark }
    } else if (stockAction.value === 'stock_out') {
      path = `/api/v1/consumables/${c.id}/stock-out`
      body = { company_id: c.company_id, quantity: stockForm.value, recipient: stockForm.recipient, remark: stockForm.remark }
    } else {
      path = `/api/v1/consumables/${c.id}/adjust`
      body = { company_id: c.company_id, delta: stockForm.value, remark: stockForm.remark }
    }
    await api(path, { method: 'POST', body: JSON.stringify(body) })
    ElMessage.success('已登记流水，库存同步更新')
    showStockDialog.value = false
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '登记失败')
  } finally {
    submitting.value = false
  }
}

function openTxnDrawer(row) {
  txnConsumable.value = row
  txnPage.value = 1
  showTxnDrawer.value = true
  fetchTxns()
}

async function fetchTxns() {
  const c = txnConsumable.value
  if (!c) return
  txnLoading.value = true
  try {
    const res = await api(`/api/v1/consumables/${c.id}/txns?company_id=${c.company_id}&page=${txnPage.value}&page_size=20`)
    txns.value = res.data?.items || []
    txnTotal.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载流水失败')
  } finally {
    txnLoading.value = false
  }
}

async function confirmDelete(row) {
  try {
    await ElMessageBox.confirm(
      `确定删除「${row.name}」？已有出入库流水的耗材会被服务端拦截`,
      '删除耗材', { type: 'warning' },
    )
  } catch { return }
  try {
    await api(`/api/v1/consumables/${row.id}?company_id=${row.company_id}`, { method: 'DELETE' })
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
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.title-area h2 { margin: 0; font-size: 20px; color: #1f2937; }
.subtitle { font-size: 12px; color: #94a3b8; }
.filter-card { margin-bottom: 16px; }
.filter-form { margin-bottom: -18px; }
.table-card { border-radius: 10px; }
.pagination-area { display: flex; justify-content: flex-end; margin-top: 16px; }
.sub-text { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.stock-warn { color: #dc2626; font-weight: 700; }
.low-tag { margin-left: 8px; }
.delta-in { color: #059669; font-weight: 600; }
.delta-out { color: #dc2626; font-weight: 600; }
.form-tip { font-size: 12px; color: #94a3b8; line-height: 1.4; margin-top: 4px; }
.dialog-tip { font-size: 12px; color: #d97706; background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; padding: 8px 12px; margin-top: 8px; }
</style>
