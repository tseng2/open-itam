<template>
  <div class="user-management">
    <div class="page-header">
      <div>
        <h2>用户与权限管理</h2>
        <span class="subtitle">管理系统登录账户、分配角色权限（超级管理员、管理员、普通用户）及状态管控</span>
      </div>
      <el-button type="primary" @click="openCreateDialog">
        <el-icon><Plus /></el-icon> 新增系统用户
      </el-button>
    </div>

    <!-- 筛选条件 -->
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="query" class="filter-form">
        <el-form-item label="角色">
          <el-select v-model="query.role" placeholder="全部角色" clearable style="width: 140px" @change="fetchUsers">
            <el-option label="超级管理员" value="super_admin" />
            <el-option label="管理员" value="admin" />
            <el-option label="普通用户" value="user" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="query.status" placeholder="全部状态" clearable style="width: 120px" @change="fetchUsers">
            <el-option label="正常" value="active" />
            <el-option label="禁用" value="disabled" />
          </el-select>
        </el-form-item>
        <el-form-item label="关键字">
          <el-input v-model="query.keyword" placeholder="搜索用户名 / 姓名 / 工号" clearable @keyup.enter="fetchUsers" style="width: 220px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchUsers">查询</el-button>
          <el-button @click="resetQuery">重置</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 用户表格 -->
    <el-card shadow="never" class="table-card">
      <el-table :data="userList" v-loading="loading" stripe style="width: 100%">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="username" label="登录用户名" width="140">
          <template #default="{ row }">
            <span class="username-cell">{{ row.username }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="real_name" label="真实姓名" width="130" />
        <el-table-column prop="job_number" label="工号" width="110">
          <template #default="{ row }">
            {{ row.job_number || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="role" label="权限角色" width="140">
          <template #default="{ row }">
            <el-tag :type="getRoleTagType(row.role)">
              {{ getRoleName(row.role) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="email" label="电子邮箱" min-width="180">
          <template #default="{ row }">
            {{ row.email || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="status" label="账户状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small">
              {{ row.status === 'active' ? '正常' : '已禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEditDialog(row)">编辑</el-button>
            <el-popconfirm title="确定要删除此用户吗？" @confirm="handleDelete(row.id)">
              <template #reference>
                <el-button link type="danger" size="small" :disabled="row.username === 'admin'">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination-container">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next"
          @size-change="fetchUsers"
          @current-change="fetchUsers"
        />
      </div>
    </el-card>

    <!-- 新增/编辑对话框 -->
    <el-dialog :title="isEdit ? '编辑系统用户' : '新增系统用户'" v-model="dialogVisible" width="500px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="form.username" :disabled="isEdit" placeholder="字母数字下划线" />
        </el-form-item>
        <el-form-item :label="isEdit ? '重置密码' : '登录密码'" :prop="isEdit ? '' : 'password'">
          <el-input v-model="form.password" type="password" :placeholder="isEdit ? '留空表示不修改密码' : '请输入初始密码'" show-password />
        </el-form-item>
        <el-form-item label="真实姓名" prop="real_name">
          <el-input v-model="form.real_name" placeholder="请输入姓名" />
        </el-form-item>
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="选择所属公司" style="width: 100%">
            <el-option v-for="c in companies" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="工号" prop="job_number">
          <el-input v-model="form.job_number" placeholder="员工编号（可选）" />
        </el-form-item>
        <el-form-item label="邮箱" prop="email">
          <el-input v-model="form.email" placeholder="example@company.com" />
        </el-form-item>
        <el-form-item label="系统角色" prop="role">
          <el-radio-group v-model="form.role">
            <el-radio-button label="super_admin">超级管理员</el-radio-button>
            <el-radio-button label="admin">管理员</el-radio-button>
            <el-radio-button label="user">普通用户</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="账户状态" prop="status">
          <el-switch v-model="form.status" active-value="active" inactive-value="disabled" active-text="正常" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../api'

const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const isEdit = ref(false)
const editId = ref(null)
const formRef = ref(null)

const userList = ref([])
const total = ref(0)
const companies = ref([])

const query = reactive({
  role: '',
  status: '',
  keyword: '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  company_id: 1,
  username: '',
  password: '',
  real_name: '',
  job_number: '',
  email: '',
  role: 'admin',
  status: 'active',
})

const rules = computed(() => ({
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: isEdit.value ? [] : [{ required: true, message: '请输入密码', trigger: 'blur' }],
  real_name: [{ required: true, message: '请输入姓名', trigger: 'blur' }],
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
}))

function getRoleName(role) {
  const map = {
    super_admin: '超级管理员',
    admin: '管理员',
    user: '普通用户',
  }
  return map[role] || '普通用户'
}

function getRoleTagType(role) {
  const map = {
    super_admin: 'danger',
    admin: 'primary',
    user: 'info',
  }
  return map[role] || 'info'
}

function formatTime(str) {
  if (!str) return '-'
  return new Date(str).toLocaleString('zh-CN', { hour12: false })
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
    if (companies.value.length > 0 && !form.company_id) {
      form.company_id = companies.value[0].id
    }
  } catch (e) {
    // ignore
  }
}

async function fetchUsers() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (query.role) params.append('role', query.role)
    if (query.status) params.append('status', query.status)
    if (query.keyword) params.append('keyword', query.keyword)
    params.append('page', query.page)
    params.append('page_size', query.page_size)

    const res = await api(`/api/v1/users?${params.toString()}`)
    userList.value = res.data.items || []
    total.value = res.data.total || 0
  } catch (err) {
    ElMessage.error(err.message || '获取用户列表失败')
  } finally {
    loading.value = false
  }
}

function resetQuery() {
  query.role = ''
  query.status = ''
  query.keyword = ''
  query.page = 1
  fetchUsers()
}

function openCreateDialog() {
  isEdit.value = false
  editId.value = null
  Object.assign(form, {
    company_id: companies.value[0]?.id || 1,
    username: '',
    password: '',
    real_name: '',
    job_number: '',
    email: '',
    role: 'admin',
    status: 'active',
  })
  dialogVisible.value = true
}

function openEditDialog(row) {
  isEdit.value = true
  editId.value = row.id
  Object.assign(form, {
    company_id: row.company_id,
    username: row.username,
    password: '',
    real_name: row.real_name,
    job_number: row.job_number,
    email: row.email,
    role: row.role || 'user',
    status: row.status || 'active',
  })
  dialogVisible.value = true
}

async function submitForm() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      if (isEdit.value) {
        await api(`/api/v1/users/${editId.value}`, {
          method: 'PUT',
          body: JSON.stringify(form),
        })
        ElMessage.success('用户更新成功')
      } else {
        await api('/api/v1/users', {
          method: 'POST',
          body: JSON.stringify(form),
        })
        ElMessage.success('用户创建成功')
      }
      dialogVisible.value = false
      fetchUsers()
    } catch (err) {
      ElMessage.error(err.message || '操作失败')
    } finally {
      submitting.value = false
    }
  })
}

async function handleDelete(id) {
  try {
    await api(`/api/v1/users/${id}`, { method: 'DELETE' })
    ElMessage.success('删除成功')
    fetchUsers()
  } catch (err) {
    ElMessage.error(err.message || '删除失败')
  }
}

onMounted(() => {
  fetchCompanies()
  fetchUsers()
})
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.page-header h2 {
  margin: 0 0 4px 0;
  font-size: 20px;
}
.subtitle {
  font-size: 13px;
  color: #6b7280;
}
.filter-card {
  margin-bottom: 16px;
  border-radius: 8px;
}
.table-card {
  border-radius: 8px;
}
.username-cell {
  font-weight: 600;
  color: #1e293b;
}
.pagination-container {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
