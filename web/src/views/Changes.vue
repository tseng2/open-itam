<template>
  <div>
    <div class="toolbar">
      <el-radio-group v-model="scope">
        <el-radio-button value="open">未读</el-radio-button>
        <el-radio-button value="all">全部</el-radio-button>
      </el-radio-group>
      <el-select v-model="severity" placeholder="严重度" clearable style="width:140px">
        <el-option value="emergency" label="紧急" />
        <el-option value="critical" label="严重" />
        <el-option value="warning" label="警告" />
        <el-option value="info" label="提示" />
      </el-select>
      <span class="count">共 {{ filtered.length }} 条</span>
    </div>
    <el-table :data="filtered" stripe>
      <el-table-column label="时间" width="160">
        <template #default="{ row }">{{ timeAgo(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="严重度" width="90">
        <template #default="{ row }">
          <el-tag :type="sevType(row.severity)" size="small">{{ sevLabel(row.severity) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="类别" width="180">
        <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
      </el-table-column>
      <el-table-column label="设备" width="180">
        <template #default="{ row }">
          <router-link :to="`/devices/${row.device_id}`">{{ row.device_id }}</router-link>
        </template>
      </el-table-column>
      <el-table-column prop="message" label="说明" min-width="280" />
      <el-table-column label="操作" width="140">
        <template #default="{ row }">
          <el-button v-if="!row.acked" type="primary" size="small" @click="ack(row)">标记已读</el-button>
          <el-tag v-else size="small" type="info">已读</el-tag>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, inject } from 'vue'
import { api, timeAgo } from '../api'

const events = ref([])
const scope = ref('open')
const severity = ref('')
const pollChanges = inject('pollChanges', () => {})

const filtered = computed(() => {
  let list = events.value
  if (scope.value === 'open') list = list.filter(e => !e.acked)
  if (severity.value) list = list.filter(e => e.severity === severity.value)
  return list
})

async function load() {
  const d = await api('/api/v1/changes?limit=500&all=true')
  events.value = d.events || []
}

async function ack(row) {
  await api(`/api/v1/changes/${row.id}/ack`, { method: 'POST' })
  row.acked = true
  pollChanges()
}

function sevType(s) {
  if (s === 'emergency') return 'danger'
  if (s === 'critical') return 'danger'
  if (s === 'warning') return 'warning'
  return 'info'
}
function sevLabel(s) {
  return { emergency: '紧急', critical: '严重', warning: '警告', info: '提示' }[s] || s
}
function kindLabel(k) {
  return {
    disk_count_changed: '磁盘数量变更',
    disk_serial_swapped: '磁盘换件',
    memory_count_changed: '内存数量变更',
    memory_serial_swapped: '内存换件',
    cpu_changed: 'CPU 变更',
    gpu_count_changed: 'GPU 变更',
    nic_count_changed: '网卡变更',
    host_serial_changed: '主机序列号变更',
    host_model_changed: '主机型号变更',
    smart_health_failed: 'SMART 整体失败',
    smart_reallocated: 'SMART 重映射扇区',
    smart_pending: 'SMART 待定坏道',
    smart_lifetime: 'SMART 寿命预警',
    smart_temp: 'SMART 温度告警',
  }[k] || k
}

onMounted(load)
</script>

<style scoped>
.toolbar { display: flex; gap: 16px; align-items: center; margin-bottom: 16px; }
.count { color: #909399; font-size: 13px; }
</style>
