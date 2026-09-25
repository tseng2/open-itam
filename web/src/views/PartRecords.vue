<template>
  <div class="part-records-view">
    <div class="page-header">
      <div class="title-area">
        <h2>配件出入库</h2>
        <span class="subtitle">IT 配件流水台账（追加式，无编辑删除）：操作人自动取当前登录人，业务时间缺省为登记当下</span>
      </div>
      <div class="actions">
        <el-button v-if="isAdmin" type="primary" @click="openCreateDialog">
          <el-icon><Plus /></el-icon> 登记流水
        </el-button>
      </div>
    </div>

    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="companyId" placeholder="全部公司" clearable filterable style="width: 200px" @change="search">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="出入方向">
          <el-select v-model="direction" placeholder="全部方向" clearable style="width: 120px" @change="search">
            <el-option label="入库" value="in" />
            <el-option label="出库" value="out" />
          </el-select>
        </el-form-item>
        <el-form-item label="物品类型">
          <el-input v-model="partType" placeholder="精确匹配（如：内存）" clearable style="width: 170px" @keyup.enter="search" @clear="search" />
        </el-form-item>
        <el-form-item label="资产编号">
          <el-input v-model="assetTag" placeholder="模糊匹配" clearable style="width: 150px" @keyup.enter="search" @clear="search" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="search">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="table-card">
      <el-table :data="items" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="方向" width="80">
          <template #default="{ row }">
            <el-tag :type="row.direction === 'in' ? 'success' : 'warning'" size="small">
              {{ row.direction === 'in' ? '入库' : '出库' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="物品" min-width="200">
          <template #default="{ row }">
            <strong>{{ row.part_name || row.part_type }}</strong>
            <div class="sub-text">{{ [row.brand, row.part_model].filter(Boolean).join(' / ') || row.part_type }}</div>
          </template>
        </el-table-column>
        <el-table-column label="数量" width="90" align="center">
          <template #default="{ row }">{{ row.quantity }}{{ row.unit ? ` ${row.unit}` : '' }}</template>
        </el-table-column>
        <el-table-column label="储物柜 / 位置" width="150">
          <template #default="{ row }">
            <div>{{ row.locker_location || '—' }}</div>
            <div class="sub-text">{{ row.location || '存放位置未登记' }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="purpose" label="用途" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">{{ row.purpose || '—' }}</template>
        </el-table-column>
        <el-table-column label="OA 单号" width="110">
          <template #default="{ row }">{{ row.oa_number || '—' }}</template>
        </el-table-column>
        <el-table-column label="关联资产" width="130">
          <template #default="{ row }">
            <span v-if="row.asset_tag" class="tag-code">{{ row.asset_tag }}</span>
            <span v-else class="empty-cell">—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作人" width="100">
          <template #default="{ row }">{{ operatorName(row) }}</template>
        </el-table-column>
        <el-table-column label="业务时间" width="160">
          <template #default="{ row }">{{ formatTime(row.operated_at) || formatTime(row.created_at) }}</template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无配件流水，右上角登记第一笔" />
        </template>
      </el-table>

      <div class="pagination-area">
        <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total"
          :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
          @size-change="fetchList" @current-change="fetchList" />
      </div>
    </el-card>

    <!-- 登记流水：direction 必填（in/out）；操作人服务端取 JWT，operated_at 缺省为现在 -->
    <el-dialog v-model="showDialog" title="登记配件流水" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="110px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="出入方向" prop="direction">
          <el-radio-group v-model="form.direction">
            <el-radio value="in">入库（采购 / 领回）</el-radio>
            <el-radio value="out">出库（发放 / 报废）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="物品类型" prop="part_type">
          <el-input v-model="form.part_type" placeholder="如：内存 / 硬盘 / 键鼠" />
        </el-form-item>
        <el-form-item label="物品名称">
          <el-input v-model="form.part_name" placeholder="如：DDR4 16G 笔记本内存" />
        </el-form-item>
        <el-form-item label="规格型号">
          <el-input v-model="form.part_model" placeholder="如：三星 M471A1K43DB1" />
        </el-form-item>
        <el-form-item label="品牌">
          <el-input v-model="form.brand" placeholder="如：三星" />
        </el-form-item>
        <el-form-item label="数量">
          <el-input-number v-model="form.quantity" :min="1" :max="99999" style="width: 200px" />
        </el-form-item>
        <el-form-item label="单位">
          <el-input v-model="form.unit" placeholder="条 / 个 / 块" style="width: 200px" />
        </el-form-item>
        <el-form-item label="储物柜位置">
          <el-input v-model="form.locker_location" placeholder="如：IT 柜 B-03" />
        </el-form-item>
        <el-form-item label="存放位置">
          <el-input v-model="form.location" placeholder="如：3 楼机房备件区" />
        </el-form-item>
        <el-form-item label="用途">
          <el-input v-model="form.purpose" type="textarea" :rows="2" placeholder="如：cmp001 扩容 / 项目备件" />
        </el-form-item>
        <el-form-item label="OA 单号">
          <el-input v-model="form.oa_number" placeholder="关联的采购 / 报废 OA 单号" />
        </el-form-item>
        <el-form-item label="关联资产编号">
          <el-input v-model="form.asset_tag" placeholder="固定资产编号（文本关联，不做校验）" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">登记</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage } from 'element-plus'

// 配件出入库（阶段一遗留清欠）：GET/POST /api/v1/part-records，
// 追加式流水无编辑删除面；direction 常量 in/out 对应入库/出库。
// 登记流水写面服务端 RBAC 收口 admin（2026-09-26），前端同步 gate
const companies = ref([])
const companyId = ref('')
const direction = ref('')
const partType = ref('')
const assetTag = ref('')
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const submitting = ref(false)

const showDialog = ref(false)
const formRef = ref(null)

const isAdmin = computed(() => {
  try {
    const u = JSON.parse(localStorage.getItem('itagent_user') || 'null')
    return !!u && (u.role === 'admin' || u.role === 'super_admin')
  } catch {
    return false
  }
})

const emptyForm = () => ({
  company_id: '', direction: 'in', part_type: '', part_name: '',
  part_model: '', brand: '', quantity: 1, unit: '',
  locker_location: '', location: '', purpose: '', oa_number: '', asset_tag: '',
})
const form = reactive(emptyForm())

const formRules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  direction: [{ required: true, message: '请选择出入方向', trigger: 'change' }],
  part_type: [{ required: true, message: '请输入物品类型', trigger: 'blur' }],
}

function operatorName(row) {
  const op = row.operator
  if (!op) return '—'
  return op.real_name || op.username || '—'
}

function formatTime(t) {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d.getTime())) return ''
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch { /* 下拉失败不阻塞列表 */ }
}

