<template>
  <div class="dimensions-view">
    <div class="page-header">
      <div class="title-area">
        <h2>基础数据（维度治理）</h2>
        <span class="subtitle">厂商 / 型号库 / 供应商 / 位置库统一维护，台账表单下拉引用，消除自由文本口径混乱</span>
      </div>
    </div>

    <!-- 筛选面板：公司是所有维度数据的多租户根，未选公司时各 tab 呈引导空态 -->
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" class="filter-form">
        <el-form-item label="所属公司">
          <el-select
            v-model="query.company_id"
            placeholder="选择公司"
            clearable
            style="width: 200px"
            @change="handleCompanyChange"
          >
            <el-option v-for="item in companies" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="关键词">
          <!-- 关键词绑定在"当前 tab"的状态上：各 tab 独立记忆，来回切换不丢查询上下文 -->
          <el-input
            v-model="activeState.keyword"
            placeholder="名称等关键词，回车查询"
            clearable
            style="width: 220px"
            @keyup.enter="handleSearch"
            @clear="handleSearch"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <!-- 厂商 -->
      <el-tab-pane label="厂商" name="manufacturers">
        <div v-if="isAdmin" class="pane-toolbar">
          <el-button type="primary" :disabled="!query.company_id" @click="openDialog('manufacturers')">
            <el-icon><Plus /></el-icon> 新建厂商
          </el-button>
        </div>
        <el-card shadow="never" class="table-card">
          <el-table :data="tabs.manufacturers.items" v-loading="tabs.manufacturers.loading" stripe style="width: 100%">
            <el-table-column prop="name" label="名称" min-width="200">
              <template #default="{ row }"><strong>{{ row.name }}</strong></template>
            </el-table-column>
            <el-table-column label="备注" min-width="280">
              <template #default="{ row }"><span class="sub-text">{{ row.remark || '-' }}</span></template>
            </el-table-column>
            <el-table-column v-if="isAdmin" label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openDialog('manufacturers', row)">编辑</el-button>
                <el-button link type="danger" size="small" @click="confirmDelete('manufacturers', row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="emptyDescription('manufacturers')" />
            </template>
          </el-table>
          <div class="pagination-area">
            <el-pagination
              v-model:current-page="tabs.manufacturers.page"
              v-model:page-size="tabs.manufacturers.page_size"
              :total="tabs.manufacturers.total"
              :page-sizes="[10, 20, 50]"
              layout="total, sizes, prev, pager, next"
              @size-change="fetchTab('manufacturers')"
              @current-change="fetchTab('manufacturers')"
            />
          </div>
        </el-card>
      </el-tab-pane>

      <!-- 型号库 -->
      <el-tab-pane label="型号库" name="asset_models">
        <div v-if="isAdmin" class="pane-toolbar">
          <el-button type="primary" :disabled="!query.company_id" @click="openDialog('asset_models')">
            <el-icon><Plus /></el-icon> 新建型号
          </el-button>
        </div>
        <el-card shadow="never" class="table-card">
          <el-table :data="tabs.asset_models.items" v-loading="tabs.asset_models.loading" stripe style="width: 100%">
            <el-table-column prop="name" label="型号名称" min-width="180">
              <template #default="{ row }"><strong>{{ row.name }}</strong></template>
            </el-table-column>
            <el-table-column label="适用类别" width="110">
              <template #default="{ row }">{{ categoryText(row.category_id) }}</template>
            </el-table-column>
            <el-table-column label="厂商" min-width="140">
              <template #default="{ row }">
                <span :class="row.manufacturer_name ? '' : 'empty-cell'">{{ row.manufacturer_name || '-' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="预挂折旧规则" min-width="170">
              <template #default="{ row }">
                <span :class="row.depreciation_name ? '' : 'empty-cell'">{{ row.depreciation_name || '未预挂' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="EOL 寿命" width="110">
              <template #default="{ row }">{{ eolText(row.eol_months) }}</template>
            </el-table-column>
            <el-table-column v-if="isAdmin" label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openDialog('asset_models', row)">编辑</el-button>
                <el-button link type="danger" size="small" @click="confirmDelete('asset_models', row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="emptyDescription('asset_models')" />
            </template>
          </el-table>
          <div class="pagination-area">
            <el-pagination
              v-model:current-page="tabs.asset_models.page"
              v-model:page-size="tabs.asset_models.page_size"
              :total="tabs.asset_models.total"
              :page-sizes="[10, 20, 50]"
              layout="total, sizes, prev, pager, next"
              @size-change="fetchTab('asset_models')"
              @current-change="fetchTab('asset_models')"
            />
          </div>
        </el-card>
      </el-tab-pane>

      <!-- 供应商 -->
      <el-tab-pane label="供应商" name="suppliers">
        <div v-if="isAdmin" class="pane-toolbar">
          <el-button type="primary" :disabled="!query.company_id" @click="openDialog('suppliers')">
            <el-icon><Plus /></el-icon> 新建供应商
          </el-button>
        </div>
        <el-card shadow="never" class="table-card">
          <el-table :data="tabs.suppliers.items" v-loading="tabs.suppliers.loading" stripe style="width: 100%">
            <el-table-column prop="name" label="名称" min-width="180">
              <template #default="{ row }"><strong>{{ row.name }}</strong></template>
            </el-table-column>
            <el-table-column label="联系人" min-width="120">
              <template #default="{ row }">{{ row.contact_name || '-' }}</template>
            </el-table-column>
            <el-table-column label="电话" min-width="140">
              <template #default="{ row }">{{ row.phone || '-' }}</template>
            </el-table-column>
            <el-table-column label="备注" min-width="220">
              <template #default="{ row }"><span class="sub-text">{{ row.remark || '-' }}</span></template>
            </el-table-column>
            <el-table-column v-if="isAdmin" label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openDialog('suppliers', row)">编辑</el-button>
                <el-button link type="danger" size="small" @click="confirmDelete('suppliers', row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="emptyDescription('suppliers')" />
            </template>
          </el-table>
          <div class="pagination-area">
            <el-pagination
              v-model:current-page="tabs.suppliers.page"
              v-model:page-size="tabs.suppliers.page_size"
              :total="tabs.suppliers.total"
              :page-sizes="[10, 20, 50]"
              layout="total, sizes, prev, pager, next"
              @size-change="fetchTab('suppliers')"
              @current-change="fetchTab('suppliers')"
            />
          </div>
        </el-card>
      </el-tab-pane>

      <!-- 位置库 -->
      <el-tab-pane label="位置库" name="locations">
        <div v-if="isAdmin" class="pane-toolbar">
          <el-button type="primary" :disabled="!query.company_id" @click="openDialog('locations')">
            <el-icon><Plus /></el-icon> 新建位置
          </el-button>
        </div>
        <el-card shadow="never" class="table-card">
          <el-table :data="tabs.locations.items" v-loading="tabs.locations.loading" stripe style="width: 100%">
            <el-table-column prop="name" label="名称" min-width="200">
              <template #default="{ row }"><strong>{{ row.name }}</strong></template>
            </el-table-column>
            <el-table-column label="父位置" min-width="160">
              <template #default="{ row }">
                <span :class="row.parent_name ? '' : 'empty-cell'">{{ row.parent_name || '顶级' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="备注" min-width="220">
              <template #default="{ row }"><span class="sub-text">{{ row.remark || '-' }}</span></template>
            </el-table-column>
            <el-table-column v-if="isAdmin" label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openDialog('locations', row)">编辑</el-button>
                <el-button link type="danger" size="small" @click="confirmDelete('locations', row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="emptyDescription('locations')" />
            </template>
          </el-table>
          <div class="pagination-area">
            <el-pagination
              v-model:current-page="tabs.locations.page"
              v-model:page-size="tabs.locations.page_size"
              :total="tabs.locations.total"
              :page-sizes="[10, 20, 50]"
              layout="total, sizes, prev, pager, next"
              @size-change="fetchTab('locations')"
              @current-change="fetchTab('locations')"
            />
          </div>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- 厂商编辑对话框 -->
    <el-dialog
      v-model="dialogs.manufacturers.visible"
      :title="dialogs.manufacturers.editMode ? '编辑厂商' : '新建厂商'"
      width="520px"
      destroy-on-close
    >
      <el-form ref="manufacturerFormRef" :model="forms.manufacturers" :rules="formRules.manufacturers" label-width="90px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="forms.manufacturers.name" placeholder="如：联想 / 戴尔 / 华为" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="forms.manufacturers.remark" type="textarea" :rows="2" placeholder="官网、售后热线等补充说明" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.manufacturers.visible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="dialogs.manufacturers.submitting"
          @click="submitDialog('manufacturers')"
        >{{ dialogs.manufacturers.editMode ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <!-- 型号库编辑对话框 -->
    <el-dialog
      v-model="dialogs.asset_models.visible"
      :title="dialogs.asset_models.editMode ? '编辑型号' : '新建型号'"
      width="640px"
      destroy-on-close
    >
      <el-form ref="assetModelFormRef" :model="forms.asset_models" :rules="formRules.asset_models" label-width="110px">
        <el-form-item label="型号名称" prop="name">
          <el-input v-model="forms.asset_models.name" placeholder="如：ThinkPad T14 Gen 5" />
        </el-form-item>
        <el-form-item label="适用类别">
          <el-select v-model="forms.asset_models.category_id" style="width: 100%">
            <el-option v-for="opt in CATEGORY_OPTIONS" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="厂商">
          <el-select v-model="forms.asset_models.manufacturer_id" clearable placeholder="不挂厂商" style="width: 100%">
            <el-option v-for="item in refOptions.manufacturers" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="预挂折旧规则">
          <el-select v-model="forms.asset_models.depreciation_id" clearable placeholder="不预挂，台账可另行挂接" style="width: 100%">
            <el-option v-for="item in refOptions.depreciations" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="EOL 寿命">
          <el-input-number v-model="forms.asset_models.eol_months" :min="0" :step="12" style="width: 180px" />
          <span class="form-tip">从启用起算的服役月数，0 表示不限</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="forms.asset_models.remark" type="textarea" :rows="2" placeholder="标准配置、采购注意事项等" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.asset_models.visible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="dialogs.asset_models.submitting"
          @click="submitDialog('asset_models')"
        >{{ dialogs.asset_models.editMode ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <!-- 供应商编辑对话框 -->
    <el-dialog
      v-model="dialogs.suppliers.visible"
      :title="dialogs.suppliers.editMode ? '编辑供应商' : '新建供应商'"
      width="560px"
      destroy-on-close
    >
      <el-form ref="supplierFormRef" :model="forms.suppliers" :rules="formRules.suppliers" label-width="90px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="forms.suppliers.name" placeholder="供应商全称，如：XX 科技有限公司" />
        </el-form-item>
        <el-form-item label="联系人">
          <el-input v-model="forms.suppliers.contact_name" placeholder="商务对接人" />
        </el-form-item>
        <el-form-item label="电话">
          <el-input v-model="forms.suppliers.phone" placeholder="座机或手机号" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="forms.suppliers.remark" type="textarea" :rows="2" placeholder="结算方式、合作备注等" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.suppliers.visible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="dialogs.suppliers.submitting"
          @click="submitDialog('suppliers')"
        >{{ dialogs.suppliers.editMode ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <!-- 位置库编辑对话框 -->
    <el-dialog
      v-model="dialogs.locations.visible"
      :title="dialogs.locations.editMode ? '编辑位置' : '新建位置'"
      width="560px"
      destroy-on-close
    >
      <el-form ref="locationFormRef" :model="forms.locations" :rules="formRules.locations" label-width="90px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="forms.locations.name" placeholder="如：总部-3F-302 工位区" />
        </el-form-item>
        <el-form-item label="父位置">
          <el-select v-model="forms.locations.parent_id" clearable placeholder="不选为顶级位置" style="width: 100%">
            <el-option v-for="item in parentLocationOptions" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
          <span class="form-tip">只列当前公司位置；服务端会校验上下级关系并拒绝成环</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="forms.locations.remark" type="textarea" :rows="2" placeholder="房间号、责任人等说明" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogs.locations.visible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="dialogs.locations.submitting"
          @click="submitDialog('locations')"
        >{{ dialogs.locations.editMode ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

// 适用类别口径与台账枚举（1~4）保持一致，维度治理额外允许 0=不限
const CATEGORY_OPTIONS = [
  { value: 0, label: '不限' },
  { value: 1, label: '台式整机' },
  { value: 2, label: '笔记本电脑' },
  { value: 3, label: '显示器' },
  { value: 4, label: '外设及其他' },
]

// 四个维度的元信息（端点 / 中文称谓 / 删除提示）单一事实源：
// 列表、弹窗、提交、删除全链路复用，避免散落的魔法字符串
const TAB_META = {
  manufacturers: { endpoint: 'manufacturers', noun: '厂商', deleteHint: '已被型号库引用的厂商无法删除' },
  asset_models: { endpoint: 'asset-models', noun: '型号', deleteHint: '已被资产台账引用的型号无法删除' },
  suppliers: { endpoint: 'suppliers', noun: '供应商', deleteHint: '已被资产台账引用的供应商无法删除' },
  locations: { endpoint: 'locations', noun: '位置', deleteHint: '已被资产台账引用或存在子位置时无法删除' },
}

// 与 Assets.vue 同款判定：仅控制前端写操作按钮的展示，权限兜底在后端 RoleMiddleware
const isAdmin = computed(() => {
  try {
    const raw = localStorage.getItem('itagent_user')
    const role = raw ? JSON.parse(raw).role : ''
    return role === 'admin' || role === 'super_admin'
  } catch {
    return false
  }
})

const companies = ref([])
const query = reactive({ company_id: '' })
const activeTab = ref('manufacturers')

// 每个 tab 独立记忆列表 / 关键词 / 分页，来回切换不丢查询上下文
function makeTabState() {
  return { items: [], total: 0, keyword: '', page: 1, page_size: 20, loading: false }
}
const tabs = reactive({
  manufacturers: makeTabState(),
  asset_models: makeTabState(),
  suppliers: makeTabState(),
  locations: makeTabState(),
})
// 筛选面板的关键词输入绑定到"当前 tab"的状态上
const activeState = computed(() => tabs[activeTab.value])

// 弹窗内下拉的候选数据：型号库需要厂商 / 折旧规则，位置库需要父位置
const refOptions = reactive({
  manufacturers: [],
  depreciations: [],
  locations: [],
})

function makeDialogState() {
  return { visible: false, editMode: false, submitting: false }
}
const dialogs = reactive({
  manufacturers: makeDialogState(),
  asset_models: makeDialogState(),
  suppliers: makeDialogState(),
  locations: makeDialogState(),
})

// destroy-on-close 下四个表单实例互不干扰，但提交仍需按维度找到各自的 form
const manufacturerFormRef = ref(null)
const assetModelFormRef = ref(null)
const supplierFormRef = ref(null)
const locationFormRef = ref(null)

function getFormRef(kind) {
  return {
    manufacturers: manufacturerFormRef,
    asset_models: assetModelFormRef,
    suppliers: supplierFormRef,
    locations: locationFormRef,
  }[kind]
}

const formRules = {
  manufacturers: { name: [{ required: true, message: '请输入厂商名称', trigger: 'blur' }] },
  asset_models: { name: [{ required: true, message: '请输入型号名称', trigger: 'blur' }] },
  suppliers: { name: [{ required: true, message: '请输入供应商名称', trigger: 'blur' }] },
  locations: { name: [{ required: true, message: '请输入位置名称', trigger: 'blur' }] },
}

// 表单值工厂：row 为空给"新建"默认值，否则回填编辑行；
// 外键 0/null 统一归一为 null，与可清空下拉的清空语义保持一致
function makeForm(kind, row) {
  const base = { id: row?.id || 0, name: row?.name || '', remark: row?.remark || '' }
  if (kind === 'asset_models') {
    return {
      ...base,
      category_id: row?.category_id ?? 0,
      manufacturer_id: row?.manufacturer_id || null,
      depreciation_id: row?.depreciation_id || null,
      eol_months: row?.eol_months ?? 0,
    }
  }
  if (kind === 'suppliers') return { ...base, contact_name: row?.contact_name || '', phone: row?.phone || '' }
  if (kind === 'locations') return { ...base, parent_id: row?.parent_id || null }
  return base
}

const forms = reactive({
  manufacturers: makeForm('manufacturers'),
  asset_models: makeForm('asset_models'),
  suppliers: makeForm('suppliers'),
  locations: makeForm('locations'),
})

// 父位置候选排除"正在编辑的这一行"：指向自身必然成环，前端先挡一道（服务端仍兜底校验环）
const parentLocationOptions = computed(() =>
  refOptions.locations.filter(item => item.id !== forms.locations.id)
)

// ---- 展示辅助 ----
function categoryText(id) {
  const hit = CATEGORY_OPTIONS.find(opt => opt.value === id)
  return hit ? hit.label : '不限'
}

function eolText(months) {
  return months ? `${months} 个月` : '不限'
}

// 空态区分"未选公司"与"选了公司但无数据"，引导下一步操作
function emptyDescription(kind) {
  if (!query.company_id) return '请先选择所属公司查看'
  return `暂无${TAB_META[kind].noun}数据`
}

// ---- 数据读取 ----
async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch (err) {
    console.error('failed to fetch companies', err)
  }
}

async function fetchTab(kind) {
  const st = tabs[kind]
  if (!query.company_id) {
    st.items = []
    st.total = 0
    return
  }
  st.loading = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', query.company_id)
    const kw = st.keyword.trim()
    if (kw) params.append('keyword', kw)
    params.append('page', st.page)
    params.append('page_size', st.page_size)
    const res = await api(`/api/v1/${TAB_META[kind].endpoint}?${params.toString()}`)
    st.items = res.data?.items || []
    st.total = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || `加载${TAB_META[kind].noun}列表失败`)
  } finally {
    st.loading = false
  }
}

// 型号库弹窗的下拉候选（厂商 / 折旧规则）：进入型号库 tab 或切换公司时预取；
// 维度数据量天然有限，page_size=200 一页拉全
async function fetchModelRefOptions() {
  if (!query.company_id) {
    refOptions.manufacturers = []
    refOptions.depreciations = []
    return
  }
  const common = `company_id=${query.company_id}&page_size=200`
  try {
    const res = await api(`/api/v1/manufacturers?${common}`)
    refOptions.manufacturers = res.data?.items || []
  } catch (err) {
    console.error('failed to fetch manufacturer options', err)
  }
  try {
    const res = await api(`/api/v1/depreciations?${common}`)
    refOptions.depreciations = res.data?.items || []
  } catch (err) {
    console.error('failed to fetch depreciation options', err)
  }
}

// 位置库弹窗打开时才拉父位置候选：保证排除自身后仍是当前公司的最新全集
async function fetchLocationRefOptions() {
  if (!query.company_id) {
    refOptions.locations = []
    return
  }
  try {
    const res = await api(`/api/v1/locations?company_id=${query.company_id}&page_size=200`)
    refOptions.locations = res.data?.items || []
  } catch (err) {
    console.error('failed to fetch location options', err)
  }
}

// ---- 查询联动 ----
function handleSearch() {
  activeState.value.page = 1
  fetchTab(activeTab.value)
}

function handleCompanyChange() {
  // 公司是维度数据的多租户根：切换后各 tab 分页复位，当前 tab 立即刷新
  Object.keys(tabs).forEach(kind => { tabs[kind].page = 1 })
  fetchTab(activeTab.value)
  if (activeTab.value === 'asset_models') fetchModelRefOptions()
}

function handleTabChange(kind) {
  // 切 tab 即刷新：离开再回来时看到的始终是当前公司的最新口径
  fetchTab(kind)
  if (kind === 'asset_models') fetchModelRefOptions()
}

// ---- 新建 / 编辑 ----
function openDialog(kind, row) {
  Object.assign(forms[kind], makeForm(kind, row))
  const dlg = dialogs[kind]
  dlg.editMode = !!row
  dlg.visible = true
  if (kind === 'locations') fetchLocationRefOptions()
}

// 提交体组装：company_id / name / remark 为公共字段；
// 可清空外键统一落 null（清空下拉 = 解除关联），EOL 清空视为 0=不限
function buildPayload(kind) {
  const form = forms[kind]
  const payload = {
    company_id: query.company_id,
    name: form.name.trim(),
    remark: form.remark || '',
  }
  if (kind === 'asset_models') {
    payload.category_id = form.category_id
    payload.manufacturer_id = form.manufacturer_id || null
    payload.depreciation_id = form.depreciation_id || null
    payload.eol_months = form.eol_months || 0
  } else if (kind === 'suppliers') {
    payload.contact_name = form.contact_name || ''
    payload.phone = form.phone || ''
  } else if (kind === 'locations') {
    payload.parent_id = form.parent_id || null
  }
  return payload
}

async function submitDialog(kind) {
  const formRef = getFormRef(kind)
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    const dlg = dialogs[kind]
    dlg.submitting = true
    try {
      const { endpoint, noun } = TAB_META[kind]
      const payload = buildPayload(kind)
      if (dlg.editMode) {
        await api(`/api/v1/${endpoint}/${forms[kind].id}`, { method: 'PUT', body: JSON.stringify(payload) })
        ElMessage.success(`${noun}已保存`)
      } else {
        await api(`/api/v1/${endpoint}`, { method: 'POST', body: JSON.stringify(payload) })
        ElMessage.success(`${noun}创建成功`)
      }
      dlg.visible = false
      fetchTab(kind)
    } catch (err) {
      // 409 同名冲突 / 400 父位置成环等：服务端 message 已可直读，原样透出
      ElMessage.error(err.message || '保存失败')
    } finally {
      dlg.submitting = false
    }
  })
}

// ---- 删除 ----
async function confirmDelete(kind, row) {
  const { endpoint, noun, deleteHint } = TAB_META[kind]
  try {
    await ElMessageBox.confirm(
      `删除${noun}「${row.name}」？${deleteHint}。`,
      '删除确认',
      { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    // DELETE 按契约携带 query company_id，由服务端做公司边界校验
    await api(`/api/v1/${endpoint}/${row.id}?company_id=${row.company_id}`, { method: 'DELETE' })
    ElMessage.success(`${noun}已删除`)
    fetchTab(kind)
  } catch (err) {
    // 409 时服务端 message 自带引用计数文案，原样展示给管理员
    ElMessage.error(err.message || `删除${noun}失败`)
  }
}

onMounted(() => {
  fetchCompanies()
  fetchTab(activeTab.value)
})
</script>

<style scoped>
.dimensions-view {
  padding: 4px;
}
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.title-area h2 {
  margin: 0 0 4px 0;
  font-size: 20px;
  color: #1f2937;
}
.subtitle {
  font-size: 13px;
  color: #6b7280;
}
.filter-card {
  margin-bottom: 16px;
  border-radius: 8px;
}
.filter-form {
  margin-bottom: -18px;
}
.pane-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}
.table-card {
  border-radius: 8px;
}
.sub-text {
  font-size: 12px;
  color: #9ca3af;
}
.empty-cell {
  color: #d1d5db;
}
.form-tip {
  margin-left: 10px;
  font-size: 12px;
  color: #9ca3af;
}
.pagination-area {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
