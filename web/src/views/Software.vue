<template>
  <div class="software-view">
    <div class="page-header">
      <div>
        <h2>软件资产与授权管理</h2>
        <span class="subtitle">商业正版授权池、Agent 扫描上报、许可证超用与合规监控</span>
      </div>
      <el-button type="primary">
        <el-icon><Plus /></el-icon> 登记软件授权
      </el-button>
    </div>

    <el-card shadow="never" class="box-card">
      <el-table :data="softwares" stripe style="width: 100%">
        <el-table-column prop="name" label="软件名称" min-width="180">
          <template #default="{ row }">
            <strong>{{ row.name }}</strong>
            <div class="sub-text">{{ row.vendor }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="category" label="分类" width="130" />
        <el-table-column prop="total_licenses" label="购买总授权数" width="130" />
        <el-table-column prop="used_licenses" label="已安装使用数" width="130" />
        <el-table-column label="授权合规度" min-width="180">
          <template #default="{ row }">
            <el-progress
              :percentage="Math.min(100, Math.round((row.used_licenses / row.total_licenses) * 100))"
              :status="row.used_licenses > row.total_licenses ? 'exception' : 'success'"
            />
          </template>
        </el-table-column>
        <el-table-column prop="expire_date" label="授权到期日" width="140" />
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="row.used_licenses > row.total_licenses ? 'danger' : 'success'">
              {{ row.used_licenses > row.total_licenses ? '超用告警' : '合规' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const softwares = ref([
  { name: 'Microsoft 365 商业高级版', vendor: 'Microsoft', category: '办公套件', total_licenses: 500, used_licenses: 468, expire_date: '2027-12-31' },
  { name: 'AutoCAD 2024 机械专业版', vendor: 'Autodesk', category: '工程设计', total_licenses: 30, used_licenses: 34, expire_date: '2026-10-15' },
  { name: 'IntelliJ IDEA Ultimate', vendor: 'JetBrains', category: '开发工具', total_licenses: 60, used_licenses: 48, expire_date: '2027-05-20' },
  { name: 'Foxit PDF 商业编辑器', vendor: '福昕软件', category: '文档工具', total_licenses: 200, used_licenses: 120, expire_date: '永久授权' },
])
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.page-header h2 { margin: 0 0 4px 0; font-size: 20px; }
.subtitle { font-size: 13px; color: #6b7280; }
.box-card { border-radius: 8px; }
.sub-text { font-size: 12px; color: #9ca3af; }
</style>
