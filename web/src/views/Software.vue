<template>
  <div class="software-view">
    <div class="page-header">
      <div class="title-area">
        <h2>软件资产与授权管理</h2>
        <span class="subtitle">授权许可池 + 受控软件池 + 合规审计：Agent 采集的商业软件与受控池比对，识别超用与未受控安装（盗版嫌疑）</span>
      </div>
    </div>

    <!-- 三 tab 共用的公司维度 -->
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="companyId" placeholder="选择公司" clearable filterable style="width: 220px" @change="onCompanyChange">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
      </el-form>
    </el-card>

    <el-tabs v-model="activeTab" class="software-tabs" @tab-change="onTabChange">
      <!-- ================ 授权许可池 ================ -->
      <el-tab-pane label="授权许可池" name="licenses">
        <div class="tab-toolbar">
          <el-input v-model="keyword" placeholder="软件名称 / 厂商" clearable style="width: 200px" @keyup.enter="searchLicenses" @clear="searchLicenses" />
          <el-checkbox v-model="expiringOnly" @change="searchLicenses">仅看 30 天内到期</el-checkbox>
          <el-button type="primary" @click="searchLicenses">查询</el-button>
          <el-button v-if="isAdmin" type="primary" plain @click="openCreateDialog">
            <el-icon><Plus /></el-icon> 登记软件授权
          </el-button>
        </div>

        <el-table :data="items" v-loading="loading" stripe style="width: 100%">
          <el-table-column label="软件名称" min-width="200">
            <template #default="{ row }">
              <strong>{{ row.name }}</strong>
              <div class="sub-text">{{ row.vendor || '厂商未登记' }}<span v-if="row.category"> · {{ row.category }}</span></div>
            </template>
          </el-table-column>
          <el-table-column label="席位占用" width="200">
            <template #default="{ row }">
              <template v-if="row.total_seats > 0">
                <el-progress
                  :percentage="Math.min(100, Math.round((row.used_seats / row.total_seats) * 100))"
                  :status="row.used_seats > row.total_seats ? 'exception' : (row.used_seats >= row.total_seats ? 'warning' : 'success')"
                />
                <div class="sub-text">{{ row.used_seats }} / {{ row.total_seats }} 席</div>
              </template>
              <span v-else>不限席位（已用 {{ row.used_seats }}）</span>
            </template>
          </el-table-column>
          <el-table-column label="授权密钥" min-width="160">
            <template #default="{ row }">
              <span v-if="row.license_key" class="license-key">{{ maskKey(row.license_key) }}</span>
              <span v-else class="empty-cell">未登记</span>
            </template>
          </el-table-column>
          <el-table-column label="到期日" width="120">
            <template #default="{ row }">
              {{ row.expiration_date ? row.expiration_date.slice(0, 10) : '永久授权' }}
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="statusTag(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column v-if="isAdmin" label="操作" width="130" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
              <el-button link type="danger" size="small" @click="confirmDelete(row)">删除</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="暂无软件授权，右上角登记第一份授权" />
          </template>
        </el-table>

        <div class="pagination-area">
          <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total"
            :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
            @size-change="fetchList" @current-change="fetchList" />
        </div>
      </el-tab-pane>

      <!-- ================ 受控软件池（阶段三合规基准） ================ -->
      <el-tab-pane label="受控软件池" name="pools">
        <div class="tab-toolbar">
          <el-input v-model="poolKeyword" placeholder="受控软件名称" clearable style="width: 200px" @keyup.enter="searchPools" @clear="searchPools" />
          <el-button type="primary" @click="searchPools">查询</el-button>
          <el-button v-if="isAdmin" type="primary" plain @click="openPoolCreateDialog">
            <el-icon><Plus /></el-icon> 新增受控软件
          </el-button>
          <span class="toolbar-tip">入池即受控：终端安装名精确或包含命中池名均计入合规统计</span>
        </div>

        <el-table :data="poolItems" v-loading="poolLoading" stripe style="width: 100%">
          <el-table-column label="受控软件" min-width="220">
            <template #default="{ row }">
              <strong>{{ row.name }}</strong>
              <div class="sub-text">{{ row.vendor || '厂商未登记' }}<span v-if="row.category"> · {{ row.category }}</span></div>
            </template>
          </el-table-column>
          <el-table-column label="挂接许可（席位来源）" min-width="180">
            <template #default="{ row }">
              <span v-if="row.license_name">{{ row.license_name }}</span>
              <span v-else class="empty-cell">未挂接（不限席位）</span>
            </template>
          </el-table-column>
          <el-table-column prop="remark" label="备注" min-width="150" show-overflow-tooltip>
            <template #default="{ row }">{{ row.remark || '—' }}</template>
          </el-table-column>
          <el-table-column v-if="isAdmin" label="操作" width="130" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="openPoolEditDialog(row)">编辑</el-button>
              <el-button link type="danger" size="small" @click="confirmDeletePool(row)">删除</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="受控软件池为空，登记公司允许且管控的商业软件后开始合规比对" />
          </template>
        </el-table>

        <div class="pagination-area">
          <el-pagination v-model:current-page="poolPage" v-model:page-size="poolPageSize" :total="poolTotal"
            :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next"
            @size-change="fetchPools" @current-change="fetchPools" />
        </div>
      </el-tab-pane>

      <!-- ================ 合规审计（阶段三，admin） ================ -->
      <el-tab-pane v-if="isAdmin" label="合规审计" name="compliance">
        <div class="tab-toolbar">
          <el-button type="primary" @click="fetchCompliance">刷新报表</el-button>
          <span class="toolbar-tip">比对口径：受控池 × 终端软件清单（未绑定资产的终端不参与统计）；未受控商业软件清单只在此呈现不投通知</span>
        </div>

        <div class="summary-cards" v-loading="compLoading">
          <div class="summary-card">
            <div class="summary-value">{{ compSummary.pool_count }}</div>
            <div class="summary-label">受控池项</div>
          </div>
          <div class="summary-card" :class="{ 'summary-danger': compSummary.overused_count > 0 }">
            <div class="summary-value">{{ compSummary.overused_count }}</div>
            <div class="summary-label">超用池项</div>
          </div>
          <div class="summary-card">
            <div class="summary-value">{{ compSummary.managed_installs }}</div>
            <div class="summary-label">受控安装终端数</div>
          </div>
          <div class="summary-card" :class="{ 'summary-warn': compSummary.unmanaged_count > 0 }">
            <div class="summary-value">{{ compSummary.unmanaged_count }}</div>
            <div class="summary-label">未受控商业软件</div>
          </div>
        </div>

        <h3 class="section-title">受控池比对（超用 = 安装数 > 挂接许可席位）</h3>
        <el-table :data="compPoolItems" v-loading="compLoading" stripe style="width: 100%">
          <el-table-column label="受控软件" min-width="200">
            <template #default="{ row }">
              <strong>{{ row.name }}</strong>
              <div class="sub-text">{{ row.vendor || '—' }}<span v-if="row.category"> · {{ row.category }}</span></div>
            </template>
          </el-table-column>
          <el-table-column label="挂接许可 / 席位" width="180">
            <template #default="{ row }">
              <template v-if="row.total_seats > 0">{{ row.license_name }} / {{ row.total_seats }} 席</template>
              <span v-else class="empty-cell">{{ row.license_name || '未挂接（不限席位）' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="安装终端数" width="110" align="center">
            <template #default="{ row }">{{ row.installs }}</template>
          </el-table-column>
          <el-table-column label="合规状态" width="110">
            <template #default="{ row }">
              <el-tag v-if="row.overused" type="danger" size="small">超用</el-tag>
              <el-tag v-else-if="row.installs > 0" type="success" size="small">合规</el-tag>
              <el-tag v-else type="info" size="small" effect="plain">无安装</el-tag>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="未配置受控软件池，先在受控软件池 tab 登记商业软件" />
          </template>
        </el-table>

        <h3 class="section-title">未受控商业软件（不在池内、非系统组件；需甄别授权或入池）</h3>
        <el-table :data="compUnmanagedItems" v-loading="compLoading" stripe style="width: 100%">
          <el-table-column prop="name" label="软件名称" min-width="260" />
          <el-table-column label="安装终端数" width="120" align="center">
            <template #default="{ row }">{{ row.installs }}</template>
          </el-table-column>
          <template #empty>
            <el-empty description="没有未受控商业软件，或终端软件清单尚未上报（Agent 每小时 full 上报）" />
          </template>
        </el-table>
        <div class="pagination-area">
          <el-pagination v-model:current-page="compPage" :page-size="compPageSize" :total="compUnmanagedTotal"
            layout="total, prev, pager, next" @current-change="fetchCompliance" />
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 登记 / 编辑授权对话框 -->
    <el-dialog v-model="showDialog" :title="editMode ? '编辑软件授权' : '登记软件授权'" width="640px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="110px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择公司" style="width: 100%">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="软件名称" prop="name">
          <el-input v-model="form.name" placeholder="如：Microsoft 365 商业高级版" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="厂商">
              <el-input v-model="form.vendor" placeholder="Microsoft / Autodesk" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="分类">
              <el-input v-model="form.category" placeholder="办公套件 / 工程设计" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="授权密钥">
          <el-input v-model="form.license_key" placeholder="可留空（补录）" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="席位总数">
              <el-input-number v-model="form.total_seats" :min="0" style="width: 100%" />
              <div class="form-tip">0 = 不限席位</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="采购日期">
              <el-date-picker v-model="form.purchase_date" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="到期日">
              <el-date-picker v-model="form.expiration_date" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
              <div class="form-tip">留空 = 永久授权</div>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="终止日">
              <el-date-picker v-model="form.termination_date" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
              <div class="form-tip">非空即视为合同已终止（最高优先级）</div>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ editMode ? '保存' : '登记' }}</el-button>
      </template>
    </el-dialog>

    <!-- 新增 / 编辑受控软件对话框（阶段三） -->
    <el-dialog v-model="showPoolDialog" :title="poolEditMode ? '编辑受控软件' : '新增受控软件'" width="560px" destroy-on-close>
      <el-form ref="poolFormRef" :model="poolForm" :rules="poolRules" label-width="120px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="poolForm.company_id" placeholder="选择公司" style="width: 100%" @change="fetchLicenseOptions">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="受控软件名称" prop="name">
          <el-input v-model="poolForm.name" placeholder="如：Microsoft 365（终端名包含即可命中）" />
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="厂商">
              <el-input v-model="poolForm.vendor" placeholder="Microsoft / Autodesk" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="分类">
              <el-input v-model="poolForm.category" placeholder="办公套件 / 工程设计" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="挂接许可">
          <el-select v-model="poolForm.license_id" placeholder="不限席位（不挂接）" clearable filterable style="width: 100%">
            <el-option v-for="l in licenseOptions" :key="l.id" :label="`${l.name}（${l.total_seats > 0 ? l.total_seats + ' 席' : '不限'}）`" :value="l.id" />
          </el-select>
          <div class="form-tip">挂接后按许可席位做超用判定与提醒；池项不做席位余量校验（超用正是引擎要发现的）</div>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="poolForm.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showPoolDialog = false">取消</el-button>
        <el-button type="primary" :loading="poolSubmitting" @click="submitPool">{{ poolEditMode ? '保存' : '新增' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

// 软件资产三面（阶段三扩受控池 + 合规审计）：
// ① 授权许可池（P2）：状态日期派生、席位按资产挂接计数、写面 admin
// ② 受控软件池（阶段三）：公司商业软件白名单，读面登录可读写面 admin
// ③ 合规审计（阶段三）：受控池 × Agent 软件清单（device_software 每小时
//    full 上报全量覆盖），超用 = 安装数 > 挂接许可席位；未受控商业软件
//    清单按安装数降序，系统组件白名单服务端过滤
const activeTab = ref('licenses')

const companies = ref([])
const companyId = ref('')

const isAdmin = computed(() => {
  try {
    const u = JSON.parse(localStorage.getItem('itagent_user') || 'null')
    return !!u && (u.role === 'admin' || u.role === 'super_admin')
  } catch {
    return false
  }
})

// ---- 授权许可池 ----
const keyword = ref('')
const expiringOnly = ref(false)
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const submitting = ref(false)
const showDialog = ref(false)
const editMode = ref(false)
const formRef = ref(null)

const emptyForm = () => ({
  id: 0,
  company_id: '',
  name: '',
  vendor: '',
  category: '',
  license_key: '',
  total_seats: 0,
  purchase_date: '',
  expiration_date: '',
  termination_date: '',
  remark: '',
})
const form = reactive(emptyForm())

const formRules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  name: [{ required: true, message: '请输入软件名称', trigger: 'blur' }],
}

// ---- 受控软件池（阶段三） ----
const poolKeyword = ref('')
const poolItems = ref([])
const poolTotal = ref(0)
const poolPage = ref(1)
const poolPageSize = ref(20)
const poolLoading = ref(false)
const poolSubmitting = ref(false)
const showPoolDialog = ref(false)
const poolEditMode = ref(false)
const poolFormRef = ref(null)
const licenseOptions = ref([])

const emptyPoolForm = () => ({
  id: 0, company_id: '', name: '', vendor: '', category: '', license_id: null, remark: '',
})
const poolForm = reactive(emptyPoolForm())

const poolRules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  name: [{ required: true, message: '请输入受控软件名称', trigger: 'blur' }],
}

