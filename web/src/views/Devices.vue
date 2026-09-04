<template>
  <div>
    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索主机名 / device_id / 用户" clearable style="width:280px" />
      <el-radio-group v-model="statusFilter">
        <el-radio-button value="all">全部</el-radio-button>
        <el-radio-button value="online">在线</el-radio-button>
        <el-radio-button value="offline">离线</el-radio-button>
      </el-radio-group>
      <span class="count">共 {{ filtered.length }} 台</span>
    </div>
    <el-table :data="filtered" stripe @row-click="goDetail" style="cursor:pointer">
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="isOnline(row) ? 'success' : 'info'" size="small">
            {{ isOnline(row) ? '在线' : '离线' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="hostname" label="主机名" min-width="140" sortable />
      <el-table-column prop="os" label="系统" min-width="180" show-overflow-tooltip />
      <el-table-column prop="agent_version" label="Agent" width="90" />
      <el-table-column label="最后在线" width="120">
        <template #default="{ row }">{{ timeAgo(row.last_seen_at) }}</template>
      </el-table-column>
      <el-table-column prop="device_id" label="Device ID" min-width="150" show-overflow-tooltip />
    </el-table>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api, timeAgo } from '../api'

const router = useRouter()
const devices = ref([])
const search = ref('')
const statusFilter = ref('all')

function isOnline(row) {
  return Date.now() - new Date(row.last_seen_at).getTime() < 15 * 60 * 1000
}

const filtered = computed(() => {
  let list = devices.value
  if (statusFilter.value === 'online') list = list.filter(isOnline)
  if (statusFilter.value === 'offline') list = list.filter(d => !isOnline(d))
  const q = search.value.trim().toLowerCase()
  if (q) {
    list = list.filter(d =>
      (d.hostname || '').toLowerCase().includes(q) ||
      (d.device_id || '').toLowerCase().includes(q))
  }
  return list
})

function goDetail(row) {
  router.push(`/devices/${row.device_id}`)
}

onMounted(async () => {
  const d = await api('/api/v1/devices?limit=500')
  devices.value = d.devices || []
})
</script>

<style scoped>
.toolbar { display: flex; gap: 16px; align-items: center; margin-bottom: 16px; }
.count { color: #909399; font-size: 13px; }
</style>
