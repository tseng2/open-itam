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
          <el-menu-item index="/stocktakes">
            <el-icon><FullScreen /></el-icon>
            <span>盘点任务</span>
          </el-menu-item>
          <el-menu-item index="/asset-requests">
            <el-icon><Checked /></el-icon>
            <span>设备申请审批</span>
          </el-menu-item>
          <el-menu-item index="/software">
            <el-icon><Tickets /></el-icon>
            <span>软件与授权许可</span>
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
          <span>组织架构 (AD)</span>
        </el-menu-item>

        <el-menu-item index="/users">
          <el-icon><User /></el-icon>
          <span>用户与权限 (RBAC)</span>
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
import { api, getToken, setToken } from './api'

const route = useRoute()
const router = useRouter()
const token = ref(getToken())
const openChanges = ref(0)

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
    '/assets': '硬件固定资产台账',
    '/dispatches': '外派出差终端管理',
    '/stocktakes': '盘点任务管理',
    '/asset-requests': '设备申请审批',
    '/devices': '终端设备画像',
    '/changes': '硬件变更与预警中心',
    '/organization': '组织架构与人员',
    '/users': '系统用户与权限 (RBAC)',
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
onMounted(() => { 
  if (!isLoginPage.value) {
    pollChanges()
    setInterval(pollChanges, 30000) 
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
.page-content {
  padding: 20px;
}
</style>
