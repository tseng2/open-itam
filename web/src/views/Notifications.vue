<template>
  <div class="notifications-view">
    <div class="page-header">
      <div class="title-area">
        <h2>消息中心</h2>
        <span class="subtitle">我的站内信收件箱：设备申请、资产告警、耗材预警、许可到期四类事件源；点击消息就地已读并跳转关联页面</span>
      </div>
      <div class="actions">
        <el-button type="primary" plain :disabled="!unreadCount" @click="markAllRead">{{ markAllLabel }}</el-button>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" class="filter-form">
        <el-form-item label="类型">
          <el-select v-model="typeFilter" placeholder="全部类型" clearable style="width: 160px" @change="search">
            <el-option v-for="(label, value) in typeTexts" :key="value" :label="label" :value="value" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="unreadOnly" @change="search">仅看未读</el-checkbox>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="search">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%" row-class-name="notif-row" @row-click="openNotification">
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag v-if="!row.read_at" type="danger" size="small">未读</el-tag>
            <el-tag v-else type="info" size="small" effect="plain">已读</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="typeTag(row.type)" size="small">{{ typeText(row.type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="消息" min-width="320">
          <template #default="{ row }">
            <div :class="{ 'msg-title-unread': !row.read_at, 'msg-title': row.read_at }">{{ row.title }}</div>
            <div class="msg-content">{{ row.content }}</div>
          </template>
        </el-table-column>
        <el-table-column label="接收时间" width="160">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="跳转" width="90">
          <template #default="{ row }">
            <el-icon v-if="routeOf(row)" class="jump-icon"><Right /></el-icon>
            <span v-else class="empty-cell">—</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="emptyText" />
        </template>
      </el-table>

      <div class="pagination-area">
        <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total"
          :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
          @size-change="fetchList" @current-change="fetchList" />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { ElMessage } from 'element-plus'
import {
  NOTIFICATION_TYPE_TEXTS,
  notificationRoute,
  notificationTypeText,
  notificationTypeTag,
} from '../notifications'

// 独立消息中心页（P2 收官）：铃铛 popover 只看最近 20 条，本页提供
// 类型过滤 + 未读开关 + 分页全量收件箱；user_id 由 JWT 本人收口，
// 传参无效；resource → 路由映射与铃铛共用 ../notifications 共享模块
const router = useRouter()

const typeTexts = NOTIFICATION_TYPE_TEXTS
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const typeFilter = ref('')
const unreadOnly = ref(false)
const unreadCount = ref(0)

const emptyText = computed(() =>
  unreadOnly.value ? '没有未读消息' : '暂无消息',
)

// 按钮文案走 computed：模板内联三元含中文括号是已知构建坑
const markAllLabel = computed(() =>
  unreadCount.value ? `全部已读（${unreadCount.value}）` : '全部已读',
)

function typeText(t) {
  return notificationTypeText(t)
}

function typeTag(t) {
  return notificationTypeTag(t)
}

function routeOf(row) {
  return notificationRoute(row.resource)
}

function formatTime(t) {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '-'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function search() {
  page.value = 1
  fetchList()
}

async function fetchList() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (typeFilter.value) params.append('type', typeFilter.value)
    if (unreadOnly.value) params.append('unread', 'true')
    params.append('page', page.value)
    params.append('page_size', pageSize.value)
    const res = await api(`/api/v1/notifications?${params}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载消息失败')
  } finally {
    loading.value = false
  }
  fetchUnreadCount()
}

async function fetchUnreadCount() {
  try {
    const d = await api('/api/v1/notifications/unread-count')
    unreadCount.value = d.data?.count || 0
  } catch { /* 徽标失败不阻塞列表 */ }
}

// 点击消息：未读就地标记已读，再按 resource 跳转关联页面
//（标记失败不拦截跳转，与顶栏铃铛同语义）
async function openNotification(row) {
  if (!row.read_at) {
    try {
      await api(`/api/v1/notifications/${row.id}/read`, { method: 'POST' })
      row.read_at = new Date().toISOString()
      unreadCount.value = Math.max(0, unreadCount.value - 1)
      if (unreadOnly.value) {
        items.value = items.value.filter(n => n.id !== row.id)
        total.value = Math.max(0, total.value - 1)
      }
    } catch { /* ignore */ }
  }
  const target = routeOf(row)
  if (target) router.push(target)
}

async function markAllRead() {
  try {
    const d = await api('/api/v1/notifications/read-all', { method: 'POST' })
    const affected = d.data?.affected || 0
    ElMessage.success(`已全部标记已读（${affected} 条）`)
    unreadCount.value = 0
    if (unreadOnly.value) {
      items.value = []
      total.value = 0
    } else {
      items.value.forEach(n => { n.read_at = n.read_at || new Date().toISOString() })
    }
  } catch (err) {
    ElMessage.error(err.message || '全部已读失败')
  }
}

onMounted(() => {
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
:deep(.notif-row) { cursor: pointer; }
.msg-title { font-size: 13px; color: #64748b; }
.msg-title-unread { font-size: 13px; font-weight: 600; color: #1e293b; }
.msg-content { font-size: 12px; color: #94a3b8; margin-top: 2px; word-break: break-all; }
.jump-icon { color: #3b82f6; }
.empty-cell { color: #cbd5e1; }
</style>
