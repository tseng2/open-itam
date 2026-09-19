<template>
  <div class="org-view">
    <div class="page-header">
      <div>
        <h2>组织与人员管理</h2>
        <span class="subtitle">集团子公司实体、部门架构及 AD/企微人员名单同步</span>
      </div>
      <el-button type="primary">
        <el-icon><Refresh /></el-icon> 立即从 AD 目录同步
      </el-button>
    </div>

    <el-row :gutter="16">
      <el-col :span="6">
        <el-card shadow="never" class="box-card">
          <template #header>
            <div class="card-title">组织架构树</div>
          </template>
          <el-tree :data="orgTree" :props="defaultProps" default-expand-all highlight-current />
        </el-card>
      </el-col>
      <el-col :span="18">
        <el-card shadow="never" class="box-card">
          <template #header>
            <div class="card-title">员工与名下挂载资产</div>
          </template>
          <el-table :data="users" stripe style="width: 100%">
            <el-table-column prop="real_name" label="姓名" width="120" />
            <el-table-column prop="username" label="AD账号" width="130" />
            <el-table-column prop="job_number" label="工号" width="110" />
            <el-table-column prop="company" label="所属公司" width="160" />
            <el-table-column prop="department" label="部门" width="140" />
            <el-table-column label="名下在用资产" min-width="180">
              <template #default="{ row }">
                <el-tag v-for="a in row.assets" :key="a" size="small" style="margin-right: 6px">
                  {{ a }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="status" label="在职状态" width="100">
              <template #default="{ row }">
                <el-tag :type="row.status === '在职' ? 'success' : 'info'" size="small">
                  {{ row.status }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const defaultProps = { children: 'children', label: 'label' }

const orgTree = ref([
  {
    label: '集团总部',
    children: [
      { label: '信息技术部 (IT)' },
      { label: '财务管理中心' },
      { label: '人力资源部' },
    ],
  },
  {
    label: '东莞智能制造基地',
    children: [
      { label: '生产运营部' },
      { label: '质量保证部' },
    ],
  },
  {
    label: '苏州研发中心',
    children: [
      { label: '软件研发一部' },
      { label: '硬件测试实验室' },
    ],
  },
])

const users = ref([
  { real_name: '张三', username: 'zhangsan', job_number: 'E00102', company: '集团总部', department: '信息技术部', assets: ['PC-202603001 (ThinkPad X1)', 'MON-2026-09'], status: '在职' },
  { real_name: '李四', username: 'lisi', job_number: 'E00109', company: '东莞智能制造基地', department: '生产运营部', assets: ['PC-202603088 (联想开天M)'], status: '在职' },
  { real_name: '王五', username: 'wangwu', job_number: 'E00215', company: '苏州研发中心', department: '软件研发一部', assets: ['PC-202603102 (MacBook Pro 16)'], status: '在职' },
])
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
.box-card {
  border-radius: 8px;
}
.card-title {
  font-weight: 600;
  color: #374151;
}
</style>
