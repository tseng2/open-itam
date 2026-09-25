<template>
  <div class="reports-view">
    <div class="page-header">
      <div class="title-area">
        <h2>报表中心</h2>
        <span class="subtitle">管理视角聚合报表：汇总 8 卡 / 年度资产价值 / 月度建账趋势 / 资产分布（admin 专用，口径见 architecture.md）</span>
      </div>
      <div class="actions">
        <el-select v-model="companyId" placeholder="选择公司" filterable style="width: 200px" @change="reloadAll">
          <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
        </el-select>
        <el-button :loading="loading" @click="reloadAll">
          <el-icon><Refresh /></el-icon> 刷新
        </el-button>
      </div>
    </div>

    <!-- 汇总 8 卡 -->
    <el-row :gutter="12" class="summary-row">
      <el-col :span="6" v-for="card in summaryCards" :key="card.key">
        <el-card shadow="hover" class="mini-card">
          <div class="mini-label">{{ card.label }}</div>
          <div class="mini-value" :class="card.cls">{{ card.prefix || '' }}{{ card.value }}</div>
          <div v-if="card.hint" class="mini-hint">{{ card.hint }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="16" class="chart-row">
      <!-- 年度资产价值 -->
      <el-col :span="12">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="card-header">
              <span class="title">年度资产价值（近 {{ annualYears }} 年购入 · 在册口径）</span>
              <el-select v-model="annualYears" size="small" style="width: 110px" @change="fetchAnnual">
                <el-option :value="3" label="近 3 年" />
                <el-option :value="6" label="近 6 年" />
                <el-option :value="10" label="近 10 年" />
              </el-select>
            </div>
          </template>
          <div v-for="row in annualRows" :key="row.year" class="annual-row">
            <div class="annual-year">{{ row.year }}</div>
            <div class="annual-bars">
              <div class="bar-track">
                <div class="bar-fill original" :style="{ width: pct(row.original, annualMaxOriginal) + '%' }"></div>
              </div>
              <div class="bar-track">
                <div class="bar-fill net" :style="{ width: pct(row.net, annualMaxOriginal) + '%' }"></div>
              </div>
            </div>
            <div class="annual-nums">
              <div>{{ fmtMoney(row.original) }} <span class="sub-text">原值</span></div>
              <div class="sub-text">净值 {{ fmtMoney(row.net) }} · {{ row.count }} 台</div>
            </div>
          </div>
          <el-empty v-if="!annualRows.length" description="暂无购入数据（需台账维护购入时间）" :image-size="60" />
        </el-card>
      </el-col>

      <!-- 月度建账趋势 -->
      <el-col :span="12">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="card-header">
              <span class="title">月度建账趋势（近 {{ trendMonths }} 月）</span>
              <el-select v-model="trendMonths" size="small" style="width: 110px" @change="fetchTrend">
                <el-option :value="6" label="近 6 月" />
                <el-option :value="12" label="近 12 月" />
                <el-option :value="24" label="近 24 月" />
              </el-select>
            </div>
          </template>
          <div class="trend-chart">
            <div v-for="row in trendRows" :key="row.month" class="trend-col" :title="`${row.month}：${row.count} 台`">
              <div class="trend-count">{{ row.count || '' }}</div>
              <div class="trend-bar" :style="{ height: trendBarHeight(row.count) }"></div>
              <div class="trend-label">{{ row.month.slice(2) }}</div>
            </div>
          </div>
          <el-empty v-if="!trendRows.length" description="暂无台账数据" :image-size="60" />
        </el-card>
      </el-col>
    </el-row>

    <!-- 分布环图 -->
    <el-row :gutter="16" class="chart-row">
      <el-col :span="12">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="card-header">
              <span class="title">资产分布</span>
              <el-radio-group v-model="dimension" size="small" @change="fetchDistribution">
                <el-radio-button value="status">按状态</el-radio-button>
                <el-radio-button value="category">按类别</el-radio-button>
              </el-radio-group>
            </div>
          </template>
          <div class="ring-area">
            <svg viewBox="0 0 120 120" class="ring-svg">
              <circle v-for="(seg, i) in ringSegments" :key="i" cx="60" cy="60" r="40"
                fill="none" stroke-width="18" :stroke="seg.color"
                :stroke-dasharray="seg.dasharray" :stroke-dashoffset="seg.dashoffset" />
              <text x="60" y="57" text-anchor="middle" class="ring-total">{{ ringTotal }}</text>
              <text x="60" y="72" text-anchor="middle" class="ring-caption">资产</text>
            </svg>
            <div class="ring-legend">
              <div v-for="(row, i) in distributionRows_" :key="row.label" class="legend-item">
                <span class="legend-dot" :style="{ background: palette[i % palette.length] }"></span>
                <span class="legend-label">{{ row.label }}</span>
                <span class="legend-num">{{ row.count }} 台 · {{ row.percent }}%</span>
              </div>
              <el-empty v-if="!distributionRows_.length" description="暂无数据" :image-size="60" />
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'

// 报表中心（P2）：四端点全部 admin（服务端 RBAC），一次并发拉齐
const companies = ref([])
const companyId = ref('')
const loading = ref(false)

const summary = ref({})
const annualRows = ref([])
const annualYears = ref(6)
const trendRows = ref([])
const trendMonths = ref(12)
const dimension = ref('status')
const distribution = ref([])

const palette = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#64748b', '#06b6d4', '#ec4899']

const summaryCards = computed(() => [
  { key: 'total_assets', label: '资产总数', value: summary.value.total_assets ?? '—', hint: '含报废（台账全量）' },
  { key: 'in_use', label: '使用中', value: summary.value.in_use ?? '—' },
  { key: 'stock', label: '库存中', value: summary.value.stock ?? '—' },
  { key: 'repair', label: '维修中', value: summary.value.repair ?? '—' },
  { key: 'scrapped', label: '已报废', value: summary.value.scrapped ?? '—', cls: 'text-muted' },
  { key: 'off_book', label: '列管资产', value: summary.value.off_book ?? '—', hint: '财务销账继续跟踪' },
  { key: 'total_value', label: '在册原值合计', value: fmtMoney(summary.value.total_value ?? 0), prefix: '￥', hint: '非报废口径' },
  { key: 'net_value', label: '在册净值合计', value: fmtMoney(summary.value.net_value ?? 0), prefix: '￥', hint: '折旧引擎每小时刷' },
])

const annualMaxOriginal = computed(() =>
  Math.max(1, ...annualRows.value.map(r => r.original)),
)

// 分布行去重命名（与响应字段 rows 对齐）
const distributionRows_ = computed(() => distribution.value)

// SVG 环图分段：周长按占比切分，dashoffset 累计偏移
const ringSegments = computed(() => {
  const rows = distribution.value
  const total = rows.reduce((s, r) => s + r.count, 0)
  if (!total) return []
  const C = 2 * Math.PI * 40
  let acc = 0
  return rows.map((r, i) => {
    const len = (r.count / total) * C
    const seg = {
      color: palette[i % palette.length],
      dasharray: `${len} ${C - len}`,
      dashoffset: -acc,
    }
    acc += len
    return seg
  })
})
const ringTotal = computed(() => distribution.value.reduce((s, r) => s + r.count, 0))

function fmtMoney(v) {
  if (!v) return '0'
  if (v >= 1e8) return (v / 1e8).toFixed(2) + ' 亿'
  if (v >= 1e4) return (v / 1e4).toFixed(2) + ' 万'
  return Number(v).toFixed(2)
}
function pct(v, max) {
  return Math.max(0, Math.round((v / max) * 100))
}
function trendBarHeight(count) {
  const max = Math.max(1, ...trendRows.value.map(r => r.count))
  return Math.max(2, Math.round((count / max) * 120)) + 'px'
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
    // 报表是公司维度面：缺省选第一家公司，避免空白报表
    if (!companyId.value && companies.value.length) {
      companyId.value = companies.value[0].id
    }
  } catch { /* 下拉失败不阻塞 */ }
}

