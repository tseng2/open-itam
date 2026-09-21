<template>
  <div class="assets-view">
    <div class="page-header">
      <div class="title-area">
        <h2>固定资产台账</h2>
        <span class="subtitle">集团 IT 实物资产档案与生命周期追踪（支持点击行查阅 Agent 采集硬件与软件清单）</span>
      </div>
      <div class="actions">
        <el-button type="primary" @click="openCreateDialog">
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

        <el-form-item label="固定资产编码/条码">
          <el-input
            v-model="query.asset_tag"
            placeholder="资产编码"
            clearable
            style="width: 170px"
          />
        </el-form-item>

        <el-form-item label="全文搜索">
          <el-input
            v-model="query.keyword"
            placeholder="MAC / IP / SN / 使用人 / 型号 / 位置等"
            clearable
            style="width: 260px"
            @keyup.enter="fetchAssets"
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
        <el-table-column label="品类 / 规格" min-width="200">
          <template #default="{ row }">
            <div><strong>{{ row.brand }}</strong> {{ row.model }}</div>
            <div class="sub-text">SN: {{ row.serial_number || '-' }}</div>
            <div class="sub-text" v-if="row.cpu_name || row.memory_size">
              {{ [row.cpu_name, row.memory_size].filter(Boolean).join(' / ') }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="关联采集终端 (Agent)" min-width="160">
          <template #default="{ row }">
            <div v-if="row.device">
              <el-tag type="success" size="small">
                <el-icon><Monitor /></el-icon> {{ row.device.hostname }}
              </el-tag>
              <div class="sub-text">内网IP: {{ row.device.ip_address || '-' }}</div>
              <div class="sub-text" v-if="row.device.public_ip">公网IP: {{ row.device.public_ip }}</div>
              <div class="sub-text mono">ID: {{ row.device.device_id }}</div>
            </div>
            <span v-else class="empty-cell">未关联终端</span>
          </template>
        </el-table-column>
        <el-table-column prop="user.real_name" label="领用人" min-width="120">
          <template #default="{ row }">
            <span v-if="row.user">{{ row.user.real_name }}</span>
            <el-tag v-else type="info" size="small">未分配/在库</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="current_version" label="基线版本" width="90" align="center">
          <template #default="{ row }">
            <el-tag type="info" effect="plain" size="small">v{{ row.current_version || 1 }}</el-tag>
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
        <div style="display: flex; justify-content: flex-end; gap: 8px; margin-bottom: 12px">
          <el-button type="primary" plain size="small" @click="openEditDialog">
            <el-icon><Edit /></el-icon> 编辑台账
          </el-button>
          <el-button type="warning" plain size="small" @click="openMergeDialog">合并资产</el-button>
          <el-button type="danger" plain size="small" @click="confirmDelete">删除</el-button>
        </div>

        <el-alert
          v-if="pendingReviewEvent"
          title="发现未审核的硬件配置变更！"
          type="warning"
          show-icon
          :description="pendingReviewEvent.description"
          style="margin-bottom: 16px"
          :closable="false"
        >
          <template #default>
            <div style="margin-top: 8px">
              <el-button type="primary" size="small" @click="openReviewDialog(pendingReviewEvent)">立即审核</el-button>
            </div>
          </template>
        </el-alert>

        <el-descriptions title="基础实物档案" :column="2" border style="margin-bottom: 20px">
          <el-descriptions-item label="固定资产编码">
            <strong style="color: #2563eb">{{ currentAsset.asset_tag }}</strong>
          </el-descriptions-item>
          <el-descriptions-item label="用友 U8 采购单号">
            <el-tag v-if="currentAsset.u8_order_no" type="success" size="small">{{ currentAsset.u8_order_no }}</el-tag>
            <span v-else class="empty-cell">未绑定U8采购单</span>
          </el-descriptions-item>
          <el-descriptions-item label="归属公司 / 中心">
            {{ currentAsset.company?.name || '默认公司' }} <span v-if="currentAsset.center_name">/ {{ currentAsset.center_name }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="部门">
            {{ currentAsset.department_name || '-' }}<span v-if="currentAsset.department_sub">（{{ currentAsset.department_sub }}）</span>
          </el-descriptions-item>
          <el-descriptions-item label="主要存放位置或用途">
            {{ currentAsset.location || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="资产负责人">
            {{ currentAsset.manager_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="当前领用人">
            {{ currentAsset.user?.real_name || '未分配/在库' }}
          </el-descriptions-item>
          <el-descriptions-item label="品牌与型号规格">
            <strong>{{ currentAsset.brand }}</strong> {{ currentAsset.model }}
          </el-descriptions-item>
          <el-descriptions-item label="出厂序列号 (SN)">
            {{ currentAsset.serial_number || '-' }}
            <el-tooltip
              v-if="isPseudoSN"
              content="组装机主板未写入真实出厂序列号（垃圾值已过滤），系统以终端指纹暂代 SN"
              placement="top"
            >
              <el-tag type="warning" size="small" style="margin-left: 6px">终端指纹暂代</el-tag>
            </el-tooltip>
          </el-descriptions-item>
          <el-descriptions-item label="终端ID（系统指纹）">
            <span class="mono">{{ currentAsset.device?.device_id || '-' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="关联计算机名">
            <el-tag size="small" type="success">{{ currentAsset.device?.hostname || currentAsset.asset_tag }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="购入时间">
            {{ formatDate(currentAsset.purchase_date) || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="已使用月数">
            {{ usedMonths(currentAsset.purchase_date) }}
          </el-descriptions-item>
          <el-descriptions-item label="验收人">
            {{ currentAsset.acceptor || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="保修期">
            {{ currentAsset.warranty_period || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="原值（不含税）">
            {{ currentAsset.original_price ? '￥' + currentAsset.original_price.toFixed(2) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="净值">
            {{ currentAsset.net_value ? '￥' + currentAsset.net_value.toFixed(2) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="加密软件管理">
            <el-tag :type="currentAsset.sec_encrypted ? 'success' : 'info'" size="small">
              {{ currentAsset.sec_encrypted ? '已纳管（绿盾）' : '否' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="备注">
            {{ currentAsset.remark || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <el-descriptions title="账面硬件规格（台账维护值，Agent 采集值见硬件明细）" :column="2" border style="margin-bottom: 20px">
          <el-descriptions-item label="CPU 名称">{{ currentAsset.cpu_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="内存">{{ currentAsset.memory_size || '-' }}</el-descriptions-item>
          <el-descriptions-item label="主硬盘">{{ currentAsset.main_disk || '-' }}</el-descriptions-item>
          <el-descriptions-item label="从硬盘">{{ currentAsset.secondary_disk || '-' }}</el-descriptions-item>
          <el-descriptions-item label="显卡名称">{{ currentAsset.gpu_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="网卡 MAC 地址">{{ currentAsset.mac_address || '-' }}</el-descriptions-item>
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

          <!-- 资产履历 -->
          <el-tab-pane label="资产履历 (Timeline)" name="events">
            <el-timeline style="margin-top: 16px; padding-left: 10px;">
              <el-timeline-item
                v-for="evt in assetEvents"
                :key="evt.id"
                :timestamp="formatTime(evt.created_at)"
                :type="evt.event_type === 'hardware_change' ? 'warning' : 'primary'"
                placement="top"
              >
                <el-card shadow="hover">
                  <h4>{{ evt.title }} <el-tag size="small" v-if="evt.review_status === 20" type="danger">待审核</el-tag></h4>
                  <p style="white-space: pre-wrap; font-size: 13px; color: #666; margin: 8px 0;">{{ evt.description }}</p>
                  <div style="margin-top: 8px; font-size: 12px; color: #999" v-if="evt.oa_number || evt.cost">
                    <span v-if="evt.oa_number" style="margin-right: 16px"><el-icon><Document /></el-icon> OA单号: {{ evt.oa_number }}</span>
                    <span v-if="evt.cost"><el-icon><Money /></el-icon> 产生金额: ￥{{ evt.cost.toFixed(2) }}</span>
                  </div>
                  <div style="margin-top: 8px; font-size: 12px; color: #999" v-if="evt.operator">
                    操作人: {{ evt.operator.real_name }}
                  </div>
                </el-card>
              </el-timeline-item>
              <el-empty v-if="assetEvents.length === 0" description="暂无履历记录" />
            </el-timeline>
          </el-tab-pane>

          <!-- 外寄维修记录 -->
          <el-tab-pane :label="`外寄维修记录 (${assetRepairs.length})`" name="repairs">
            <div style="margin: 12px 0">
              <el-button type="primary" size="small" @click="openRepairDialog">
                <el-icon><Plus /></el-icon> 登记送修
              </el-button>
            </div>
            <el-table :data="assetRepairs" border>
              <el-table-column label="寄修日期" width="110">
                <template #default="{ row }">{{ formatDate(row.send_date) || '-' }}</template>
              </el-table-column>
              <el-table-column prop="vendor" label="维修厂商" width="100" />
              <el-table-column prop="user_name" label="使用人" width="110" />
              <el-table-column prop="fault_reason" label="故障原因" min-width="180" show-overflow-tooltip />
              <el-table-column prop="oa_number" label="OA单号" width="110">
                <template #default="{ row }">{{ row.oa_number || '-' }}</template>
              </el-table-column>
              <el-table-column label="状态" width="90" align="center">
                <template #default="{ row }">
                  <el-tag :type="repairStatusTag(row.status)" size="small">{{ repairStatusText(row.status) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="寄回日期" width="110">
                <template #default="{ row }">{{ formatDate(row.return_date) || '-' }}</template>
              </el-table-column>
              <el-table-column prop="result" label="维修结果" min-width="180" show-overflow-tooltip />
              <el-table-column label="操作" width="110" fixed="right">
                <template #default="{ row }">
                  <el-button
                    v-if="row.status === 'repairing'"
                    link type="primary" size="small"
                    @click="openReturnDialog(row)"
                  >登记寄回</el-button>
                </template>
              </el-table-column>
              <template #empty><el-empty description="暂无外寄维修记录" /></template>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-drawer>

    <!-- 登记 / 编辑资产对话框 -->
    <el-dialog
      v-model="showAddDialog"
      :title="editMode ? '编辑固定资产台账' : '人工登记固定资产档案'"
      width="780px"
      destroy-on-close
    >
      <el-form :model="form" :rules="rules" ref="formRef" label-width="120px">
        <el-divider content-position="left">基本档案</el-divider>
        <el-row :gutter="16">
          <el-col :span="12">
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
          </el-col>
          <el-col :span="12">
            <el-form-item label="资产分类" prop="category_id">
              <el-select v-model="form.category_id" placeholder="分类" style="width: 100%">
                <el-option label="台式整机" :value="1" />
                <el-option label="笔记本电脑" :value="2" />
                <el-option label="显示器" :value="3" />
                <el-option label="外设及其他" :value="4" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="固定资产编码" prop="asset_tag">
              <el-input v-model="form.asset_tag" placeholder="如: SSA-G-202506-DJ0085" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="使用状态">
              <el-select v-model="form.status" style="width: 100%">
                <el-option label="库存中" :value="10" />
                <el-option label="使用中" :value="20" />
                <el-option label="维修中" :value="30" />
                <el-option label="已报废" :value="40" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="品牌">
              <el-input v-model="form.brand" placeholder="如: 联想 / DELL / 苹果" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="型号规格">
              <el-input v-model="form.model" placeholder="如: ThinkPad E490" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="出厂序列号 SN">
              <el-input v-model="form.serial_number" placeholder="硬件出厂序列号，用于与Agent自动绑定" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-divider content-position="left">归属与位置</el-divider>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="中心">
              <el-input v-model="form.center_name" placeholder="如: 项目中心 / 研发中心" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="部门">
              <el-input v-model="form.department_name" placeholder="如: 华东部" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="部门（完整路径）">
              <el-input v-model="form.department_sub" placeholder="如: 公司/华东部/项目三课" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="存放位置或用途">
              <el-input v-model="form.location" placeholder="主要存放位置或用途" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="资产负责人">
              <el-input v-model="form.manager_name" placeholder="资产负责人姓名" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-divider content-position="left">账面硬件规格（无 Agent 终端手工维护）</el-divider>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="CPU 名称">
              <el-input v-model="form.cpu_name" placeholder="如: i5-8250U" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="内存">
              <el-input v-model="form.memory_size" placeholder="如: 16G" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="主硬盘">
              <el-input v-model="form.main_disk" placeholder="如: 512G SSD" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="从硬盘">
              <el-input v-model="form.secondary_disk" placeholder="如: 1TB" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="显卡名称">
              <el-input v-model="form.gpu_name" placeholder="如: RTX 3060" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="网卡 MAC">
              <el-input v-model="form.mac_address" placeholder="如: 3C:52:82:xx:xx:xx" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-divider content-position="left">采购与财务</el-divider>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="购入时间">
              <el-date-picker
                v-model="form.purchase_date"
                type="date"
                value-format="YYYY-MM-DD"
                placeholder="选择购入日期"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="验收人">
              <el-input v-model="form.acceptor" placeholder="验收人姓名" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="保修期">
              <el-input v-model="form.warranty_period" placeholder="如: 3年 / 2027-06-25" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="用友 U8 订单号">
              <el-input v-model="form.u8_order_no" placeholder="如: PO-202603001（仅入台账明细）" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="原值（不含税）">
              <el-input-number v-model="form.original_price" :min="0" :precision="2" :step="100" style="width: 100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="净值">
              <el-input-number v-model="form.net_value" :min="0" :precision="2" :step="100" style="width: 100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="加密软件管理">
              <el-switch v-model="form.sec_encrypted" active-text="已纳管（绿盾）" inactive-text="否" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" placeholder="其他需要说明的信息" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitCreate">
          {{ editMode ? '保存台账' : '确认建账' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 外寄维修送修登记对话框 -->
    <el-dialog v-model="showRepairDialog" title="外寄维修送修登记" width="620px" destroy-on-close>
      <el-form :model="repairForm" label-width="110px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="OA 申请单号">
              <el-input v-model="repairForm.oa_number" placeholder="如: PUBLIC0939" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="使用人">
              <el-input v-model="repairForm.user_name" placeholder="送修时使用人，如: IT闲置" />
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item label="故障原因">
              <el-input v-model="repairForm.fault_reason" type="textarea" :rows="2" placeholder="用户反馈的故障现象" />
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item label="IT诊断结果">
              <el-input v-model="repairForm.diagnosis" type="textarea" :rows="2" placeholder="IT 检测结论" />
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item label="IT维修建议">
              <el-input v-model="repairForm.suggestion" type="textarea" :rows="2" placeholder="维修或处置建议" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="维修厂商">
              <el-input v-model="repairForm.vendor" placeholder="如: 太鲁格" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="寄修日期">
              <el-date-picker
                v-model="repairForm.send_date"
                type="date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="联系人">
              <el-input v-model="repairForm.contact_name" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="联系电话">
              <el-input v-model="repairForm.contact_phone" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="维修金额(元)">
              <el-input-number v-model="repairForm.cost" :min="0" :precision="2" :step="100" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="showRepairDialog = false">取消</el-button>
        <el-button type="primary" :loading="repairSubmitting" @click="submitRepair">确认送修</el-button>
      </template>
    </el-dialog>

    <!-- 维修寄回登记对话框 -->
    <el-dialog v-model="showReturnDialog" title="维修寄回登记" width="480px" destroy-on-close>
      <el-form :model="returnForm" label-width="100px">
        <el-form-item label="寄回日期">
          <el-date-picker
            v-model="returnForm.return_date"
            type="date"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="维修结果">
          <el-input v-model="returnForm.result" type="textarea" :rows="3" placeholder="如: 已维修，需要确认是否还有问题" />
        </el-form-item>
        <el-form-item label="维修金额(元)">
          <el-input-number v-model="returnForm.cost" :min="0" :precision="2" :step="100" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showReturnDialog = false">取消</el-button>
        <el-button type="primary" :loading="returnSubmitting" @click="submitReturn">确认寄回</el-button>
      </template>
    </el-dialog>

    <!-- 合并资产对话框 -->
    <el-dialog v-model="showMergeDialog" title="合并重复资产" width="520px" destroy-on-close>
      <el-alert type="warning" show-icon :closable="false" style="margin-bottom: 12px">
        <template #title>
          当前资产 <strong>{{ currentAsset?.asset_tag }}</strong> 的数据（终端绑定/履历/维修/基线）将并入目标资产，
          当前记录随后被删除，操作不可撤销
        </template>
      </el-alert>
      <el-select
        v-model="mergeTargetId"
        filterable
        remote
        :remote-method="searchMergeTargets"
        :loading="mergeSearching"
        placeholder="输入关键字搜索目标资产（编码/SN/主机名）"
        style="width: 100%"
      >
        <el-option
          v-for="item in mergeCandidates"
          :key="item.id"
          :label="`${item.asset_tag} ｜ ${item.brand || ''} ${item.model || ''} ｜ ${item.user?.real_name || '在库'}`"
          :value="item.id"
        />
      </el-select>
      <template #footer>
        <el-button @click="showMergeDialog = false">取消</el-button>
        <el-button type="warning" :disabled="!mergeTargetId" :loading="merging" @click="submitMerge">
          确认合并
        </el-button>
      </template>
    </el-dialog>

    <!-- 硬件变更审核对话框 -->
    <el-dialog v-model="showReviewDialog" title="硬件变更审核" width="500px" destroy-on-close>
      <el-form :model="reviewForm" label-width="100px">
        <el-form-item label="OA 申请单号">
          <el-input v-model="reviewForm.oa_number" placeholder="如有对应OA单，请填写" />
        </el-form-item>
        <el-form-item label="产生金额(元)">
          <el-input-number v-model="reviewForm.cost" :min="0" :precision="2" :step="100" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="showReviewDialog = false">取消</el-button>
          <el-button type="primary" :loading="reviewSubmitting" @click="submitReview">
            通过并更新基线
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Monitor, Plus, Document, Money, Edit } from '@element-plus/icons-vue'
import { api } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'

const loading = ref(false)
const submitting = ref(false)
const showAddDialog = ref(false)
const editMode = ref(false)
const drawerVisible = ref(false)
const drawerLoading = ref(false)
const formRef = ref(null)

const companies = ref([])
const assets = ref([])
const total = ref(0)
const currentAsset = ref(null)
const deviceDetail = ref(null)
const assetEvents = ref([])
const assetRepairs = ref([])
const activeTab = ref('hardware')
const softSearch = ref('')

const showRepairDialog = ref(false)
const repairSubmitting = ref(false)
const showReturnDialog = ref(false)
const returnSubmitting = ref(false)
const showMergeDialog = ref(false)
const merging = ref(false)
const mergeSearching = ref(false)
const mergeTargetId = ref(null)
const mergeCandidates = ref([])

const pendingReviewEvent = computed(() => {
  return assetEvents.value.find(e => e.review_status === 20 && e.event_type === 'hardware_change')
})

// 组装机无真实出厂 SN 时，服务端以终端指纹（device_id）暂代，页面上给出区分提示
const isPseudoSN = computed(() => {
  const sn = currentAsset.value?.serial_number
  const did = currentAsset.value?.device?.device_id
  return !!sn && !!did && sn === did
})

const showReviewDialog = ref(false)
const reviewSubmitting = ref(false)
const reviewForm = reactive({
  event_id: 0,
  oa_number: '',
  cost: 0
})

const query = reactive({
  company_id: '',
  asset_tag: '',
  keyword: '',
  status: '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  id: 0,
  company_id: '',
  category_id: 1,
  category_name: '',
  asset_tag: '',
  status: 10,
  brand: '',
  model: '',
  serial_number: '',
  center_name: '',
  department_name: '',
  department_sub: '',
  location: '',
  manager_name: '',
  cpu_name: '',
  memory_size: '',
  main_disk: '',
  secondary_disk: '',
  gpu_name: '',
  mac_address: '',
  purchase_date: '',
  acceptor: '',
  warranty_period: '',
  u8_order_no: '',
  original_price: 0,
  net_value: 0,
  sec_encrypted: false,
  remark: '',
})

const repairForm = reactive({
  oa_number: '',
  user_name: '',
  fault_reason: '',
  diagnosis: '',
  suggestion: '',
  vendor: '',
  contact_name: '',
  contact_phone: '',
  send_date: '',
  cost: 0,
})

const returnForm = reactive({
  repair_id: 0,
  return_date: '',
  result: '',
  cost: 0,
})

const rules = {
  company_id: [{ required: true, message: '请选择所属公司', trigger: 'change' }],
  asset_tag: [{ required: true, message: '请输入固定资产编码', trigger: 'blur' }],
}

function statusText(s) {
  const map = { 10: '库存中', 20: '使用中', 30: '维修中', 40: '已报废' }
  return map[s] || '未知'
}

function formatTime(t) {
  if (!t) return ''
  const date = new Date(t)
  return date.toLocaleString()
}

function formatDate(t) {
  if (!t) return ''
  const date = new Date(t)
  if (isNaN(date.getTime())) return ''
  return date.toISOString().slice(0, 10)
}

// 台账"已使用月数"为购入时间的派生计算列，不入库
function usedMonths(purchaseDate) {
  if (!purchaseDate) return '-'
  const start = new Date(purchaseDate)
  if (isNaN(start.getTime())) return '-'
  const now = new Date()
  const months = (now.getFullYear() - start.getFullYear()) * 12 + (now.getMonth() - start.getMonth())
  return months < 0 ? '0 个月' : `${months} 个月`
}

function repairStatusText(s) {
  const map = { repairing: '寄修中', returned: '已寄回', scrapped: '已报废' }
  return map[s] || s || '未知'
}

function repairStatusTag(s) {
  const map = { repairing: 'warning', returned: 'success', scrapped: 'danger' }
  return map[s] || 'info'
}

function statusTagType(s) {
  const map = { 10: 'info', 20: 'success', 30: 'warning', 40: 'danger' }
  return map[s] || ''
}

const formatCPUs = computed(() => {
  const cpus = deviceDetail.value?.hardware?.cpu || []
  if (cpus.length > 0) {
    return cpus.map(c => `${c.model} (${c.cores}核/${c.threads}线程)`).join('; ')
  }
  // 终端尚未在线采集时，依次回退到设备摘要与台账账面规格
  if (currentAsset.value?.device?.cpu_model) {
    return currentAsset.value.device.cpu_model
  }
  if (currentAsset.value?.cpu_name) {
    return currentAsset.value.cpu_name + '（台账账面值）'
  }
  return '暂无数据 (终端未采集)'
})

const formatMemory = computed(() => {
  const mb = deviceDetail.value?.hardware?.memory_total_mb
  if (mb) {
    return `${(mb / 1024).toFixed(1)} GB`
  }
  // 终端尚未在线采集时，依次回退到设备摘要与台账账面规格
  if (currentAsset.value?.device?.memory_total_gb) {
    return `${currentAsset.value.device.memory_total_gb} GB`
  }
  if (currentAsset.value?.memory_size) {
    return currentAsset.value.memory_size + '（台账账面值）'
  }
  return '暂无数据'
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
    const [eventsRes, repairsRes] = await Promise.all([
      api(`/api/v1/assets/${row.id}/events`).catch(() => ({ data: [] })),
      api(`/api/v1/assets/${row.id}/repairs`).catch(() => ({ data: [] }))
    ])
    assetEvents.value = eventsRes.data || []
    assetRepairs.value = repairsRes.data || []

    const deviceId = row.device?.device_id || ''
    if (deviceId) {
      const res = await api(`/api/v1/devices/${deviceId}/history?limit=20`)
      const reports = res.reports || []
      // 最新一条可能是心跳包（不含 hardware），取最近一条全量上报，与终端画像页口径一致
      const full = reports.find(r => r.report_type === 'full')
      deviceDetail.value = full ? full.payload : null
    } else {
      deviceDetail.value = null
    }
  } catch (err) {
    console.error('failed to load device detail', err)
  } finally {
    drawerLoading.value = false
  }
}

function resetForm() {
  Object.assign(form, {
    id: 0,
    company_id: '',
    category_id: 1,
    category_name: '',
    asset_tag: '',
    status: 10,
    brand: '',
    model: '',
    serial_number: '',
    center_name: '',
    department_name: '',
    department_sub: '',
    location: '',
    manager_name: '',
    cpu_name: '',
    memory_size: '',
    main_disk: '',
    secondary_disk: '',
    gpu_name: '',
    mac_address: '',
    purchase_date: '',
    acceptor: '',
    warranty_period: '',
    u8_order_no: '',
    original_price: 0,
    net_value: 0,
    sec_encrypted: false,
    remark: '',
  })
}

function openCreateDialog() {
  resetForm()
  editMode.value = false
  showAddDialog.value = true
}

// 编辑台账：以当前资产数据回填表单，保存走 PUT 更新接口
function openEditDialog() {
  if (!currentAsset.value) return
  const a = currentAsset.value
  resetForm()
  Object.assign(form, {
    id: a.id,
    company_id: a.company_id,
    category_id: a.category_id,
    category_name: a.category_name || '',
    asset_tag: a.asset_tag,
    status: a.status,
    brand: a.brand || '',
    model: a.model || '',
    serial_number: a.serial_number || '',
    center_name: a.center_name || '',
    department_name: a.department_name || '',
    department_sub: a.department_sub || '',
    location: a.location || '',
    manager_name: a.manager_name || '',
    cpu_name: a.cpu_name || '',
    memory_size: a.memory_size || '',
    main_disk: a.main_disk || '',
    secondary_disk: a.secondary_disk || '',
    gpu_name: a.gpu_name || '',
    mac_address: a.mac_address || '',
    purchase_date: formatDate(a.purchase_date),
    acceptor: a.acceptor || '',
    warranty_period: a.warranty_period || '',
    u8_order_no: a.u8_order_no || '',
    original_price: a.original_price || 0,
    net_value: a.net_value || 0,
    sec_encrypted: !!a.sec_encrypted,
    remark: a.remark || '',
  })
  editMode.value = true
  showAddDialog.value = true
}

function openRepairDialog() {
  Object.assign(repairForm, {
    oa_number: '', user_name: currentAsset.value?.user?.real_name || '',
    fault_reason: '', diagnosis: '', suggestion: '', vendor: '',
    contact_name: '', contact_phone: '', send_date: '', cost: 0,
  })
  showRepairDialog.value = true
}

async function submitRepair() {
  if (!currentAsset.value) return
  repairSubmitting.value = true
  try {
    await api(`/api/v1/assets/${currentAsset.value.id}/repairs`, {
      method: 'POST',
      body: JSON.stringify({ ...repairForm, send_date: repairForm.send_date || null })
    })
    ElMessage.success('送修登记成功，资产状态已置为维修中')
    showRepairDialog.value = false
    await openAssetDetail(currentAsset.value)
    fetchAssets()
  } catch (err) {
    ElMessage.error(err.message || '送修登记失败')
  } finally {
    repairSubmitting.value = false
  }
}

function openReturnDialog(row) {
  returnForm.repair_id = row.id
  returnForm.return_date = ''
  returnForm.result = ''
  returnForm.cost = row.cost || 0
  showReturnDialog.value = true
}

async function submitReturn() {
  if (!currentAsset.value || !returnForm.repair_id) return
  returnSubmitting.value = true
  try {
    await api(`/api/v1/assets/${currentAsset.value.id}/repairs/${returnForm.repair_id}`, {
      method: 'PUT',
      body: JSON.stringify({
        return_date: returnForm.return_date || null,
        result: returnForm.result,
        cost: returnForm.cost,
        status: 'returned'
      })
    })
    ElMessage.success('寄回登记成功，资产状态已恢复为使用中')
    showReturnDialog.value = false
    await openAssetDetail(currentAsset.value)
    fetchAssets()
  } catch (err) {
    ElMessage.error(err.message || '寄回登记失败')
  } finally {
    returnSubmitting.value = false
  }
}

function openReviewDialog(evt) {
  reviewForm.event_id = evt.id
  reviewForm.oa_number = ''
  reviewForm.cost = 0
  showReviewDialog.value = true
}

async function submitReview() {
  if (!currentAsset.value || !reviewForm.event_id) return
  reviewSubmitting.value = true
  try {
    await api(`/api/v1/assets/${currentAsset.value.id}/events/${reviewForm.event_id}/approve`, {
      method: 'POST',
      body: JSON.stringify({
        oa_number: reviewForm.oa_number,
        cost: reviewForm.cost
      })
    })
    ElMessage.success('硬件变更已审核通过，基线已更新')
    showReviewDialog.value = false
    // 重新拉取当前资产数据和事件
    await openAssetDetail(currentAsset.value)
    fetchAssets() // 刷新外层列表
  } catch (err) {
    ElMessage.error(err.message || '审核失败')
  } finally {
    reviewSubmitting.value = false
  }
}

function openMergeDialog() {
  mergeTargetId.value = null
  mergeCandidates.value = []
  showMergeDialog.value = true
  searchMergeTargets('')
}

// 合并目标候选搜索：排除当前资产自身
async function searchMergeTargets(kw) {
  if (!currentAsset.value) return
  mergeSearching.value = true
  try {
    const params = new URLSearchParams({ page: 1, page_size: 20 })
    if (kw) params.append('keyword', kw)
    const res = await api(`/api/v1/assets?${params.toString()}`)
    mergeCandidates.value = (res.data?.items || []).filter(a => a.id !== currentAsset.value.id)
  } catch (err) {
    console.error('search merge targets failed', err)
  } finally {
    mergeSearching.value = false
  }
}

async function submitMerge() {
  if (!currentAsset.value || !mergeTargetId.value) return
  merging.value = true
  try {
    await api(`/api/v1/assets/${currentAsset.value.id}/merge`, {
      method: 'POST',
      body: JSON.stringify({ target_id: mergeTargetId.value })
    })
    ElMessage.success('合并完成，源记录已删除')
    showMergeDialog.value = false
    drawerVisible.value = false
    fetchAssets()
  } catch (err) {
    ElMessage.error(err.message || '合并失败')
  } finally {
    merging.value = false
  }
}

async function confirmDelete() {
  if (!currentAsset.value) return
  try {
    await ElMessageBox.confirm(
      `删除后该资产从列表消失（软删除，履历数据保留）；其关联终端将解绑，下次上报会重新生成待编资产。确认删除 ${currentAsset.value.asset_tag} 吗？`,
      '删除资产确认',
      { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await api(`/api/v1/assets/${currentAsset.value.id}`, { method: 'DELETE' })
    ElMessage.success('资产已删除')
    drawerVisible.value = false
    fetchAssets()
  } catch (err) {
    ElMessage.error(err.message || '删除失败')
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
    if (query.asset_tag) params.append('asset_tag', query.asset_tag)
    if (query.keyword) params.append('keyword', query.keyword)
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
  query.asset_tag = ''
  query.keyword = ''
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
      // date-picker 空值时是空字符串，后端 *time.Time 无法解析，统一置为 null
      const categoryNames = { 1: '台式整机', 2: '笔记本电脑', 3: '显示器', 4: '外设及其他' }
      const payload = {
        ...form,
        purchase_date: form.purchase_date || null,
        category_name: categoryNames[form.category_id] || '',
      }
      if (editMode.value) {
        await api(`/api/v1/assets/${form.id}`, {
          method: 'PUT',
          body: JSON.stringify(payload),
        })
        ElMessage.success('台账保存成功！')
        if (currentAsset.value && currentAsset.value.id === form.id) {
          await openAssetDetail(currentAsset.value)
        }
      } else {
        await api('/api/v1/assets', {
          method: 'POST',
          body: JSON.stringify(payload),
        })
        ElMessage.success('固定资产档案登记成功！')
      }
      showAddDialog.value = false
      fetchAssets()
    } catch (err) {
      ElMessage.error(err.message || '保存失败')
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
.mono {
  font-family: monospace;
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
