<template>
  <el-container style="min-height:100vh">
    <el-header class="topbar">
      <span class="brand">IT 资产管理</span>
      <el-menu :default-active="$route.path" mode="horizontal" :ellipsis="false" router>
        <el-menu-item index="/devices">设备</el-menu-item>
        <el-menu-item index="/changes">
          变更中心
          <el-badge v-if="openChanges > 0" :value="openChanges" class="badge" />
        </el-menu-item>
      </el-menu>
      <div class="spacer" />
      <el-input
        v-model="token"
        placeholder="管理 Token"
        type="password"
        style="width:220px"
        @keyup.enter="saveToken"
      >
        <template #append>
          <el-button @click="saveToken">保存</el-button>
        </template>
      </el-input>
    </el-header>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>

<script setup>
import { ref, provide, onMounted } from 'vue'
import { api, getToken, setToken } from './api'

const token = ref(getToken())
const openChanges = ref(0)

function saveToken() {
  setToken(token.value)
  location.reload()
}

async function pollChanges() {
  try {
    const d = await api('/api/v1/changes?limit=1')
    openChanges.value = (d.events || []).length
    const all = await api('/api/v1/changes?limit=500')
    openChanges.value = (all.events || []).filter(e => !e.acked).length
  } catch { /* ignore */ }
}
provide('pollChanges', pollChanges)
onMounted(() => { pollChanges(); setInterval(pollChanges, 30000) })
</script>

<style>
body { margin: 0; font-family: inherit; background: #f5f7fa; }
.topbar { display: flex; align-items: center; gap: 24px; background: #fff; border-bottom: 1px solid #e4e7ed; }
.brand { font-weight: 700; font-size: 16px; }
.spacer { flex: 1; }
.badge { margin-left: 6px; }
.el-menu--horizontal { border-bottom: none; }
</style>
