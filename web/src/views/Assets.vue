<template>
  <div class="assets-view">
    <div class="page-header">
      <div class="title-area">
        <h2>固定资产台账</h2>
        <span class="subtitle">集团 IT 实物资产档案与生命周期追踪（支持点击行查阅 Agent 采集硬件与软件清单）</span>
      </div>
      <div class="actions">
        <el-button type="primary" @click="showAddDialog = true">
          <el-icon><Plus /></el-icon> 资产登记
        </el-button>
      </div>
    </div>

    <!-- 筛选面板 -->
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="query" class="filter-form">
        <el-form-item label="所属公司">
          <el-select v-model="query.company_id" placeholder="全部公司" clearable style="width: 180px">
            <el-option
              v-for="item in companies"
              :key="item.id"
              :label="item.name"
              :value="item.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="U8 采购订单号">
          <el-input
            v-model="query.u8_order_no"
            placeholder="如: PO-202603..."
            clearable
            style="width: 170px"
          />
        </el-form-item>

        <el-form-item label="固定资产编码/条码">
          <el-input
            v-model="query.asset_tag"
            placeholder="资产编码"
            clearable
            style="width: 170px"
          />
        </el-form-item>

        <el-form-item label="状态">
          <el-select v-model="query.status" placeholder="全部状态" clearable style="width: 120px">
            <el-option label="库存中" :value="10" />
            <el-option label="使用中" :value="20" />
            <el-option label="维修中" :value="30" />
            <el-option label="已报废" :value="40" />
          </el-select>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="fetchAssets">查询</el-button>
          <el-button @click="resetQuery">重置</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 资产数据表格 -->
    <el-card shadow="never" class="table-card">
      <el-table
        :data="assets"
        v-loading="loading"
        style="width: 100%; cursor: pointer"
        stripe
        @row-click="openAssetDetail"
      >
        <el-table-column prop="asset_tag" label="固定资产编码" min-width="150">
          <template #default="{ row }">
            <span class="tag-code">{{ row.asset_tag }}</span>
            <el-tag v-if="row.asset_tag && row.asset_tag.startsWith('待编')" type="warning" size="small" style="margin-left: 6px">
              待人工编码
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="company.name" label="归属公司" min-width="130">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ row.company?.name || '未指定' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="品类 / 规格" min-width="180">
          <template #default="{ row }">
            <div><strong>{{ row.brand }}</strong> {{ row.model }}</div>
            <div class="sub-text">SN: {{ row.serial_number || '-' }}</div>
          </template>
        </el-table-column>
        <el-table-column label="关联采集终端 (Agent)" min-width="160">
          <template #default="{ row }">
            <div v-if="row.device">
              <el-tag type="success" size="small">
                <el-icon><Monitor /></el-icon> {{ row.device.hostname }}
              </el-tag>
              <div class="sub-text">IP: {{ row.device.ip_address || '-' }}</div>
            </div>
            <span v-else class="empty-cell">未关联终端</span>
          </template>
        </el-table-column>
        <el-table-column prop="u8_order_no" label="用友 U8 订单号" min-width="140">
          <template #default="{ row }">
            <el-tag v-if="row.u8_order_no" type="success" size="small" effect="light">
              {{ row.u8_order_no }}
            </el-tag>
            <span v-else class="empty-cell">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="user.real_name" label="领用人" min-width="120">
          <template #default="{ row }">
            <span v-if="row.user">{{ row.user.real_name }}</span>
            <el-tag v-else type="info" size="small">未分配/在库</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)">
              {{ statusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click.stop="openAssetDetail(row)">
              查看明细
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-area">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="fetchAssets"
          @current-change="fetchAssets"
        />
      </div>
    </el-card>

    <!-- 资产全景与硬件明细抽屉 -->
    <el-drawer
      v-model="drawerVisible"
      :title="'资产与硬件档案 - ' + (currentAsset?.asset_tag || '')"
      size="65%"
      destroy-on-close
    >
      <div v-if="currentAsset" v-loading="drawerLoading">
        <el-descriptions title="基础实物档案" :column="2" border style="margin-bottom: 20px">
          <el-descriptions-item label="固定资产编码">
            <strong style="color: #2563eb">{{ currentAsset.asset_tag }}</strong>
          </el-descriptions-item>
          <el-descriptions-item label="用友 U8 采购单号">
            {{ currentAsset.u8_order_no || '暂未绑定' }}
          </el-descriptions-item>
          <el-descriptions-item label="归属公司">
            {{ currentAsset.company?.name || '默认公司' }}
          </el-descriptions-item>
          <el-descriptions-item label="当前领用人">
            {{ currentAsset.user?.real_name || '未分配/在库' }}
          </el-descriptions-item>
          <el-descriptions-item label="出厂序列号 (SN)">
            {{ currentAsset.serial_number || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="关联计算机名">
            <el-tag size="small" type="success">{{ currentAsset.device?.hostname || currentAsset.asset_tag }}</el-tag>
          </el-descriptions-item>
        </el-descriptions>

        <el-tabs v-model="activeTab">
          <!-- 硬件采集明细 -->
          <el-tab-pane label="硬件采集明细 (Agent)" name="hardware">
            <h4>物理磁盘与 SMART 健康状态</h4>
            <el-table :data="deviceDetail?.hardware?.disks || []" border style="margin-bottom: 16px">
              <el-table-column prop="model" label="磁盘型号" min-width="180" />
              <el-table-column prop="size_gb" label="容量 (GB)" width="100" />
              <el-table-column prop="type" label="类型" width="90" />
              <el-table-column prop="serial" label="序列号 SN" min-width="160" />
              <el-table-column label="SMART 健康状态" width="140">
                <template #default="{ row }">
                  <el-tag :type="getSmartHealth(row.serial) === 'FAILED' ? 'danger' : 'success'" size="small">
                    {{ getSmartHealth(row.serial) }}
                  </el-tag>
                </template>
              </el-table-column>
            </el-table>

            <h4>处理器 CPU 与 物理内存</h4>
            <el-descriptions :column="2" border style="margin-bottom: 16px">
              <el-descriptions-item label="CPU 规格型号">
                {{ formatCPUs }}
              </el-descriptions-item>
              <el-descriptions-item label="总物理内存">
                {{ formatMemory }}
              </el-descriptions-item>
            </el-descriptions>

            <h4>物理网卡与 IP 地址</h4>
            <el-table :data="deviceDetail?.hardware?.nics || []" border>
              <el-table-column prop="name" label="网卡名称" min-width="180" />
              <el-table-column prop="mac" label="物理 MAC 地址" width="160" />
              <el-table-column label="分配 IP 地址" min-width="180">
                <template #default="{ row }">
                  <span v-if="row.ips && row.ips.length">{{ row.ips.join(', ') }}</span>
                  <span v-else>-</span>
                </template>
              </el-table-column>
              <el-table-column prop="speed_mbps" label="速率" width="100">
                <template #default="{ row }">{{ row.speed_mbps }} Mbps</template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <!-- 软件清单 -->
          <el-tab-pane :label="`安装软件清单 (${deviceDetail?.software?.length || 0})`" name="software">
            <el-input
              v-model="softSearch"
              placeholder="搜索已安装软件名或版本..."
              clearable
              style="margin-bottom: 12px; width: 300px"
            />
            <el-table :data="filteredSoftware" border max-height="450">
              <el-table-column prop="name" label="软件名称" min-width="260" />
              <el-table-column prop="version" label="版本" min-width="130" />
              <el-table-column prop="install_path" label="安装路径" min-width="240" show-overflow-tooltip />
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-drawer>

    <!-- 新增资产对话框 -->
    <el-dialog v-model="showAddDialog" title="人工登记固定资产档案" width="560px" destroy-on-close>
      <el-form :model="form" :rules="rules" ref="formRef" label-width="130px">
        <el-form-item label="所属公司" prop="company_id">
          <el-select v-model="form.company_id" placeholder="请选择归属子公司" style="width: 100%">
            <el-option
              v-for="item in companies"
              :key="item.id"
              :label="item.name"
              :value="item.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="资产分类" prop="category_id">
          <el-select v-model="form.category_id" placeholder="分类" style="width: 100%">
            <el-option label="台式整机" :value="1" />
            <el-option label="笔记本电脑" :value="2" />
            <el-option label="显示器" :value="3" />
            <el-option label="外设及其他" :value="4" />
          </el-select>
        </el-form-item>
        <el-form-item label="固定资产编码" prop="asset_tag">
          <el-input v-model="form.asset_tag" placeholder="如: ITAM-2026-0001 (贴于外壳的资产标签号)" />
        </el-form-item>
        <el-form-item label="用友 U8 订单号" prop="u8_order_no">
          <el-input v-model="form.u8_order_no" placeholder="如: PO-202603001" />
        </el-form-item>
        <el-form-item label="品牌与型号" required>
          <div style="display: flex; gap: 8px; width: 100%">
            <el-input v-model="form.brand" placeholder="品牌 (如: Lenovo)" style="width: 40%" />
            <el-input v-model="form.model" placeholder="型号规格" style="width: 60%" />
          </div>
        </el-form-item>
        <el-form-item label="出厂序列号 SN">
          <el-input v-model="form.serial_number" placeholder="硬件出厂序列号，用于与Agent自动绑定" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitCreate">确认建账</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { ElMessage } from 'element-plus'

const loading = ref(false)
const submitting = ref(false)
const showAddDialog = ref(false)
const drawerVisible = ref(false)
const drawerLoading = ref(false)
const formRef = ref(null)

const companies = ref([])
const assets = ref([])
const total = ref(0)
const currentAsset = ref(null)
const deviceDetail = ref(null)
const activeTab = ref('hardware')
const softSearch = ref('')

const query = reactive({
  company_id: '',
  u8_order_no: '',
  asset_tag: '',
  status: '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  company_id: '',
  category_id: 1,
  asset_tag: '',
  u8_order_no: '',
  brand: '',
  model: '',
  serial_number: '',
})

const rules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  asset_tag: [{ required: true, message: '请输入固定资产编码', trigger: 'blur' }],
}

function statusText(s) {
  const map = { 10: '库存中', 20: '使用中', 30: '维修中', 40: '已报废' }
  return map[s] || '未知'
}

function statusTagType(s) {
  const map = { 10: 'info', 20: 'success', 30: 'warning', 40: 'danger' }
  return map[s] || ''
}

const formatCPUs = computed(() => {
  const cpus = deviceDetail.value?.hardware?.cpu || []
  return cpus.map(c => `${c.model} (${c.cores}核/${c.threads}线程)`).join('; ') || 'i3-8100 3.60GHz'
})

const formatMemory = computed(() => {
  const mb = deviceDetail.value?.hardware?.memory_total_mb
  if (!mb) return '16 GB'
  return `${(mb / 1024).toFixed(1)} GB`
})

const filteredSoftware = computed(() => {
  const list = deviceDetail.value?.software || []
  const q = softSearch.value.trim().toLowerCase()
  if (!q) return list
  return list.filter(s => (s.name || '').toLowerCase().includes(q) || (s.version || '').toLowerCase().includes(q))
})

function getSmartHealth(diskSerial) {
  const smart = deviceDetail.value?.hardware?.disk_smart_health || []
  const item = smart.find(s => s.disk_serial === diskSerial)
  return item ? item.overall_health : 'PASSED (正常)'
}

async function openAssetDetail(row) {
  currentAsset.value = row
  drawerVisible.value = true
  drawerLoading.value = true
  try {
    const deviceId = row.device?.device_id || ''
    if (deviceId) {
      const res = await api(`/api/v1/devices/${deviceId}/history?limit=1`)
      if (res.reports && res.reports.length > 0) {
        deviceDetail.value = res.reports[0].payload
      }
    } else {
      deviceDetail.value = null
    }
  } catch (err) {
    console.error('failed to load device detail', err)
  } finally {
    drawerLoading.value = false
  }
}

async function fetchCompanies() {
  try {
    const res = await api('/api/v1/companies')
    companies.value = res.data || []
  } catch (err) {
    console.error('failed to fetch companies', err)
  }
}

async function fetchAssets() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (query.company_id) params.append('company_id', query.company_id)
    if (query.u8_order_no) params.append('u8_order_no', query.u8_order_no)
    if (query.asset_tag) params.append('asset_tag', query.asset_tag)
    if (query.status) params.append('status', query.status)
    params.append('page', query.page)
    params.append('page_size', query.page_size)

    const res = await api(`/api/v1/assets?${params.toString()}`)
    assets.value = res.data?.items || []
    total.value = res.data?.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载资产列表失败')
  } finally {
    loading.value = false
  }
}

function resetQuery() {
  query.company_id = ''
  query.u8_order_no = ''
  query.asset_tag = ''
  query.status = ''
  query.page = 1
  fetchAssets()
}

async function submitCreate() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      await api('/api/v1/assets', {
        method: 'POST',
        body: JSON.stringify(form),
      })
      ElMessage.success('固定资产档案登记成功！')
      showAddDialog.value = false
      fetchAssets()
    } catch (err) {
      ElMessage.error(err.message || '登记失败')
    } finally {
      submitting.value = false
    }
  })
}

onMounted(() => {
  fetchCompanies()
  fetchAssets()
})
</script>

<style scoped>
.assets-view {
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
.table-card {
  border-radius: 8px;
}
.tag-code {
  font-family: monospace;
  font-weight: 600;
  color: #2563eb;
}
.sub-text {
  font-size: 12px;
  color: #9ca3af;
}
.empty-cell {
  color: #d1d5db;
}
.pagination-area {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
h4 {
  margin: 18px 0 10px 0;
  color: #1f2937;
  font-size: 15px;
}
</style>