async function fetchSummary() {
  const res = await api(`/api/v1/reports/summary?company_id=${companyId.value}`)
  summary.value = res.data || {}
}
async function fetchAnnual() {
  const res = await api(`/api/v1/reports/annual-value?company_id=${companyId.value}&years=${annualYears.value}`)
  annualRows.value = res.data?.rows || []
}
async function fetchTrend() {
  const res = await api(`/api/v1/reports/monthly-trend?company_id=${companyId.value}&months=${trendMonths.value}`)
  trendRows.value = res.data?.rows || []
}
async function fetchDistribution() {
  const res = await api(`/api/v1/reports/distribution?company_id=${companyId.value}&dimension=${dimension.value}`)
  distribution.value = res.data?.rows || []
}

async function reloadAll() {
  if (!companyId.value) return
  loading.value = true
  try {
    await Promise.all([fetchSummary(), fetchAnnual(), fetchTrend(), fetchDistribution()])
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await fetchCompanies()
  reloadAll()
})
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.title-area h2 { margin: 0; font-size: 20px; color: #1f2937; }
.subtitle { font-size: 12px; color: #94a3b8; }
.actions { display: flex; gap: 12px; align-items: center; }
.summary-row { margin-bottom: 16px; }
.summary-row .el-col { margin-bottom: 12px; }
.mini-card { border-radius: 10px; }
.mini-label { font-size: 12px; color: #64748b; margin-bottom: 6px; }
.mini-value { font-size: 22px; font-weight: 700; color: #1e293b; }
.mini-hint { font-size: 11px; color: #94a3b8; margin-top: 4px; }
.text-muted { color: #94a3b8; }
.chart-row { margin-bottom: 16px; }
.chart-card { border-radius: 10px; min-height: 320px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.card-header .title { font-weight: 600; color: #1f2937; }
.sub-text { color: #94a3b8; font-size: 12px; }

/* 年度价值行 */
.annual-row { display: flex; align-items: center; gap: 12px; padding: 10px 4px; border-bottom: 1px dashed #e2e8f0; }
.annual-row:last-child { border-bottom: none; }
.annual-year { width: 48px; font-weight: 600; color: #334155; font-size: 13px; }
.annual-bars { flex: 1; display: flex; flex-direction: column; gap: 4px; }
.bar-track { height: 10px; background: #f1f5f9; border-radius: 5px; overflow: hidden; }
.bar-fill { height: 100%; border-radius: 5px; min-width: 0; transition: width 0.4s ease; }
.bar-fill.original { background: linear-gradient(90deg, #1d4ed8, #3b82f6); }
.bar-fill.net { background: linear-gradient(90deg, #047857, #10b981); }
.annual-nums { width: 170px; font-size: 12px; color: #334155; text-align: right; }

/* 月度趋势柱状 */
.trend-chart { display: flex; align-items: flex-end; gap: 6px; height: 200px; padding: 8px 4px; }
.trend-col { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: flex-end; height: 100%; }
.trend-count { font-size: 11px; color: #64748b; margin-bottom: 2px; min-height: 14px; }
.trend-bar { width: 60%; background: linear-gradient(180deg, #60a5fa, #2563eb); border-radius: 4px 4px 0 0; min-height: 2px; }
.trend-label { font-size: 10px; color: #94a3b8; margin-top: 6px; white-space: nowrap; }

/* 分布环图 */
.ring-area { display: flex; gap: 24px; align-items: center; padding: 8px; }
.ring-svg { width: 200px; height: 200px; flex-shrink: 0; }
.ring-total { font-size: 20px; font-weight: 700; fill: #1e293b; }
.ring-caption { font-size: 10px; fill: #94a3b8; }
.ring-legend { flex: 1; display: flex; flex-direction: column; gap: 10px; }
.legend-item { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.legend-dot { width: 10px; height: 10px; border-radius: 3px; flex-shrink: 0; }
.legend-label { color: #334155; min-width: 72px; }
.legend-num { color: #64748b; font-size: 12px; }
</style>