function search() {
  page.value = 1
  fetchList()
}

async function fetchList() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (companyId.value) params.append('company_id', companyId.value)
    if (direction.value) params.append('direction', direction.value)
    if (partType.value.trim()) params.append('part_type', partType.value.trim())
    if (assetTag.value.trim()) params.append('asset_tag', assetTag.value.trim())
    params.append('page', page.value)
    params.append('page_size', pageSize.value)
    const res = await api(`/api/v1/part-records?${params}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载配件流水失败')
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  Object.assign(form, emptyForm())
  form.company_id = companyId.value || ''
  showDialog.value = true
}

async function submit() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      const body = {
        company_id: form.company_id,
        direction: form.direction,
        part_type: form.part_type.trim(),
        part_name: form.part_name.trim(),
        part_model: form.part_model.trim(),
        brand: form.brand.trim(),
        quantity: form.quantity,
        unit: form.unit.trim(),
        locker_location: form.locker_location.trim(),
        location: form.location.trim(),
        purpose: form.purpose,
        oa_number: form.oa_number.trim(),
        asset_tag: form.asset_tag.trim(),
      }
      await api('/api/v1/part-records', { method: 'POST', body: JSON.stringify(body) })
      ElMessage.success('配件流水已登记（操作人自动记录为当前登录人）')
      showDialog.value = false
      fetchList()
    } catch (err) {
      ElMessage.error(err.message || '登记失败')
    } finally {
      submitting.value = false
    }
  })
}

onMounted(() => {
  fetchCompanies()
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
.sub-text { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.empty-cell { color: #cbd5e1; }
.tag-code { font-family: monospace; color: #2563eb; }
</style>
