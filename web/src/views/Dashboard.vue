<template>
  <div class="dashboard-view">
    <!-- 顶部核心指标看板 -->
    <el-row :gutter="16" class="metric-row">
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card blue">
          <div class="stat-header">
            <span class="stat-title">全集团资产总数</span>
            <el-icon class="stat-icon"><Monitor /></el-icon>
          </div>
          <div class="stat-value">1,248 <span class="unit">台</span></div>
          <div class="stat-footer">
            <span class="trend up">较上月 +32 台</span>
            <span class="sub">在线率 96.4%</span>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card green">
          <div class="stat-header">
            <span class="stat-title">使用中 (已领用)</span>
            <el-icon class="stat-icon"><User /></el-icon>
          </div>
          <div class="stat-value">1,120 <span class="unit">台</span></div>
          <div class="stat-footer">
            <span>在库闲置 98 台</span>
            <span>维修中 30 台</span>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card purple">
          <div class="stat-header">
            <span class="stat-title">用友 U8 采购追踪</span>
            <el-icon class="stat-icon"><ShoppingBag /></el-icon>
          </div>
          <div class="stat-value">¥ 482.5 <span class="unit">万</span></div>
          <div class="stat-footer">
            <span>关联 U8 订单 184 笔</span>
            <el-tag size="small" type="success" effect="plain">已全量对齐</el-tag>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card orange">
          <div class="stat-header">
            <span class="stat-title">待处理告警与变更</span>
            <el-icon class="stat-icon"><WarningFilled /></el-icon>
          </div>
          <div class="stat-value text-danger">3 <span class="unit">起</span></div>
          <div class="stat-footer">
            <span>SMART 硬盘告警 1 起</span>
            <span>内存变更 2 起</span>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 图表与大盘分析 -->
    <el-row :gutter="16" class="mt-4">
      <el-col :span="16">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="card-header">
              <span class="title">各子公司硬件资产分布状况</span>
              <el-button link size="small" @click="fetchCompanyDistribution">刷新</el-button>
            </div>
          </template>
          <div class="mock-chart-placeholder">
            <div class="bar-group" v-for="c in companyDistribution" :key="c.name">
              <div class="bar-label">{{ c.name }}</div>
              <div class="bar-track">
                <div class="bar-fill" :style="{ width: c.percent + '%' }"></div>
              </div>
              <div class="bar-val">{{ c.count }} 台 ({{ c.percent }}%)</div>
            </div>
            <div v-if="!companyDistribution.length" class="empty-dist">暂无公司或资产数据，先在「组织架构」维护公司并建账资产</div>
          </div>
        </el-card>
      </el-col>

      <el-col :span="8">
        <el-card shadow="never" class="chart-card">
          <template #header>
            <div class="card-header">
              <span class="title">实时终端与 Agent 状态</span>
              <el-tag size="small" type="success" effect="light">服务正常</el-tag>
            </div>
          </template>
          <div class="agent-summary">
            <div class="summary-item">
              <span class="label">今日活跃上报终端</span>
              <span class="num text-primary">1,086 台</span>
            </div>
            <div class="summary-item">
              <span class="label">超过 7 天未上报</span>
              <span class="num text-warning">24 台</span>
            </div>
            <div class="summary-item">
              <span class="label">AD 组织架构同步</span>
              <span class="num text-success">已同步 (今日 03:00)</span>
            </div>
            <div class="summary-item">
              <span class="label">最新 Agent 客户端</span>
              <span class="num">v0.2.5 (在线终端 94%)</span>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'

// 各子公司资产分布：真实数据（原 mock 写死「东莞/苏州」已废）——
// 拉公司列表后逐公司查资产 total（公司量级小，串行可接受），
// percent 按资产数占比取整。总览大盘其余卡片实装属独立任务
const companyDistribution = ref([])

async function fetchCompanyDistribution() {
  try {
    const res = await api('/api/v1/companies')
    const companies = res.data || []
    const rows = []
    for (const c of companies) {
      try {
        const a = await api(`/api/v1/assets?company_id=${c.id}&page=1&page_size=1`)
        rows.push({ name: c.name, count: a.data?.total || 0 })
      } catch { /* 单公司查询失败不阻塞整体 */ rows.push({ name: c.name, count: 0 }) }
    }
    const total = rows.reduce((s, r) => s + r.count, 0)
    companyDistribution.value = rows.map(r => ({
      ...r,
      percent: total > 0 ? Math.max(1, Math.round((r.count / total) * 100)) : 0,
    }))
  } catch { /* 公司列表失败保持空态 */ }
}

onMounted(() => {
  fetchCompanyDistribution()
})
</script>

<style scoped>
.dashboard-view {
  padding: 4px;
}
.empty-dist {
  padding: 24px 0;
  text-align: center;
  color: #94a3b8;
  font-size: 13px;
}
.stat-card {
  border-radius: 10px;
  border: none;
  color: #fff;
  transition: transform 0.2s ease;
}
.stat-card:hover {
  transform: translateY(-2px);
}
.stat-card.blue {
  background: linear-gradient(135deg, #1e3a8a 0%, #3b82f6 100%);
}
.stat-card.green {
  background: linear-gradient(135deg, #065f46 0%, #10b981 100%);
}
.stat-card.purple {
  background: linear-gradient(135deg, #4c1d95 0%, #8b5cf6 100%);
}
.stat-card.orange {
  background: linear-gradient(135deg, #9a3412 0%, #f97316 100%);
}
.stat-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  opacity: 0.9;
  font-size: 13px;
}
.stat-icon {
  font-size: 20px;
}
.stat-value {
  font-size: 28px;
  font-weight: 700;
  margin: 12px 0 8px 0;
}
.stat-value .unit {
  font-size: 14px;
  font-weight: normal;
  opacity: 0.85;
}
.stat-footer {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  opacity: 0.85;
  border-top: 1px solid rgba(255, 255, 255, 0.15);
  padding-top: 8px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.card-header .title {
  font-weight: 600;
  color: #1f2937;
}
.chart-card {
  border-radius: 10px;
  min-height: 380px;
}
.mt-4 {
  margin-top: 16px;
}
.mock-chart-placeholder {
  padding: 16px 8px;
}
.bar-group {
  margin-bottom: 20px;
}
.bar-label {
  font-size: 13px;
  color: #374151;
  margin-bottom: 6px;
}
.bar-track {
  height: 12px;
  background: #f3f4f6;
  border-radius: 6px;
  overflow: hidden;
}
.bar-fill {
  height: 100%;
  background: linear-gradient(90deg, #3b82f6, #60a5fa);
  border-radius: 6px;
  transition: width 0.5s ease;
}
.bar-val {
  font-size: 12px;
  color: #6b7280;
  margin-top: 4px;
}
.agent-summary {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 8px 0;
}
.summary-item {
  display: flex;
  justify-content: space-between;
  padding: 12px;
  background: #f9fafb;
  border-radius: 8px;
}
.summary-item .label {
  font-size: 13px;
  color: #4b5563;
}
.summary-item .num {
  font-size: 14px;
  font-weight: 600;
}
.text-primary { color: #2563eb; }
.text-success { color: #059669; }
.text-warning { color: #d97706; }
</style>
