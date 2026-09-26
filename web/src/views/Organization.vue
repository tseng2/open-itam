<template>
  <div class="org-view">
    <div class="page-header">
      <div>
        <h2>组织与人员管理</h2>
        <span class="subtitle">集团子公司（多租户）实增实删、人员总览；AD 目录同步属阶段二规划（当前为手工维护）</span>
      </div>
      <el-button v-if="isAdmin" type="primary" @click="openCreateDialog">
        <el-icon><Plus /></el-icon> 新增公司
      </el-button>
    </div>

    <el-row :gutter="16">
      <el-col :span="10">
        <el-card shadow="never" class="box-card">
          <template #header>
            <div class="card-title-row">
              <span class="card-title">公司（多租户）</span>
              <el-button link size="small" @click="fetchCompanies">刷新</el-button>
            </div>
          </template>
          <el-table :data="companies" v-loading="companyLoading" highlight-current-row stripe
            style="width: 100%" @current-change="onSelectCompany">
            <el-table-column label="公司名称" min-width="160">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <div class="sub-text">{{ row.domain || '未配置域' }}</div>
              </template>
            </el-table-column>
            <el-table-column prop="code" label="编码" width="110">
              <template #default="{ row }">{{ row.code || '—' }}</template>
            </el-table-column>
            <el-table-column v-if="isAdmin" label="操作" width="110" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click.stop="openEditDialog(row)">编辑</el-button>
                <el-button link type="danger" size="small" @click.stop="confirmDelete(row)">删除</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无公司，右上角新增第一家" />
            </template>
          </el-table>
        </el-card>
      </el-col>

      <el-col :span="14">
        <el-card shadow="never" class="box-card">
          <template #header>
            <div class="card-title-row">
              <span class="card-title">人员总览{{ selectedCompany ? `：${selectedCompany.name}` : '' }}</span>
              <el-input v-model="userKeyword" placeholder="姓名 / 账号" clearable style="width: 180px"
                @keyup.enter="searchUsers" @clear="searchUsers" />
            </div>
          </template>
          <el-table :data="users" v-loading="userLoading" stripe style="width: 100%">
            <el-table-column label="姓名" width="110">
              <template #default="{ row }">
                <strong>{{ row.real_name || row.username }}</strong>
              </template>
            </el-table-column>
            <el-table-column prop="username" label="账号" width="130" />
            <el-table-column prop="job_number" label="工号" width="100">
              <template #default="{ row }">{{ row.job_number || '—' }}</template>
            </el-table-column>
            <el-table-column label="角色" width="90">
              <template #default="{ row }">
                <el-tag :type="roleTag(row.role)" size="small">{{ roleText(row.role) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="email" label="邮箱" min-width="150">
              <template #default="{ row }">{{ row.email || '—' }}</template>
            </el-table-column>
            <el-table-column label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
                  {{ row.status === 'active' ? '在职' : '停用' }}
                </el-tag>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="emptyUsersText" />
            </template>
          </el-table>
          <div class="pagination-area">
            <el-pagination v-model:current-page="userPage" :page-size="userPageSize" :total="userTotal"
              layout="total, prev, pager, next" @current-change="fetchUsers" />
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新增 / 编辑公司对话框 -->
    <el-dialog v-model="showDialog" :title="editMode ? '编辑公司' : '新增公司'" width="480px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="90px">
        <el-form-item label="公司名称" prop="name">
          <el-input v-model="form.name" placeholder="如：东莞智能制造基地" />
        </el-form-item>
        <el-form-item label="公司编码" prop="code">
          <el-input v-model="form.code" placeholder="如：DGBASE" />
        </el-form-item>
        <el-form-item label="AD 域名">
          <el-input v-model="form.domain" placeholder="如：dg.example.com（阶段二 AD 同步用）" />
        </el-form-item>
      </el-form>
      <div v-if="editMode" class="dialog-tip">改名会同步影响全部引用该公司的页面展示（公司 ID 不变，历史数据零迁移）</div>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ editMode ? '保存' : '新增' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

// 组织与人员管理：公司（多租户）CRUD + 人员总览（真实数据，替代原 mock）。
// 公司写面服务端 RBAC 收口 admin（名称全局唯一 409、删除引用拦截 409 带
// 明细），人员只读（用户维护入口在「用户与权限」页）
const companies = ref([])
const companyLoading = ref(false)
const selectedCompany = ref(null)

const users = ref([])
const userTotal = ref(0)
const userPage = ref(1)
const userPageSize = ref(20)
const userKeyword = ref('')
const userLoading = ref(false)

const showDialog = ref(false)
const editMode = ref(false)
const editingId = ref(0)
const submitting = ref(false)
const formRef = ref(null)

const isAdmin = computed(() => {
  try {
    const u = JSON.parse(localStorage.getItem('itagent_user') || 'null')
    return !!u && (u.role === 'admin' || u.role === 'super_admin')
  } catch {
    return false
  }
})

const emptyUsersText = computed(() =>
  selectedCompany.value ? '该公司暂无人员，可在「用户与权限」页维护' : '左侧选择一家公司查看人员',
)

const emptyForm = () => ({ name: '', code: '', domain: '' })
const form = reactive(emptyForm())

const formRules = {
  name: [{ required: true, message: '请输入公司名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入公司编码', trigger: 'blur' }],
}

function roleText(role) {
  return { super_admin: '超管', admin: '管理员', user: '普通用户' }[role] || role
}

function roleTag(role) {
  return { super_admin: 'danger', admin: 'warning', user: 'info' }[role] || 'info'
}

async function fetchCompanies() {
  companyLoading.value = true
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch (err) {
    ElMessage.error(err.message || '加载公司失败')
  } finally {
    companyLoading.value = false
  }
}

function onSelectCompany(row) {
  selectedCompany.value = row
  userPage.value = 1
  fetchUsers()
}

function searchUsers() {
  userPage.value = 1
  fetchUsers()
}

async function fetchUsers() {
  if (!selectedCompany.value) {
    users.value = []
    userTotal.value = 0
    return
  }
  userLoading.value = true
  try {
    const params = new URLSearchParams()
    params.append('company_id', selectedCompany.value.id)
    if (userKeyword.value.trim()) params.append('keyword', userKeyword.value.trim())
    params.append('page', userPage.value)
    params.append('page_size', userPageSize.value)
    const res = await api(`/api/v1/users?${params}`)
    users.value = res.data?.items || []
    userTotal.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载人员失败')
  } finally {
    userLoading.value = false
  }
}

function openCreateDialog() {
  Object.assign(form, emptyForm())
  editMode.value = false
  showDialog.value = true
}

function openEditDialog(row) {
  Object.assign(form, emptyForm(), {
    name: row.name, code: row.code || '', domain: row.domain || '',
  })
  editingId.value = row.id
  editMode.value = true
  showDialog.value = true
}

async function submit() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      const payload = { name: form.name.trim(), code: form.code.trim(), domain: form.domain.trim() }
      if (editMode.value) {
        await api(`/api/v1/companies/${editingId.value}`, { method: 'PUT', body: JSON.stringify(payload) })
        ElMessage.success('公司已更新')
      } else {
        await api('/api/v1/companies', { method: 'POST', body: JSON.stringify(payload) })
        ElMessage.success('公司已新增')
      }
      showDialog.value = false
      fetchCompanies()
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
      `确定删除「${row.name}」？仍有业务数据（资产/人员等）引用时会被服务端拦截`,
      '删除公司', { type: 'warning' },
    )
  } catch { return }
  try {
    await api(`/api/v1/companies/${row.id}`, { method: 'DELETE' })
    ElMessage.success('已删除')
    if (selectedCompany.value?.id === row.id) {
      selectedCompany.value = null
      users.value = []
      userTotal.value = 0
    }
    fetchCompanies()
  } catch (err) {
    // 409 引用明细文案原样展示（服务端返回「资产 N、人员 N…」明细）
    ElMessage.error(err.message || '删除失败')
  }
}

onMounted(() => {
  fetchCompanies()
})
</script>

<style scoped>
.org-view { padding: 4px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.page-header h2 { margin: 0 0 4px 0; font-size: 20px; }
.subtitle { font-size: 13px; color: #6b7280; }
.box-card { border-radius: 8px; }
.card-title-row { display: flex; justify-content: space-between; align-items: center; }
.card-title { font-weight: 600; color: #374151; }
.sub-text { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.pagination-area { display: flex; justify-content: flex-end; margin-top: 12px; }
.dialog-tip { font-size: 12px; color: #d97706; background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; padding: 8px 12px; margin-top: 8px; }
</style>