// ---- 合规审计（阶段三，admin） ----
const compLoading = ref(false)
const compSummary = ref({ pool_count: 0, overused_count: 0, managed_installs: 0, unmanaged_count: 0 })
const compPoolItems = ref([])
const compUnmanagedItems = ref([])
const compUnmanagedTotal = ref(0)
const compPage = ref(1)
const compPageSize = ref(10)

function statusText(s) {
  return { 10: '在用', 20: '已到期', 30: '已终止' }[s] || '未知'
}
function statusTag(s) {
  return { 10: 'success', 20: 'danger', 30: 'info' }[s] || 'info'
}
// 密钥脱敏：只亮出末 4 位
function maskKey(key) {
  if (key.length <= 4) return key
  return '••••••••' + key.slice(-4)
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
    if (!companyId.value && companies.value.length) {
      companyId.value = companies.value[0].id
    }
  } catch { /* 下拉失败不阻塞列表 */ }
}

function onCompanyChange() {
  page.value = 1
  poolPage.value = 1
  compPage.value = 1
  licenseOptions.value = []
  fetchActiveTab()
}

function onTabChange() {
  fetchActiveTab()
}

// 当前激活 tab 拉各自数据（公司必选口径与后端一致）
function fetchActiveTab() {
  if (!companyId.value) return
  if (activeTab.value === 'licenses') fetchList()
  if (activeTab.value === 'pools') fetchPools()
  if (activeTab.value === 'compliance') fetchCompliance()
}

