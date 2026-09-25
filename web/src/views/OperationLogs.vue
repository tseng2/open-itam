<template>
  <div>
    <div class="toolbar">
      <!-- 公司过滤可空 = 全部（含全局配置/登录等 company_id=0 的审计行） -->
      <el-select v-model="companyId" placeholder="全部公司" clearable filterable style="width: 170px" @change="reload">
        <el-option v-for="c in companies" :key="c.id" :value="c.id" :label="c.name" />
      </el-select>
      <el-select v-model="action" placeholder="动作" clearable style="width: 130px" @change="reload">
        <el-option v-for="a in ACTION_OPTIONS" :key="a" :value="a" :label="actionLabel(a)" />
      </el-select>
      <el-select v-model="resource" placeholder="对象" clearable filterable style="width: 160px" @change="reload">
        <el-option v-for="r in RESOURCE_OPTIONS" :key="r.value" :value="r.value" :label="r.label" />
      </el-select>
      <el-input v-model="keyword" placeholder="操作人" clearable style="width: 140px" @keyup.enter="reload" @clear="reload" />
      <el-date-picker v-model="range" type="datetimerange" start-placeholder="开始时间" end-placeholder="结束时间"
        style="width: 345px" @change="reload" />
      <el-button type="primary" :loading="loading" @click="reload">查询</el-button>
    </div>

    <el-table v-loading="loading" :data="items" stripe>
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="expand-body">
            <p><span class="expand-label">请求路径：</span>{{ row.path || '-' }}</p>
            <p><span class="expand-label">终端特征：</span>{{ row.user_agent || '-' }}</p>
            <p><span class="expand-label">请求明细：</span></p>
            <pre class="detail-pre">{{ prettyDetail(row.detail) }}</pre>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="时间" width="170">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作人" width="170">
        <template #default="{ row }">
          <span class="op-username">{{ row.username || '未知' }}</span>
          <el-tag size="small" :type="roleTag(row.role)" class="role-tag">{{ roleLabel(row.role) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="动作" width="110">
        <template #default="{ row }">
          <el-tag size="small" :type="actionTag(row.action)">{{ actionLabel(row.action) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="对象" min-width="170">
        <template #default="{ row }">
          <span>{{ resourceLabel(row.resource) }}</span>
          <span v-if="row.resource_id" class="res-id">#{{ row.resource_id }}</span>
        </template>
      </el-table-column>
      <el-table-column label="结果" width="90" align="center">
        <template #default="{ row }">
          <el-tag size="small" :type="row.status < 400 ? 'success' : 'danger'">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="ip" label="来源 IP" width="140" />
      <el-table-column label="所属" width="110">
        <template #default="{ row }">
          <el-tag v-if="!row.company_id" size="small" type="info" effect="plain">全局</el-tag>
          <span v-else>{{ companyName(row.company_id) }}</span>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination class="pager" v-model:current-page="page" v-model:page-size="pageSize"
      :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next"
      @size-change="reload" @current-change="reload" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'

// 审计面是 admin 只读查询；本页不发任何写请求（审计流水不可变）
const companies = ref([])
const companyId = ref('')
const action = ref('')
const resource = ref('')
const keyword = ref('')
const range = ref(null)
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)

// 动作选项：五个规范动作 + 常见路径派生动作（服务端按路径尾段派生）
const ACTION_OPTIONS = [
  'create', 'update', 'delete', 'login', 'login_failed',
  'import', 'return', 'cancel', 'approve', 'reject', 'start', 'finish',
  'rotate-token', 'off-book', 'restore-book', 'merge', 'recalculate',
]
// 对象选项与服务端路由前缀一一对应（含公开面单独留痕的 auth）
const RESOURCE_OPTIONS = [
  { value: 'assets', label: '硬件资产' },
  { value: 'dispatches', label: '外派登记' },
  { value: 'stocktakes', label: '盘点任务' },
  { value: 'asset-requests', label: '设备申请' },
  { value: 'depreciations', label: '折旧规则' },
  { value: 'manufacturers', label: '厂商库' },
  { value: 'suppliers', label: '供应商库' },
  { value: 'locations', label: '位置库' },
  { value: 'asset-models', label: '型号库' },
  { value: 'users', label: '用户' },
  { value: 'companies', label: '公司' },
  { value: 'protection', label: '终端防护' },
  { value: 'webhook-alerts', label: '告警配置' },
  { value: 'asset-repairs', label: '维修登记' },
  { value: 'storage-lendings', label: '移动存储' },
  { value: 'part-records', label: '配件记录' },
  { value: 'auth', label: '登录认证' },
]

const ACTION_LABELS = {
  create: '新增', update: '修改', delete: '删除', login: '登录', login_failed: '登录失败',
  import: '批量导入', return: '归还', cancel: '作废', approve: '通过', reject: '驳回',
  start: '开始', finish: '完成', 'rotate-token': '换码', 'off-book': '销账转列管',
  'restore-book': '恢复在册', merge: '合并', recalculate: '重算净值',
}

function actionLabel(a) { return ACTION_LABELS[a] || a }
function actionTag(a) {
  if (a === 'delete') return 'danger'
  if (a === 'login_failed') return 'danger'
  if (a === 'login') return 'success'
  if (a === 'create') return 'success'
  return 'warning'
}
function resourceLabel(r) {
  const hit = RESOURCE_OPTIONS.find(o => o.value === r)
  return hit ? hit.label : r
}
function roleLabel(r) {
  return { super_admin: '超管', admin: '管理员', user: '用户' }[r] || (r || '-')
}
function roleTag(r) {
  return { super_admin: 'danger', admin: 'warning', user: 'info' }[r] || 'info'
}
function companyName(id) {
  const hit = companies.value.find(c => c.id === id)
  return hit ? hit.name : `#${id}`
}
function fmtTime(ts) {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN', { hour12: false })
}
function prettyDetail(raw) {
  if (!raw) return '（未采集：GET/无报文/multipart 或超限）'
  try { return JSON.stringify(JSON.parse(raw), null, 2) } catch { return raw }
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch { /* 公司下拉拉取失败不阻塞审计查询 */ }
}

async function reload() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    // company_id 缺省 = 全部（含全局）；审计行本体带公司快照，越权面由服务端 RBAC 收口
    if (companyId.value) params.append('company_id', companyId.value)
    if (action.value) params.append('action', action.value)
    if (resource.value) params.append('resource', resource.value)
    if (keyword.value.trim()) params.append('keyword', keyword.value.trim())
    if (range.value && range.value.length === 2) {
      // 服务端按 RFC3339 闭区间过滤，前端统一发 UTC 保证与落库口径一致
      params.append('start_time', new Date(range.value[0]).toISOString())
      params.append('end_time', new Date(range.value[1]).toISOString())
    }
    params.append('page', page.value)
    params.append('page_size', pageSize.value)
    const res = await api(`/api/v1/operation-logs?${params}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } finally {
    loading.value = false
  }
}

onMounted(() => { fetchCompanies(); reload() })
</script>

<style scoped>
.toolbar { display: flex; gap: 12px; align-items: center; margin-bottom: 16px; flex-wrap: wrap; }
.pager { margin-top: 16px; justify-content: flex-end; }
.op-username { margin-right: 6px; }
.role-tag { margin-left: 2px; }
.res-id { color: #909399; margin-left: 4px; }
.expand-body { padding: 4px 16px; color: #475569; font-size: 13px; }
.expand-label { color: #94a3b8; }
.detail-pre {
  background: #0f172a; color: #e2e8f0; padding: 12px; border-radius: 6px;
  font-size: 12px; max-width: 960px; overflow: auto; white-space: pre-wrap;
}
</style>
