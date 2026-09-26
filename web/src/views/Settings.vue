<template>
  <div class="settings-view">
    <div class="page-header">
      <div>
        <h2>系统与集成设置</h2>
        <span class="subtitle">终端防护、Webhook 告警、AD 目录服务（规划中）与标签打印规则</span>
      </div>
    </div>

    <el-tabs type="border-card" class="settings-card">
      <el-tab-pane label="终端防护">
        <el-alert
          title="防退出 / 防卸载密码归服务端集中管理，安装包不内置任何密码；终端在 ≤10 分钟心跳内自动拉取生效"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom: 20px"
        />
        <el-row :gutter="16">
          <el-col :span="12">
            <el-card shadow="never" class="protection-card">
              <template #header>
                <div class="protection-card-header">
                  <span>防退出（托盘退出）</span>
                  <el-switch v-model="protection.quit.enabled" @change="v => saveEnabled('quit', protection.quit, v)" />
                </div>
              </template>
              <el-form label-width="90px">
                <el-form-item label="管理密码">
                  <el-input
                    v-model="protection.quit.password"
                    type="password"
                    show-password
                    placeholder="至少 8 位；留空保持现有密码"
                  />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" :loading="protection.quit.saving" @click="savePassword('quit', protection.quit)">保存密码</el-button>
                  <el-tag v-if="protection.quit.passwordSet" type="success" size="small" style="margin-left: 8px">已设置密码</el-tag>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
          <el-col :span="12">
            <el-card shadow="never" class="protection-card">
              <template #header>
                <div class="protection-card-header">
                  <span>防卸载（终端卸载）</span>
                  <el-switch v-model="protection.uninstall.enabled" @change="v => saveEnabled('uninstall', protection.uninstall, v)" />
                </div>
              </template>
              <el-form label-width="90px">
                <el-form-item label="管理密码">
                  <el-input
                    v-model="protection.uninstall.password"
                    type="password"
                    show-password
                    placeholder="至少 8 位；留空保持现有密码"
                  />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" :loading="protection.uninstall.saving" @click="savePassword('uninstall', protection.uninstall)">保存密码</el-button>
                  <el-tag v-if="protection.uninstall.passwordSet" type="success" size="small" style="margin-left: 8px">已设置密码</el-tag>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
        </el-row>
        <el-alert
          title="开启防卸载后，终端卸载需本地密码或在线验证码（设备明细页可生成）；两个模块都关闭时全部操作免验证"
          type="warning"
          show-icon
          :closable="false"
          style="margin-top: 16px"
        />
      </el-tab-pane>

      <el-tab-pane label="Webhook 告警">
        <el-alert
          title="超期未归 / 疑似失联资产由服务端每分钟定时扫描并推送；secret 非空时请求携带 X-ITAM-Signature 签名头（HMAC-SHA256，对原始 body 计算，hex 编码），接收端可据此校验来源"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom: 20px"
        />
        <el-form label-width="140px" style="max-width: 680px">
          <el-form-item label="告警开关">
            <el-switch v-model="webhook.enabled" active-text="启用后每分钟扫描一次" />
          </el-form-item>
          <el-form-item label="接收端 URL">
            <el-input v-model="webhook.url" placeholder="https://hooks.example.com/itam" />
          </el-form-item>
          <el-form-item label="签名 Secret">
            <el-input
              v-model="webhook.secret"
              type="password"
              show-password
              placeholder="可选；留空保持现有 secret"
              style="max-width: 320px"
            />
            <el-tag v-if="webhook.secretSet" type="success" size="small" style="margin-left: 8px">已设置</el-tag>
          </el-form-item>
          <el-form-item label="冷却窗口">
            <el-input-number v-model="webhook.cooldown" :min="1" :max="10080" style="width: 140px" />
            <span class="cooldown-hint">分钟；同一资产未处理期间不重复推送，期满仍未恢复再次提醒</span>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="webhook.saving" @click="saveWebhook">保存配置</el-button>
            <el-button :loading="webhook.testing" @click="testWebhook">发送测试</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="Agent 采集与失联判定">
        <el-alert
          title="心跳/全量上报周期与失联阈值由服务端统一下发（Agent v0.2.6 起动态生效，老版本沿用自身默认）；失联阈值必须大于心跳周期，否则健康终端会被误判失联"
          type="info"
          show-icon
          :closable="false"
          style="margin-bottom: 20px"
        />
        <el-form label-width="180px" style="max-width: 680px">
          <el-form-item label="心跳上报周期 (分钟)">
            <el-input-number v-model="agentCfg.heartbeatMin" :min="1" :max="1440" style="width: 160px" />
            <span class="cfg-hint">终端动态信息（登录人/网络/心跳）上报频率</span>
          </el-form-item>
          <el-form-item label="全量上报周期 (分钟)">
            <el-input-number v-model="agentCfg.fullMin" :min="5" :max="10080" style="width: 160px" />
            <span class="cfg-hint">硬件基线/软件清单等全量采集，不能小于心跳周期</span>
          </el-form-item>
          <el-form-item label="失联判定阈值 (分钟)">
            <el-input-number v-model="agentCfg.thresholdMin" :min="2" :max="2880" style="width: 160px" />
            <span class="cfg-hint">心跳超过该时长未上报即判「疑似失联」，必须大于心跳周期</span>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="agentCfg.saving" @click="saveAgentCfg">保存采集配置</el-button>
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
import { reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../api'

const protection = reactive({
  quit: { enabled: false, passwordSet: false, password: '', saving: false },
  uninstall: { enabled: false, passwordSet: false, password: '', saving: false },
})

const webhook = reactive({
  enabled: false,
  url: '',
  secret: '',
  secretSet: false,
  cooldown: 60,
  saving: false,
  testing: false,
})

// Agent 采集与失联判定（agent_settings 单例）：表单以分钟呈现、
// 提交换算秒；联动校验（阈值>心跳、full≥心跳）前端先拦一道，
// 服务端为权威校验（400 原样展示）。
// 漫游地理基准已迁至组织页（companies.region 按公司维护），本 tab 不再承载
const agentCfg = reactive({
  heartbeatMin: 60,
  fullMin: 360,
  thresholdMin: 65,
  saving: false,
})

async function loadAgentCfg() {
  try {
    const res = await api('/api/v1/agent-settings')
    const d = res.data || {}
    agentCfg.heartbeatMin = Math.round((d.heartbeat_interval_sec || 3600) / 60)
    agentCfg.fullMin = Math.round((d.full_interval_sec || 21600) / 60)
    agentCfg.thresholdMin = Math.round((d.offline_threshold_sec || 3900) / 60)
  } catch { /* 非 admin 或加载失败保持默认展示 */ }
}

async function saveAgentCfg() {
  if (agentCfg.thresholdMin <= agentCfg.heartbeatMin) {
    ElMessage.warning('失联判定阈值必须大于心跳周期')
    return
  }
  if (agentCfg.fullMin < agentCfg.heartbeatMin) {
    ElMessage.warning('全量上报周期不能小于心跳周期')
    return
  }
  agentCfg.saving = true
  try {
    await api('/api/v1/agent-settings', {
      method: 'PUT',
      body: JSON.stringify({
        heartbeat_interval_sec: agentCfg.heartbeatMin * 60,
        full_interval_sec: agentCfg.fullMin * 60,
        offline_threshold_sec: agentCfg.thresholdMin * 60,
      }),
    })
    ElMessage.success('采集配置已保存，终端在下一轮心跳自动生效（v0.2.6+）')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    agentCfg.saving = false
  }
}

onMounted(async () => {
  try {
    const res = await api('/api/v1/protection/modules')
    const d = res.data || {}
    protection.quit.enabled = d.quit_protection?.enabled || false
    protection.quit.passwordSet = d.quit_protection?.password_set || false
    protection.uninstall.enabled = d.uninstall_protection?.enabled || false
    protection.uninstall.passwordSet = d.uninstall_protection?.password_set || false
  } catch (e) {
    ElMessage.error('加载防护配置失败: ' + e.message)
  }
  try {
    const res = await api('/api/v1/webhook-alerts/config')
    const d = res.data || {}
    webhook.enabled = d.enabled || false
    webhook.url = d.webhook_url || ''
    webhook.secretSet = d.secret_set || false
    webhook.cooldown = d.cooldown_minutes || 60
  } catch (e) {
    ElMessage.error('加载 Webhook 告警配置失败: ' + e.message)
  }
  loadAgentCfg()
})

async function saveEnabled(key, target, enabled) {
  const oldValue = target.enabled
  target.enabled = enabled
  try {
    await api(`/api/v1/protection/modules/${key}`, { method: 'PUT', body: JSON.stringify({ enabled }) })
    ElMessage.success(enabled ? '已启用，终端 ≤10 分钟内生效' : '已关闭，终端操作免验证')
  } catch (e) {
    target.enabled = oldValue
    ElMessage.error(e.message)
  }
}

async function savePassword(key, target) {
  if (!target.password) {
    ElMessage.warning('请输入密码')
    return
  }
  target.saving = true
  try {
    await api(`/api/v1/protection/modules/${key}`, { method: 'PUT', body: JSON.stringify({ password: target.password }) })
    target.passwordSet = true
    target.password = ''
    ElMessage.success('密码已更新，终端 ≤10 分钟内生效')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    target.saving = false
  }
}

async function saveWebhook() {
  if (webhook.enabled && !webhook.url) {
    ElMessage.warning('启用前请先填写接收端 URL')
    return
  }
  webhook.saving = true
  try {
    const body = {
      enabled: webhook.enabled,
      webhook_url: webhook.url,
      cooldown_minutes: webhook.cooldown,
    }
    if (webhook.secret) body.secret = webhook.secret
    const res = await api('/api/v1/webhook-alerts/config', { method: 'PUT', body: JSON.stringify(body) })
    webhook.secretSet = res.data?.secret_set || webhook.secretSet
    webhook.secret = ''
    ElMessage.success('Webhook 告警配置已保存')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    webhook.saving = false
  }
}

async function testWebhook() {
  webhook.testing = true
  try {
    const res = await api('/api/v1/webhook-alerts/test', { method: 'POST', body: JSON.stringify({}) })
    ElMessage.success(res.data?.message || '测试推送已送达')
  } catch (e) {
    ElMessage.error('测试推送失败: ' + e.message)
  } finally {
    webhook.testing = false
  }
}
</script>

<style scoped>
.page-header {
  margin-bottom: 16px;
}
.page-header h2 { margin: 0 0 4px 0; font-size: 20px; }
.subtitle { font-size: 13px; color: #6b7280; }
.settings-card { border-radius: 8px; min-height: 480px; }
.protection-card-header { display: flex; align-items: center; justify-content: space-between; }
.cooldown-hint { margin-left: 8px; font-size: 13px; color: #6b7280; }
.cfg-hint { margin-left: 8px; font-size: 12px; color: #9ca3af; }
</style>
