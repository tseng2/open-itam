<template>
  <div v-if="device">
    <el-page-header @back="$router.back()" :title="'返回'" class="back">
      <template #content>
        <span class="title">{{ device.hostname }} <el-tag size="small" :type="online ? 'success' : 'info'">{{ online ? '在线' : '离线' }}</el-tag></span>
      </template>
    </el-page-header>

    <el-tabs v-model="tab">
      <el-tab-pane label="概览" name="overview">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="主机名">{{ device.hostname }}</el-descriptions-item>
          <el-descriptions-item label="OS">{{ fullPayload?.os?.name }} {{ fullPayload?.os?.version }}</el-descriptions-item>
          <el-descriptions-item label="品牌">{{ fullPayload?.hardware?.brand }}</el-descriptions-item>
          <el-descriptions-item label="型号">{{ fullPayload?.hardware?.model }}</el-descriptions-item>
          <el-descriptions-item label="主机序列号">{{ fullPayload?.hardware?.serial }}</el-descriptions-item>
          <el-descriptions-item label="BIOS 序列号">{{ fullPayload?.hardware?.bios_serial }}</el-descriptions-item>
          <el-descriptions-item label="登录用户">
            <span v-if="fullPayload?.logon">
              <el-tag size="small" :type="fullPayload.logon.logon_type === 'ad' ? 'success' : 'info'">
                {{ fullPayload.logon.logon_type === 'ad' ? 'AD 域' : '本机' }}
              </el-tag>
              {{ fullPayload.logon.logon_domain }}\{{ fullPayload.logon.logon_user }}
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="Agent 版本">{{ device.agent_version }}</el-descriptions-item>
          <el-descriptions-item label="最后在线">{{ timeAgo(device.last_seen_at) }}</el-descriptions-item>
          <el-descriptions-item label="Device ID">{{ device.device_id }}</el-descriptions-item>
        </el-descriptions>

        <div class="overview-actions">
          <el-button type="warning" @click="openUninstallCode">生成卸载验证码</el-button>
        </div>
      </el-tab-pane>

      <el-tab-pane label="硬件" name="hardware">
        <h4>磁盘（含健康度）</h4>
        <el-table :data="fullPayload?.hardware?.disks || []" border>
          <el-table-column prop="model" label="型号" min-width="160" />
          <el-table-column prop="size_gb" label="容量 GB" width="100" />
          <el-table-column prop="type" label="类型" width="80" />
          <el-table-column prop="serial" label="序列号" min-width="160" />
          <el-table-column prop="removable" label="可移动" width="80" />
        </el-table>

        <h4>磁盘健康 (SMART)</h4>
        <el-table :data="fullPayload?.hardware?.disk_smart_health || []" border>
          <el-table-column prop="model" label="型号" min-width="160" />
          <el-table-column prop="disk_serial" label="序列号" min-width="160" />
          <el-table-column label="健康" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="healthType(row.overall_health)">
                {{ row.overall_health }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="reallocated_sectors" label="重映射扇区" width="110" />
          <el-table-column prop="pending_sectors" label="待定坏道" width="100" />
          <el-table-column prop="power_on_hours" label="通电小时" width="100" />
          <el-table-column prop="temperature_c" label="温度 °C" width="90" />
          <el-table-column prop="percent_lifetime_used" label="寿命 %" width="80" />
        </el-table>

        <h4>CPU / 内存 / GPU / 网卡</h4>
        <el-collapse v-model="hwGroups">
          <el-collapse-item title="CPU" name="cpu">
            <pre>{{ formatCPU }}</pre>
          </el-collapse-item>
          <el-collapse-item title="内存条" name="mem">
            <el-table :data="fullPayload?.hardware?.memory_modules || []" border>
              <el-table-column prop="slot" label="槽位" width="80" />
              <el-table-column prop="size_mb" label="MB" width="100" />
              <el-table-column prop="type" label="类型" width="80" />
              <el-table-column prop="speed_mhz" label="MHz" width="100" />
              <el-table-column prop="serial" label="序列号" min-width="160" />
            </el-table>
          </el-collapse-item>
          <el-collapse-item title="GPU" name="gpu">
            <pre>{{ formatGPU }}</pre>
          </el-collapse-item>
          <el-collapse-item title="网卡" name="nic">
            <el-table :data="fullPayload?.hardware?.nics || []" border>
              <el-table-column prop="name" label="名称" />
              <el-table-column prop="mac" label="MAC" />
              <el-table-column label="IP 地址">
                <template #default="{ row }">{{ (row.ips && row.ips.length) ? row.ips.join('，') : '—' }}</template>
              </el-table-column>
              <el-table-column prop="speed_mbps" label="Mbps" />
            </el-table>
          </el-collapse-item>
        </el-collapse>
      </el-tab-pane>

      <el-tab-pane label="软件" name="software">
        <el-input v-model="softSearch" placeholder="搜索软件名 / 版本" clearable style="width:300px;margin-bottom:12px" />
        <el-table :data="filteredSoftware" border max-height="600">
          <el-table-column prop="name" label="名称" min-width="280" />
          <el-table-column prop="version" label="版本" min-width="120" />
          <el-table-column prop="install_path" label="安装路径" min-width="220" show-overflow-tooltip />
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="历史" name="history">
        <el-table :data="history" border max-height="600">
          <el-table-column prop="report_type" label="类型" width="90" />
          <el-table-column prop="reported_at" label="时间" width="160" />
          <el-table-column label="概要">
            <template #default="{ row }">
              <span v-if="row.report_type === 'heartbeat'">
                登录 {{ row.payload?.logon?.logon_domain }}\{{ row.payload?.logon?.logon_user }} ·
                公网 IP {{ row.payload?.network?.public_ip || '-' }}
              </span>
              <span v-else>
                {{ row.payload?.hardware?.disks?.length }} 块磁盘 /
                {{ row.payload?.software?.length }} 个软件 /
                {{ row.payload?.hardware?.disk_smart_health?.length || 0 }} 项 SMART
              </span>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="codeVisible" title="卸载验证码" width="420px" destroy-on-close>
      <template v-if="uninstallCode">
        <el-alert
          title="将验证码转告终端用户：10 分钟内有效、单次使用、仅限本设备；终端卸载时输入即可通过在线验证"
          type="warning"
          show-icon
          :closable="false"
        />
        <div class="code-display">{{ uninstallCode.code }}</div>
        <div class="code-expire">有效期至 {{ formatExpire(uninstallCode.expires_at) }}</div>
      </template>
      <template #footer>
        <el-button @click="copyCode">复制验证码</el-button>
        <el-button type="primary" @click="codeVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, inject } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api, timeAgo } from '../api'

const route = useRoute()
const device = ref(null)
const fullPayload = ref(null)
const history = ref([])
const tab = ref('overview')
const softSearch = ref('')
const hwGroups = ref(['cpu', 'mem', 'gpu', 'nic'])
const pollChanges = inject('pollChanges', () => {})
const codeVisible = ref(false)
const uninstallCode = ref(null)

const online = computed(() => {
  if (!device.value) return false
  return Date.now() - new Date(device.value.last_seen_at).getTime() < 15 * 60 * 1000
})

const filteredSoftware = computed(() => {
  const list = fullPayload.value?.software || []
  const q = softSearch.value.trim().toLowerCase()
  if (!q) return list
  return list.filter(s => (s.name || '').toLowerCase().includes(q) || (s.version || '').toLowerCase().includes(q))
})

const formatCPU = computed(() => {
  const cpus = fullPayload.value?.hardware?.cpu || []
  return cpus.map(c => `${c.model} (${c.cores}c/${c.threads}t)`).join('\n') || '-'
})
const formatGPU = computed(() => {
  const gpus = fullPayload.value?.hardware?.gpus || []
  return gpus.map(g => `${g.model}  ${g.vram_mb || 0}MB`).join('\n') || '-'
})

function healthType(h) {
  if (h === 'PASSED') return 'success'
  if (h === 'FAILED') return 'danger'
  return 'warning'
}

onMounted(async () => {
  const id = route.params.id
  device.value = (await api(`/api/v1/devices/${id}`)).device
  const h = await api(`/api/v1/devices/${id}/history?limit=20`)
  history.value = h.reports || []
  const full = history.value.find(r => r.report_type === 'full')
  if (full) fullPayload.value = full.payload
  pollChanges()
})

async function openUninstallCode() {
  try {
    const res = await api('/api/v1/protection/uninstall-code', {
      method: 'POST',
      body: JSON.stringify({ device_id: device.value.device_id }),
    })
    uninstallCode.value = res.data
    codeVisible.value = true
  } catch (e) {
    ElMessage.error(e.message)
  }
}

function formatExpire(iso) {
  const d = new Date(iso)
  const pad = n => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

async function copyCode() {
  try {
    await navigator.clipboard.writeText(uninstallCode.value.code)
    ElMessage.success('验证码已复制')
  } catch {
    ElMessage.error('复制失败，请手动记录')
  }
}
</script>

<style scoped>
.back { margin-bottom: 16px; }
.title { font-size: 18px; font-weight: 600; }
h4 { margin: 16px 0 8px; }
pre { margin: 0; white-space: pre-wrap; font-family: inherit; }
.overview-actions { margin-top: 16px; }
.code-display {
  font-size: 32px;
  font-weight: 700;
  letter-spacing: 8px;
  text-align: center;
  padding: 16px 0;
  font-family: 'Consolas', monospace;
}
.code-expire { font-size: 12px; color: #6b7280; text-align: center; }
</style>
