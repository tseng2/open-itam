<template>
  <div class="portal-view">
    <div class="page-header">
      <div class="title-area">
        <h2>我的门户 · 员工自助</h2>
        <span class="subtitle">个人视角的设备与申请全景：数据只属于你自己（服务端按登录身份收口）</span>
      </div>
      <el-button :loading="loading" @click="reloadAll">
        <el-icon><Refresh /></el-icon> 刷新
      </el-button>
    </div>

    <!-- 个人统计四卡 -->
    <el-row :gutter="16" class="stat-row">
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card blue">
          <div class="stat-header">
            <span class="stat-title">我的设备</span>
            <el-icon class="stat-icon"><Monitor /></el-icon>
          </div>
          <div class="stat-value">{{ summary.device_count }} <span class="unit">台</span></div>
          <div class="stat-footer"><span>使用中 + 维修中（报废不计）</span></div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card purple">
          <div class="stat-header">
            <span class="stat-title">待审批申请</span>
            <el-icon class="stat-icon"><Checked /></el-icon>
          </div>
          <div class="stat-value">{{ summary.pending_request_count }} <span class="unit">项</span></div>
          <div class="stat-footer"><span>提交后等待 IT 管理员处理</span></div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card green">
          <div class="stat-header">
            <span class="stat-title">持有天数</span>
            <el-icon class="stat-icon"><Clock /></el-icon>
          </div>
          <div class="stat-value">{{ summary.days_in_use }} <span class="unit">天</span></div>
          <div class="stat-footer"><span>最早一次领用至今</span></div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card orange">
          <div class="stat-header">
            <span class="stat-title">归还提醒</span>
            <el-icon class="stat-icon"><Bell /></el-icon>
          </div>
          <div class="stat-value" :class="{ 'text-danger': summary.expiring_count > 0 }">
            {{ summary.expiring_count }} <span class="unit">项</span>
          </div>
          <div class="stat-footer"><span>30 天内到期的短期借用</span></div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 我的设备 -->
    <el-card shadow="never" class="table-card">
      <template #header>
        <div class="card-header">
          <span class="title">我的设备</span>
          <el-button link type="primary" size="small" @click="$router.push('/asset-requests')">申请领用新设备</el-button>
        </div>
      </template>
      <el-table :data="myAssets" v-loading="loading" stripe>
        <el-table-column prop="asset_tag" label="资产编码" min-width="150" />
        <el-table-column label="类别 / 型号" min-width="180">
          <template #default="{ row }">
            {{ row.category_name || '-' }}
            <span v-if="row.brand || row.model" class="sub-text">· {{ [row.brand, row.model].filter(Boolean).join(' ') }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 20 ? 'success' : 'warning'" size="small">
              {{ row.status === 20 ? '使用中' : '维修中' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="软件许可" min-width="140">
          <template #default="{ row }">{{ row.license_name || '—' }}</template>
        </el-table-column>
        <el-table-column label="购入时间" width="120">
          <template #default="{ row }">{{ fmtDate(row.purchase_date) }}</template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无在册设备，去「设备申请审批」提交领用申请" />
        </template>
      </el-table>
    </el-card>

    <!-- 我的申请 -->
    <el-card shadow="never" class="table-card">
      <template #header>
        <div class="card-header"><span class="title">我的申请</span></div>
      </template>
      <el-table :data="myRequests" v-loading="loading" stripe>
        <el-table-column label="目标资产" min-width="150">
          <template #default="{ row }">{{ row.asset?.asset_tag || `资产 #${row.asset_id}` }}</template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{ row }">{{ row.is_long_term ? '长期领用' : '短期借用' }}</template>
        </el-table-column>
        <el-table-column label="归期" width="120">
          <template #default="{ row }">
            <span v-if="row.expected_return_at" :class="{ 'text-danger': isExpiring(row.expected_return_at) }">
              {{ fmtDate(row.expected_return_at) }}
            </span>
            <span v-else class="empty-cell">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="reason" label="事由" min-width="150" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="requestTag(row.status)" size="small">{{ requestText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="审批批注" min-width="140">
          <template #default="{ row }">{{ row.decision_remark || '—' }}</template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无申请记录" />
        </template>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'

// 员工自助门户（P2）：三端点全部 JWT 本人收口，无公司上下文
const loading = ref(false)
const summary = ref({ device_count: 0, pending_request_count: 0, days_in_use: 0, expiring_count: 0 })
const myAssets = ref([])
const myRequests = ref([])

function fmtDate(ts) {
  if (!ts) return '—'
  return new Date(ts).toLocaleDateString('zh-CN')
}

function requestText(s) {
  return { 10: '待审批', 20: '已通过', 30: '已驳回', 40: '已取消' }[s] || '未知'
}
function requestTag(s) {
  return { 10: 'warning', 20: 'success', 30: 'danger', 40: 'info' }[s] || 'info'
}
// 归期已进 30 天窗口标红提醒
function isExpiring(ts) {
  const due = new Date(ts).getTime()
  return due > Date.now() && due < Date.now() + 30 * 86400 * 1000
}

async function reloadAll() {
  loading.value = true
  try {
    const [summaryRes, assetsRes, requestsRes] = await Promise.all([
      api('/api/v1/portal/summary'),
      api('/api/v1/portal/my-assets?page_size=50'),
      api('/api/v1/portal/my-requests?page_size=50'),
    ])
    summary.value = summaryRes.data || summary.value
    myAssets.value = assetsRes.data?.items || []
    myRequests.value = requestsRes.data?.items || []
  } finally {
    loading.value = false
  }
}

onMounted(reloadAll)
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.title-area h2 { margin: 0; font-size: 20px; color: #1f2937; }
.subtitle { font-size: 12px; color: #94a3b8; }
.stat-row { margin-bottom: 16px; }
.stat-card { border-radius: 10px; border: none; color: #fff; }
.stat-card.blue { background: linear-gradient(135deg, #1e3a8a 0%, #3b82f6 100%); }
.stat-card.green { background: linear-gradient(135deg, #065f46 0%, #10b981 100%); }
.stat-card.purple { background: linear-gradient(135deg, #4c1d95 0%, #8b5cf6 100%); }
.stat-card.orange { background: linear-gradient(135deg, #9a3412 0%, #f97316 100%); }
.stat-header { display: flex; justify-content: space-between; align-items: center; opacity: 0.9; font-size: 13px; }
.stat-icon { font-size: 20px; }
.stat-value { font-size: 28px; font-weight: 700; margin: 12px 0 8px 0; }
.stat-value .unit { font-size: 14px; font-weight: normal; opacity: 0.85; }
.stat-footer { font-size: 12px; opacity: 0.85; border-top: 1px solid rgba(255, 255, 255, 0.15); padding-top: 8px; }
.text-danger { color: #fecaca; }
.table-card { border-radius: 10px; margin-bottom: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.card-header .title { font-weight: 600; color: #1f2937; }
.sub-text { color: #94a3b8; font-size: 12px; }
.empty-cell { color: #c0c4cc; }
</style>