// ==================== 授权许可池 ====================

function searchLicenses() {
  page.value = 1
  fetchList()
}

async function fetchList() {
  if (!companyId.value) {
    items.value = []
    total.value = 0
    return
  }
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', companyId.value)
    if (keyword.value.trim()) params.append('keyword', keyword.value.trim())
    if (expiringOnly.value) params.append('expiring_days', 30)
    params.append('page', page.value)
    params.append('page_size', pageSize.value)
    const res = await api(`/api/v1/licenses?${params}`)
    items.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载软件许可失败')
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  Object.assign(form, emptyForm())
  form.company_id = companyId.value || ''
  editMode.value = false
  showDialog.value = true
}

function openEditDialog(row) {
  Object.assign(form, emptyForm(), {
    id: row.id,
    company_id: row.company_id,
    name: row.name,
    vendor: row.vendor || '',
    category: row.category || '',
    license_key: row.license_key || '',
    total_seats: row.total_seats || 0,
    purchase_date: row.purchase_date ? row.purchase_date.slice(0, 10) : '',
    expiration_date: row.expiration_date ? row.expiration_date.slice(0, 10) : '',
    termination_date: row.termination_date ? row.termination_date.slice(0, 10) : '',
    remark: row.remark || '',
  })
  editMode.value = true
  showDialog.value = true
}

