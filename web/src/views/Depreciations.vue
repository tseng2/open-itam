<template>
  <div class="depreciations-view">
    <div class="page-header">
      <div class="title-area">
        <h2>折旧规则引擎</h2>
        <span class="subtitle">分阶段阶梯折旧 + 残值下限兜底：资产挂接规则后，引擎按购入时间定时刷净值；折旧完且财务销账的资产可在台账中转「列管」继续跟踪</span>
      </div>
      <div class="actions">
        <el-button :loading="recalculating" @click="recalculate">
          <el-icon><Refresh /></el-icon> 立即重算净值
        </el-button>
        <el-button type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 新建规则
        </el-button>
      </div>
    </div>

    <!-- 筛选面板 -->
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="query" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="query.company_id" placeholder="选择公司" clearable style="width: 200px" @change="fetchList">
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 规则表格 -->
    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="name" label="规则名称" min-width="180">
          <template #default="{ row }">
            <strong>{{ row.name }}</strong>
            <div class="sub-text">{{ row.remark || '-' }}</div>
          </template>
        </el-table-column>
        <el-table-column label="总折旧期" width="110">
          <template #default="{ row }">{{ monthsText(row.months) }}</template>
        </el-table-column>
        <el-table-column label="残值下限" width="130">
          <template #default="{ row }">{{ floorText(row) }}</template>
        </el-table-column>
        <el-table-column label="折旧阶梯" min-width="240">
          <template #default="{ row }">
            <template v-if="parseStageSummaries(row.stages).length">
              <el-tag
                v-for="(s, i) in parseStageSummaries(row.stages)"
                :key="i"
                size="small"
                effect="plain"
                style="margin-right: 6px"
              >{{ s }}</el-tag>
            </template>
            <span v-else class="empty-cell">直线折旧（按总月数均摊）</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="confirmDelete(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="emptyDescription" />
        </template>
      </el-table>

      <div class="pagination-area">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next"
          @size-change="fetchList"
          @current-change="fetchList"
        />
      </div>
    </el-card>

    <!-- 规则编辑对话框 -->
    <el-dialog v-model="showDialog" :title="editMode ? '编辑折旧规则' : '新建折旧规则'" width="720px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="130px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%">
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="规则名称" prop="name">
          <el-input v-model="form.name" placeholder="如：IT 设备 3 年加速折旧" />
        </el-form-item>
        <el-form-item label="总折旧月数" prop="months">
          <el-input-number v-model="form.months" :min="1" :step="6" style="width: 200px" />
          <span class="form-tip">配置阶梯时必须与阶梯覆盖月数一致；不配阶梯则按此月数直线折旧</span>
        </el-form-item>
        <el-form-item label="残值类型">
          <el-radio-group v-model="form.floor_type">
            <el-radio-button value="percent">残值率（%）</el-radio-button>
            <el-radio-button value="amount">固定金额（元）</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="form.floor_type === 'percent' ? '残值率' : '残值金额'">
          <el-input-number
            v-model="form.floor_val"
            :min="0"
            :max="form.floor_type === 'percent' ? 100 : 9999999"
            :precision="form.floor_type === 'percent' ? 1 : 2"
            :step="form.floor_type === 'percent' ? 5 : 100"
            style="width: 200px"
          />
          <span class="form-tip">{{ form.floor_type === 'percent' ? '如 5 表示净值最低保留原值 5%' : '净值最低保留的绝对金额' }}</span>
        </el-form-item>

        <el-divider content-position="left">折旧阶梯（可留空走直线折旧）</el-divider>
        <div v-for="(stage, idx) in form.stages" :key="idx" class="stage-row">
          <span class="stage-label">第 {{ idx + 1 }} 段</span>
          <el-input-number v-model="stage.period" :min="1" :step="1" style="width: 120px" />
          <el-select v-model="stage.unit" style="width: 90px">
            <el-option label="月" value="MONTH" />
            <el-option label="年" value="YEAR" />
          </el-select>
          <span class="stage-label">折旧</span>
          <el-input-number v-model="stage.ratio_pct" :min="0" :max="100" :precision="1" :step="5" style="width: 120px" />
          <span class="stage-label">%</span>
          <el-button link type="danger" size="small" @click="removeStage(idx)">删除</el-button>
        </div>
        <div class="stage-summary" v-if="form.stages.length">
          阶梯覆盖 {{ stageMonthsSum() }} 个月（需等于总折旧月数 {{ form.months }}），
          累计折旧 {{ stageRatioSum() }}%
        </div>
        <div class="stage-summary error" v-else>
          未配置阶梯：按总月数直线折旧至残值
        </div>
        <el-button size="small" style="margin-top: 8px" @click="addStage">+ 添加阶梯段</el-button>

        <el-form-item label="启用" style="margin-top: 16px">
          <el-switch v-model="form.enabled" />
          <span class="form-tip">停用后引擎冻结该规则名下资产的净值刷新（现值保持不动）</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" placeholder="折旧口径依据、财务制度说明等" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitRule">{{ editMode ? '保存规则' : '创建规则' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

const loading = ref(false)
const submitting = ref(false)
const recalculating = ref(false)
const showDialog = ref(false)
const editMode = ref(false)
const formRef = ref(null)

const companies = ref([])
const items = ref([])
const total = ref(0)

const query = reactive({
  company_id: '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  id: 0,
  company_id: '',
  name: '',
  months: 36,
  floor_type: 'percent',
  floor_val: 5,
  enabled: true,
  remark: '',
  stages: [],
})

const formRules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  name: [{ required: true, message: '请输入规则名称', trigger: 'blur' }],
  months: [{ required: true, message: '请填写总折旧月数', trigger: 'change' }],
}

const emptyDescription = computed(() =>
  query.company_id
    ? '暂无折旧规则，点击右上角「新建规则」配置折旧口径'
    : '请先选择所属公司查看折旧规则'
)

// ---- 展示辅助 ----
function monthsText(months) {
  const y = Math.floor(months / 12)
  const m = months % 12
  return y ? `${y} 年${m ? ' ' + m + ' 个月' : ''}（${months} 月）` : `${months} 个月`
}

function floorText(row) {
  if (row.floor_type === 'amount') return `≥ ￥${Number(row.floor_val).toFixed(2)}`
  return `原值的 ${Number(row.floor_val) * 100}%`
}

// stages JSON → 摘要标签数组；解析失败安静降级为空（服务端入口已校验过格式）
function parseStageSummaries(raw) {
  try {
    const stages = typeof raw === 'string' && raw.trim() ? JSON.parse(raw) : []
    if (!Array.isArray(stages)) return []
    return stages.map(s => {
      const unit = s.unit === 'YEAR' ? '年' : '月'
      return `${s.period} ${unit}内折旧 ${Math.round(s.ratio * 1000) / 10}%`
    })
  } catch {
    return []
  }
}

// ---- 阶梯编辑辅助 ----
function addStage() {
  form.stages.push({ period: 12, unit: 'MONTH', ratio_pct: 30 })
}

function removeStage(idx) {
  form.stages.splice(idx, 1)
}

function stageMonthsSum() {
  return form.stages.reduce((sum, s) => sum + (s.unit === 'YEAR' ? s.period * 12 : s.period), 0)
}

function stageRatioSum() {
  return Math.round(form.stages.reduce((sum, s) => sum + Number(s.ratio_pct || 0), 0) * 10) / 10
}

// 组装 stages JSON：percent 转小数；空数组 → 空串（直线折旧）
function buildStagesJSON() {
  const valid = form.stages.filter(s => s.period > 0 && s.ratio_pct > 0)
  if (!valid.length) return ''
  return JSON.stringify(valid.map(s => ({
    period: s.period,
    unit: s.unit,
    ratio: Number((s.ratio_pct / 100).toFixed(4)),
  })))
}

// 前置校验（权威校验在服务端，这里挡住明显错误并给出可读提示）
function validateStages() {
  if (!form.stages.length) return true
  const sum = stageMonthsSum()
  if (sum !== form.months) {
    ElMessage.error(`阶梯覆盖 ${sum} 个月与总折旧月数 ${form.months} 不一致`)
    return false
  }
  const ratio = stageRatioSum()
  if (ratio > 100.01) {
    ElMessage.error(`阶梯累计折旧 ${ratio}% 超过 100%`)
    return false
  }
  return true
}

// ---- 数据读写 ----
async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch (err) {
    console.error('failed to fetch companies', err)
  }
}

