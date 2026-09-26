<template>
  <div v-if="isLoginPage" class="login-wrapper">
    <router-view />
  </div>

  <!-- 免登录移动扫码页（P0-β）：脱离管理端外壳，全屏移动布局 -->
  <div v-else-if="isMobilePage" class="mobile-wrapper">
    <router-view />
  </div>

  <el-container v-else class="app-layout">
    <!-- 侧边导航栏 -->
    <el-aside width="240px" class="sidebar">
      <div class="sidebar-brand">
        <div class="logo-icon">
          <el-icon><Platform /></el-icon>
        </div>
        <div class="brand-text">
          <span class="main-title">ITAM 资产管理</span>
          <span class="sub-title">集团多组织终端管控平台</span>
        </div>
      </div>

      <el-menu
        :default-active="$route.path"
        class="sidebar-menu"
        background-color="#111827"
        text-color="#9ca3af"
        active-text-color="#3b82f6"
        router
      >
        <el-menu-item index="/dashboard">
          <el-icon><DataLine /></el-icon>
          <span>总览大盘</span>
        </el-menu-item>

        <!-- P2 员工自助门户：个人视角（JWT 本人收口），全员可见 -->
        <el-menu-item index="/portal">
          <el-icon><UserFilled /></el-icon>
          <span>我的门户 (自助)</span>
        </el-menu-item>

        <!-- P2 消息中心独立页：全员可见（JWT 本人收口），铃铛 popover 的全量入口 -->
        <el-menu-item index="/notifications">
          <el-icon><Bell /></el-icon>
          <span>消息中心</span>
        </el-menu-item>

        <el-sub-menu index="assets-group">
          <template #title>
            <el-icon><Box /></el-icon>
            <span>资产管理</span>
          </template>
          <el-menu-item index="/assets">
            <el-icon><Monitor /></el-icon>
            <span>硬件固定资产</span>
          </el-menu-item>
          <el-menu-item index="/dispatches">
            <el-icon><Suitcase /></el-icon>
            <span>外派出差终端</span>
          </el-menu-item>
          <el-menu-item index="/storage-lendings">
            <el-icon><Wallet /></el-icon>
            <span>移动存储领用</span>
          </el-menu-item>
          <el-menu-item index="/part-records">
            <el-icon><ShoppingCart /></el-icon>
            <span>配件出入库</span>
          </el-menu-item>
          <el-menu-item index="/stocktakes">
            <el-icon><FullScreen /></el-icon>
            <span>盘点任务</span>
          </el-menu-item>
          <el-menu-item index="/asset-requests">
            <el-icon><Checked /></el-icon>
            <span>设备申请审批</span>
          </el-menu-item>
          <el-menu-item index="/depreciations">
            <el-icon><Coin /></el-icon>
            <span>折旧规则引擎</span>
          </el-menu-item>
          <el-menu-item index="/dimensions">
            <el-icon><Files /></el-icon>
            <span>基础数据（维度库）</span>
          </el-menu-item>
          <el-menu-item index="/software">
            <el-icon><Tickets /></el-icon>
            <span>软件与授权许可</span>
          </el-menu-item>
          <el-menu-item index="/consumables">
            <el-icon><Goods /></el-icon>
            <span>耗材管理</span>
          </el-menu-item>
        </el-sub-menu>

        <el-sub-menu index="agent-group">
          <template #title>
            <el-icon><Cpu /></el-icon>
            <span>终端与采集</span>
          </template>
          <el-menu-item index="/devices">
            <el-icon><Connection /></el-icon>
            <span>在线终端画像</span>
          </el-menu-item>
          <el-menu-item index="/changes">
            <el-icon><Warning /></el-icon>
            <span>预警与硬件变更</span>
            <el-badge v-if="openChanges > 0" :value="openChanges" class="menu-badge" />
          </el-menu-item>
        </el-sub-menu>

        <el-menu-item index="/organization">
          <el-icon><OfficeBuilding /></el-icon>
          <span>组织架构</span>
        </el-menu-item>

        <el-menu-item index="/users">
          <el-icon><User /></el-icon>
          <span>用户与权限 (RBAC)</span>
        </el-menu-item>

        <!-- P2 操作日志：admin 只读审计面（服务端 RBAC 收口，菜单不区分角色） -->
        <el-menu-item index="/operation-logs">
          <el-icon><Memo /></el-icon>
          <span>操作日志 (审计)</span>
        </el-menu-item>

        <!-- P2 报表中心：管理视角聚合面（服务端 RBAC 收口） -->
        <el-menu-item index="/reports">
          <el-icon><TrendCharts /></el-icon>
          <span>报表中心</span>
        </el-menu-item>

        <el-menu-item index="/settings">
          <el-icon><Setting /></el-icon>
          <span>系统与 U8 集成</span>
        </el-menu-item>
      </el-menu>

      <div class="sidebar-footer">
        <div class="version-badge">Version 1.0.0 (自研版)</div>
      </div>
    </el-aside>

    <!-- 右侧内容主区 -->
    <el-container class="main-container">
      <el-header class="top-header">
        <div class="header-left">
          <el-breadcrumb separator="/">
            <el-breadcrumb-item :to="{ path: '/' }">系统首页</el-breadcrumb-item>
            <el-breadcrumb-item>{{ currentRouteTitle }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <div class="header-right">
          <el-tag type="info" effect="plain" class="company-tag">集团管控模式: 全公司</el-tag>
          <!-- P2 消息中心起步：站内信铃铛（未读徽标 30s 轮询，点击展开收件箱） -->
          <el-popover placement="bottom" :width="380" trigger="click" @show="loadNotifications">
            <template #reference>
              <el-badge :value="unreadCount" :hidden="!unreadCount" :max="99" class="bell-badge">
                <el-icon :size="18" class="bell-icon"><Bell /></el-icon>
              </el-badge>
            </template>
            <div class="notif-panel">
              <div class="notif-head">
                <span class="notif-head-title">消息中心</span>
                <span class="notif-head-actions">
                  <el-button link type="primary" size="small" @click="goNotifications">查看全部</el-button>
                  <el-button link type="primary" size="small" :disabled="!unreadCount" @click="markAllRead">全部已读</el-button>
                </span>
              </div>
              <el-scrollbar max-height="360px">
                <div v-if="!notifications.length" class="notif-empty">暂无消息</div>
                <div v-for="n in notifications" :key="n.id"
                  class="notif-item" :class="{ 'is-unread': !n.read_at }" @click="openNotification(n)">
                  <div class="notif-title">{{ n.title }}</div>
                  <div class="notif-content">{{ n.content }}</div>
                  <div class="notif-time">{{ timeAgo(n.created_at) }}</div>
                </div>
              </el-scrollbar>
            </div>
          </el-popover>
          <div class="user-profile" v-if="currentUser">
            <el-tag type="success" size="small">{{ currentUser.role === 'super_admin' ? '超级管理员' : (currentUser.role === 'admin' ? '管理员' : '普通用户') }}</el-tag>
            <span class="username-display">{{ currentUser.real_name || currentUser.username }}</span>
          </div>
          <el-button size="small" type="danger" plain @click="handleLogout">退出登录</el-button>
        </div>
      </el-header>

      <el-main class="page-content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, provide, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, getToken, setToken, timeAgo } from './api'
import { notificationRoute } from './notifications'

const route = useRoute()
const router = useRouter()
const token = ref(getToken())
const openChanges = ref(0)
const unreadCount = ref(0)
const notifications = ref([])

const isLoginPage = computed(() => route.path === '/login')
const isMobilePage = computed(() => !!route.meta.mobile)

const currentUser = computed(() => {
  try {
    const raw = localStorage.getItem('itagent_user')
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
})

const currentRouteTitle = computed(() => {
  const map = {
    '/dashboard': '总览大盘',
    '/portal': '我的门户（员工自助）',
    '/notifications': '消息中心',
    '/assets': '硬件固定资产台账',
    '/dispatches': '外派出差终端管理',
    '/storage-lendings': '移动存储领用',
    '/part-records': '配件出入库记录',
    '/stocktakes': '盘点任务管理',
    '/asset-requests': '设备申请审批',
    '/depreciations': '折旧规则引擎',
    '/dimensions': '基础数据（维度库）',
    '/consumables': '耗材管理',
    '/devices': '终端设备画像',
    '/changes': '硬件变更与预警中心',
    '/organization': '组织与人员管理',
    '/users': '系统用户与权限 (RBAC)',
    '/operation-logs': '操作日志审计',
    '/reports': '报表中心',
    '/software': '软件资产与授权',
    '/settings': '系统与集成设置',
  }
  return map[route.path] || '页面'
})

function handleLogout() {
  setToken('')
  localStorage.removeItem('itagent_user')
  router.push('/login')
}

async function pollChanges() {
  if (isLoginPage.value || !getToken()) return
  try {
    const d = await api('/api/v1/changes?limit=1')
    openChanges.value = (d.events || []).length
    const all = await api('/api/v1/changes?limit=500')
    openChanges.value = (all.events || []).filter(e => !e.acked).length
  } catch { /* ignore */ }
}
provide('pollChanges', pollChanges)

// ---- P2 消息中心起步：站内信铃铛 ----
// 未读计数与预警徽标同节奏轮询（30s）；company_id 缺省 = 不限公司，
// 收件箱边界由 JWT 本人决定
async function pollUnread() {
  if (isLoginPage.value || !getToken()) return
  try {
    const d = await api('/api/v1/notifications/unread-count')
    unreadCount.value = d.data?.count || 0
  } catch { /* ignore */ }
}

async function loadNotifications() {
  try {
    const d = await api('/api/v1/notifications?page_size=20')
    notifications.value = d.data?.items || []
  } catch { /* ignore */ }
}

// 点击消息：就地标记已读并按 resource 跳转对应页面
//（映射与独立消息中心页共用 ./notifications 共享模块，禁止两处复制）
async function openNotification(n) {
  if (!n.read_at) {
    try {
      await api(`/api/v1/notifications/${n.id}/read`, { method: 'POST' })
      n.read_at = new Date().toISOString()
      unreadCount.value = Math.max(0, unreadCount.value - 1)
    } catch { /* ignore */ }
  }
  const target = notificationRoute(n.resource)
  if (target) router.push(target)
}

// 铃铛 popover 只看最近 20 条，全量收件箱去独立消息中心页
function goNotifications() {
  router.push('/notifications')
}

async function markAllRead() {
  try {
    await api('/api/v1/notifications/read-all', { method: 'POST' })
    unreadCount.value = 0
    notifications.value.forEach(n => { n.read_at = n.read_at || new Date().toISOString() })
  } catch { /* ignore */ }
}

onMounted(() => {
  if (!isLoginPage.value) {
    pollChanges()
    pollUnread()
    setInterval(pollChanges, 30000)
    setInterval(pollUnread, 30000)
  }
})
</script>

<style>
body {
  margin: 0;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  background-color: #f3f4f6;
}
.login-wrapper {
  width: 100vw;
  height: 100vh;
  overflow: hidden;
}
/* 免登录移动扫码页：全屏布局，页面内部自带移动端样式 */
.mobile-wrapper {
  width: 100vw;
  min-height: 100vh;
  background: #f5f7fa;
}
.user-profile {
  display: flex;
  align-items: center;
  gap: 8px;
}
.username-display {
  font-size: 14px;
  font-weight: 500;
  color: #334155;
}
.app-layout {
  min-height: 100vh;
}
.sidebar {
  background-color: #111827;
  display: flex;
  flex-direction: column;
  border-right: 1px solid #1f2937;
}
.sidebar-brand {
  height: 64px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 18px;
  background-color: #0f172a;
  border-bottom: 1px solid #1e293b;
}
.logo-icon {
  width: 34px;
  height: 34px;
  background: linear-gradient(135deg, #2563eb, #3b82f6);
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 18px;
}
.brand-text {
  display: flex;
  flex-direction: column;
}
.main-title {
  color: #f9fafb;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 0.5px;
}
.sub-title {
  color: #94a3b8;
  font-size: 11px;
}
.sidebar-menu {
  border-right: none;
  flex: 1;
}
.sidebar-menu .el-menu-item,
.sidebar-menu .el-sub-menu__title {
  height: 50px;
  line-height: 50px;
}
.sidebar-menu .el-menu-item.is-active {
  background-color: #1e293b !important;
  border-left: 3px solid #3b82f6;
}
.menu-badge {
  margin-left: 8px;
}
.sidebar-footer {
  padding: 16px;
  border-top: 1px solid #1e293b;
  text-align: center;
}
.version-badge {
  color: #64748b;
  font-size: 12px;
}
.main-container {
  background-color: #f8fafc;
}
.top-header {
  height: 64px;
  background-color: #ffffff;
  border-bottom: 1px solid #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
}
.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}
.company-tag {
  background: #f1f5f9;
  border: 1px solid #cbd5e1;
  color: #475569;
  font-size: 12px;
}
/* 站内信铃铛与收件箱面板 */
.bell-badge {
  cursor: pointer;
  display: flex;
  align-items: center;
}
.bell-icon {
  color: #475569;
}
.notif-panel {
  display: flex;
  flex-direction: column;
}
.notif-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 4px 8px 4px;
  border-bottom: 1px solid #e2e8f0;
}
.notif-head-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
.notif-head-title {
  font-size: 14px;
  font-weight: 600;
  color: #1e293b;
}
.notif-empty {
  padding: 32px 0;
  text-align: center;
  color: #94a3b8;
  font-size: 13px;
}
.notif-item {
  padding: 10px 6px;
  border-bottom: 1px dashed #e2e8f0;
  cursor: pointer;
  border-radius: 6px;
}
.notif-item:hover {
  background: #f1f5f9;
}
.notif-item.is-unread .notif-title {
  font-weight: 600;
  color: #1e293b;
}
.notif-title {
  font-size: 13px;
  color: #334155;
  margin-bottom: 4px;
}
.notif-content {
  font-size: 12px;
  color: #64748b;
  margin-bottom: 4px;
  word-break: break-all;
}
.notif-time {
  font-size: 11px;
  color: #94a3b8;
}
.page-content {
  padding: 20px;
}
</style>