async function submit() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      // date-picker 空串统一置 null，服务端 *time.Time 才能解析
      const payload = { ...form, purchase_date: form.purchase_date || null, expiration_date: form.expiration_date || null, termination_date: form.termination_date || null }
      if (editMode.value) {
        await api(`/api/v1/licenses/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
        ElMessage.success('授权已更新')
      } else {
        await api('/api/v1/licenses', { method: 'POST', body: JSON.stringify(payload) })
        ElMessage.success('授权登记成功')
      }
      showDialog.value = false
      fetchList()
    } catch (err) {
      ElMessage.error(err.message || '保存失败')
    } finally {
      submitting.value = false
    }
  })
}

async function confirmDelete(row) {
  try {
    await ElMessageBox.confirm(
      `确定删除「${row.name}」？席位仍被资产或受控池挂接时会被服务端拦截`,
      '删除软件授权', { type: 'warning' },
    )
  } catch { return }
  try {
    await api(`/api/v1/licenses/${row.id}?company_id=${row.company_id}`, { method: 'DELETE' })
    ElMessage.success('已删除')
    fetchList()
  } catch (err) {
    ElMessage.error(err.message || '删除失败')
  }
}

// ==================== 受控软件池（阶段三） ====================

function searchPools() {
  poolPage.value = 1
  fetchPools()
}

async function fetchPools() {
  if (!companyId.value) {
    poolItems.value = []
    poolTotal.value = 0
    return
  }
  poolLoading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', companyId.value)
    if (poolKeyword.value.trim()) params.append('keyword', poolKeyword.value.trim())
    params.append('page', poolPage.value)
    params.append('page_size', poolPageSize.value)
    const res = await api(`/api/v1/software-pools?${params}`)
    poolItems.value = res.data?.items || []
    poolTotal.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载受控软件池失败')
  } finally {
    poolLoading.value = false
  }
}

// 许可下拉选项（挂接席位来源）：同公司全量缓存模式（page_size=200，折旧规则下拉先例）
async function fetchLicenseOptions() {
  if (!poolForm.company_id) {
    licenseOptions.value = []
    return
  }
  try {
    const res = await api(`/api/v1/licenses?company_id=${poolForm.company_id}&page_size=200`)
    licenseOptions.value = res.data?.items || []
  } catch { /* 下拉失败不阻塞编辑 */ }
}

function openPoolCreateDialog() {
  Object.assign(poolForm, emptyPoolForm())
  poolForm.company_id = companyId.value || ''
  poolEditMode.value = false
  licenseOptions.value = []
  if (poolForm.company_id) fetchLicenseOptions()
  showPoolDialog.value = true
}

function openPoolEditDialog(row) {
  Object.assign(poolForm, emptyPoolForm(), {
    id: row.id,
    company_id: row.company_id,
    name: row.name,
    vendor: row.vendor || '',
    category: row.category || '',
    license_id: row.license_id || null,
    remark: row.remark || '',
  })
  poolEditMode.value = true
  licenseOptions.value = []
  fetchLicenseOptions()
  showPoolDialog.value = true
}

async function submitPool() {
  if (!poolFormRef.value) return
  await poolFormRef.value.validate(async (valid) => {
    if (!valid) return
    poolSubmitting.value = true
    try {
      const payload = {
        company_id: poolForm.company_id,
        name: poolForm.name.trim(),
        vendor: poolForm.vendor.trim(),
        category: poolForm.category.trim(),
        license_id: poolForm.license_id || 0,
        remark: poolForm.remark,
      }
      if (poolEditMode.value) {
        await api(`/api/v1/software-pools/${poolForm.id}`, { method: 'PUT', body: JSON.stringify(payload) })
        ElMessage.success('受控软件已更新')
      } else {
        await api('/api/v1/software-pools', { method: 'POST', body: JSON.stringify(payload) })
        ElMessage.success('受控软件已入池')
      }
      showPoolDialog.value = false
      fetchPools()
    } catch (err) {
      ElMessage.error(err.message || '保存失败')
    } finally {
      poolSubmitting.value = false
    }
  })
}

async function confirmDeletePool(row) {
  try {
    await ElMessageBox.confirm(
      `确定删除受控软件「${row.name}」？删除后该软件的终端安装将归入未受控清单`,
      '删除受控软件', { type: 'warning' },
    )
  } catch { return }
  try {
    await api(`/api/v1/software-pools/${row.id}?company_id=${row.company_id}`, { method: 'DELETE' })
    ElMessage.success('已删除')
    fetchPools()
  } catch (err) {
    ElMessage.error(err.message || '删除失败')
  }
}

// ==================== 合规审计（阶段三，admin） ====================

async function fetchCompliance() {
  if (!companyId.value) {
    compPoolItems.value = []
    compUnmanagedItems.value = []
    compUnmanagedTotal.value = 0
    compSummary.value = { pool_count: 0, overused_count: 0, managed_installs: 0, unmanaged_count: 0 }
    return
  }
  compLoading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', companyId.value)
    params.append('page', compPage.value)
    params.append('page_size', compPageSize.value)
    const res = await api(`/api/v1/software-compliance?${params}`)
    const d = res.data || {}
    compSummary.value = d.summary || compSummary.value
    compPoolItems.value = d.pool_items || []
    compUnmanagedItems.value = d.unmanaged?.items || []
    compUnmanagedTotal.value = d.unmanaged?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载合规报表失败')
  } finally {
    compLoading.value = false
  }
}

onMounted(async () => {
  await fetchCompanies()
  fetchActiveTab()
})
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.title-area h2 { margin: 0; font-size: 20px; color: #1f2937; }
.subtitle { font-size: 12px; color: #94a3b8; }
.filter-card { margin-bottom: 16px; }
.filter-form { margin-bottom: -18px; }
.software-tabs :deep(.el-tabs__header) { margin-bottom: 12px; }
.tab-toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; }
.toolbar-tip { font-size: 12px; color: #94a3b8; }
.summary-cards { display: flex; gap: 14px; margin-bottom: 18px; }
.summary-card {
  flex: 1; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 10px;
  padding: 14px 18px; text-align: center;
}
.summary-card.summary-danger { background: #fef2f2; border-color: #fecaca; }
.summary-card.summary-warn { background: #fffbeb; border-color: #fde68a; }
.summary-value { font-size: 26px; font-weight: 700; color: #1e293b; }
.summary-card.summary-danger .summary-value { color: #dc2626; }
.summary-card.summary-warn .summary-value { color: #d97706; }
.summary-label { font-size: 12px; color: #64748b; margin-top: 2px; }
.section-title { font-size: 14px; color: #334155; margin: 18px 0 10px 0; }
.pagination-area { display: flex; justify-content: flex-end; margin-top: 16px; }
.sub-text { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.license-key { font-family: monospace; font-size: 12px; color: #475569; }
.empty-cell { color: #c0c4cc; }
.form-tip { font-size: 12px; color: #94a3b8; line-height: 1.4; margin-top: 4px; }
</style>