async function fetchList() {
  if (!query.company_id) {
    items.value = []
    total.value = 0
    return
  }
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', query.company_id)
    params.append('page', query.page)
    params.append('page_size', query.page_size)
    const res = await api(`/api/v1/depreciations?${params.toString()}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载折旧规则失败')
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  Object.assign(form, {
    id: 0,
    company_id: query.company_id || '',
    name: '',
    months: 36,
    floor_type: 'percent',
    floor_val: 5,
    enabled: true,
    remark: '',
    stages: [],
  })
  editMode.value = false
  showDialog.value = true
}

// 服务端 stages 是 JSON 字符串，编辑时还原为可编辑行（ratio → percent）
function openEditDialog(row) {
  let stages = []
  try {
    const parsed = row.stages && row.stages.trim() ? JSON.parse(row.stages) : []
    stages = (Array.isArray(parsed) ? parsed : []).map(s => ({
      period: s.period,
      unit: s.unit,
      ratio_pct: Math.round(s.ratio * 1000) / 10,
    }))
  } catch {
    stages = []
  }
  Object.assign(form, {
    id: row.id,
    company_id: row.company_id,
    name: row.name,
    months: row.months,
    floor_type: row.floor_type === 'amount' ? 'amount' : 'percent',
    floor_val: row.floor_type === 'amount' ? Number(row.floor_val) : Number(row.floor_val) * 100,
    enabled: !!row.enabled,
    remark: row.remark || '',
    stages,
  })
  editMode.value = true
  showDialog.value = true
}

async function submitRule() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    if (!validateStages()) return
    submitting.value = true
    try {
      const payload = {
        company_id: form.company_id,
        name: form.name,
        months: form.months,
        floor_type: form.floor_type,
        floor_val: form.floor_type === 'percent' ? Number((form.floor_val / 100).toFixed(4)) : form.floor_val,
        stages: buildStagesJSON(),
        enabled: form.enabled,
        remark: form.remark,
      }
      if (editMode.value) {
        await api(`/api/v1/depreciations/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
        ElMessage.success('规则已保存，可点击「立即重算净值」刷新挂接资产')
      } else {
        await api('/api/v1/depreciations', { method: 'POST', body: JSON.stringify(payload) })
        ElMessage.success('规则创建成功，在资产台账编辑中即可挂接')
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
      `删除折旧规则「${row.name}」？正被资产引用的规则无法删除。`,
      '删除确认',
      { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await api(`/api/v1/depreciations/${row.id}?company_id=${row.company_id}`, { method: 'DELETE' })
    ElMessage.success('规则已删除')
    fetchList()
  } catch (err) {
    // 服务端 409 携带引用计数，原样展示给管理员
    ElMessage.error(err.message || '删除失败')
  }
}

async function recalculate() {
  recalculating.value = true
  try {
    const res = await api('/api/v1/depreciations/recalculate', { method: 'POST', body: '{}' })
    const n = res.data?.updated ?? 0
    ElMessage.success(n > 0 ? `本轮重算刷新了 ${n} 个资产的净值` : '本轮无净值变化（已是最新的）')
  } catch (err) {
    ElMessage.error(err.message || '重算失败')
  } finally {
    recalculating.value = false
  }
}

onMounted(() => {
  fetchCompanies()
  fetchList()
})
</script>

<style scoped>
.depreciations-view {
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
.actions {
  display: flex;
  gap: 8px;
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
.sub-text {
  font-size: 12px;
  color: #9ca3af;
}
.empty-cell {
  color: #d1d5db;
}
.form-tip {
  margin-left: 10px;
  font-size: 12px;
  color: #9ca3af;
}
.stage-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  padding-left: 12px;
}
.stage-label {
  font-size: 13px;
  color: #4b5563;
}
.stage-summary {
  font-size: 12px;
  color: #6b7280;
  margin: 4px 0 8px 12px;
}
.stage-summary.error {
  color: #9ca3af;
}
.pagination-area {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
