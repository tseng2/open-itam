<template>
  <div class="settings-view">
    <div class="page-header">
      <div>
        <h2>系统与集成设置</h2>
        <span class="subtitle">用友 U8 凭据与采购单对接、AD 域控同步、数据字典与二维码规则</span>
      </div>
    </div>

    <el-tabs type="border-card" class="settings-card">
      <el-tab-pane label="用友 U8 v18 集成">
        <el-form label-width="180px" style="max-width: 680px; margin-top: 16px">
          <el-form-item label="U8 API 网关地址">
            <el-input v-model="u8.gateway" placeholder="http://u8-api.internal:8080" />
          </el-form-item>
          <el-form-item label="U8 账套号 (AccID)">
            <el-input v-model="u8.accId" placeholder="如: 001" />
          </el-form-item>
          <el-form-item label="U8 OpenAPI AppKey">
            <el-input v-model="u8.appKey" />
          </el-form-item>
          <el-form-item label="采购单同步状态">
            <el-switch v-model="u8.autoSync" active-text="开启每日定时拉取新采购单入库" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary">保存配置</el-button>
            <el-button>测试连通性</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="AD / 目录服务 (Identity Hub)">
        <el-form label-width="180px" style="max-width: 680px; margin-top: 16px">
          <el-form-item label="LDAP 服务器地址">
            <el-input placeholder="ldaps://ad.example.com:636" />
          </el-form-item>
          <el-form-item label="Base DN">
            <el-input placeholder="DC=corp,DC=example,DC=com" />
          </el-form-item>
          <el-form-item label="同步频率">
            <el-select placeholder="每 2 小时增量同步">
              <el-option label="每 1 小时" value="1h" />
              <el-option label="每 2 小时" value="2h" />
              <el-option label="每日凌晨 03:00" value="daily" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button type="primary">保存 AD 设置</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="条码与二维码标签规则">
        <el-alert
          title="预留接口：可自定义打印模板（包含资产名称、SN、用友U8采购单、所属公司与盘点二维码）"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom: 20px"
        />
        <el-form label-width="180px" style="max-width: 680px">
          <el-form-item label="标签编码前缀">
            <el-input placeholder="如: ITAM-" />
          </el-form-item>
          <el-form-item label="二维码跳转链接模版">
            <el-input placeholder="https://itam.internal/scan/{asset_tag}" />
          </el-form-item>
        </el-form>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { reactive } from 'vue'

const u8 = reactive({
  gateway: 'http://10.1.1.200:8080',
  accId: '001',
  appKey: 'U8-OPENAPI-2026-ITAM',
  autoSync: true,
})
</script>

<style scoped>
.page-header {
  margin-bottom: 16px;
}
.page-header h2 { margin: 0 0 4px 0; font-size: 20px; }
.subtitle { font-size: 13px; color: #6b7280; }
.settings-card { border-radius: 8px; min-height: 480px; }
</style>
