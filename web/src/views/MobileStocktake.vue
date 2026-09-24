<template>
  <div class="mobile-stocktake">
    <!-- 顶部任务条 -->
    <div class="topbar">
      <div class="brand">ITAM 扫码盘点</div>
      <div v-if="task" class="task-info">
        <div class="task-name">{{ task.name }}</div>
        <div class="task-progress">
          待盘 {{ task.counts?.[10] || 0 }} · 正常 {{ task.counts?.[20] || 0 }} ·
          丢失 {{ task.counts?.[30] || 0 }} · 损坏 {{ task.counts?.[40] || 0 }} · 报废 {{ task.counts?.[50] || 0 }}
        </div>
      </div>
      <div v-else class="task-info muted">未接入盘点任务</div>
    </div>

    <!-- 盘点码录入（未持有有效盘点码时的一切页面态） -->
    <div v-if="!task" class="token-entry">
      <div class="card">
        <div class="card-title">输入盘点码</div>
        <div class="card-hint">盘点码由 IT 管理员在「盘点任务」中生成，只在生成时一次性展示。任务结束后立即失效。</div>
        <el-input
          ref="tokenInputRef"
          v-model="tokenInput"
          size="large"
          placeholder="粘贴或扫描盘点码"
          @keyup.enter="applyToken(tokenInput)"
        >
          <template #append>
            <el-button :loading="validating" @click="applyToken(tokenInput)">进入盘点</el-button>
          </template>
        </el-input>
        <div v-if="tokenError" class="error-text">{{ tokenError }}</div>
      </div>
    </div>

    <!-- 扫码输入台（/m/scan） -->
    <div v-else-if="mode === 'scan'" class="scan-console">
      <div class="card">
        <div class="card-title">扫描 / 输入资产编码</div>
        <el-input
          ref="scanInputRef"
          v-model="scanInput"
          size="large"
          placeholder="扫码枪对准标签，或手动输入编码"
          @keyup.enter="goAsset(scanInput)"
        >
          <template #append>
            <el-button type="primary" @click="goAsset(scanInput)">核对</el-button>
          </template>
        </el-input>
        <div class="card-hint">扫码枪会自动输入并回车；也可以在下方维护本次盘点人姓名（将记录到每条明细）。</div>
        <el-input v-model="scannedBy" size="large" placeholder="盘点人姓名（必填，本地记忆）" style="margin-top: 10px" />
      </div>
    </div>

    <!-- 资产核对（/a/:number） -->
    <div v-else class="asset-view">
      <!-- 范围外 / 已答复型提示 -->
      <div v-if="assetError" class="card">
        <div class="error-text">{{ assetError }}</div>
        <el-button type="primary" size="large" class="block-btn" @click="backToScan">返回继续扫码</el-button>
      </div>

      <template v-else-if="assetData">
        <!-- 范围外资产：只提示，不展示任何台账信息 -->
        <div v-if="!assetData.in_scope" class="card">
          <div class="card-title">{{ assetData.asset.asset_tag }}</div>
          <div class="error-text">该资产不在本次盘点范围内，无需核对。</div>
          <el-button type="primary" size="large" class="block-btn" @click="backToScan">返回继续扫码</el-button>
        </div>

        <template v-else>
          <div class="card">
            <div class="asset-head">
              <span class="asset-tag">{{ assetData.asset.asset_tag }}</span>
              <el-tag v-if="assetData.item?.result !== 10" :type="resultTagType(assetData.item.result)" size="small">
                已核：{{ resultText(assetData.item.result) }}
              </el-tag>
            </div>
            <div class="asset-rows">
              <div class="row"><span>类别</span><b>{{ assetData.asset.category_name || '-' }}</b></div>
              <div class="row"><span>规格</span><b>{{ [assetData.asset.brand, assetData.asset.model_name].filter(Boolean).join(' ') || '-' }}</b></div>
              <div class="row"><span>序列号</span><b class="mono">{{ assetData.asset.serial_number || '-' }}</b></div>
              <div class="row"><span>建账位置</span><b>{{ assetData.item?.expected_location || '-' }}</b></div>
              <div class="row"><span>负责人</span><b>{{ assetData.asset.manager_name || '-' }}</b></div>
            </div>
            <div v-if="assetData.hw_pending > 0" class="hw-warn">
              该资产有 {{ assetData.hw_pending }} 条硬件变更待人工确认——请现场核实配置后，判"正常"将自动确认变更履历
            </div>
          </div>

          <div class="card">
            <div class="card-title">核对结果</div>
            <div class="result-grid">
              <button class="result-btn ok" :class="{ active: checkForm.result === 20 }" @click="checkForm.result = 20">正常</button>
              <button class="result-btn lost" :class="{ active: checkForm.result === 30 }" @click="checkForm.result = 30">丢失</button>
              <button class="result-btn damaged" :class="{ active: checkForm.result === 40 }" @click="checkForm.result = 40">损坏</button>
              <button class="result-btn scrapped" :class="{ active: checkForm.result === 50 }" @click="checkForm.result = 50">报废</button>
            </div>
            <el-input v-model="checkForm.actual_location" placeholder="实盘位置（选填）" style="margin-top: 12px" />
            <el-input v-model="checkForm.remark" placeholder="备注（选填）" style="margin-top: 8px" />
            <el-input v-model="scannedBy" placeholder="盘点人姓名（必填）" style="margin-top: 8px" />
            <el-button type="primary" size="large" class="block-btn" :loading="submitting" @click="submitCheck">
              提交核对结果
            </el-button>
            <el-button size="large" class="block-btn plain-btn" @click="backToScan">返回继续扫码</el-button>
          </div>
        </template>
      </template>

      <div v-else class="card loading-card">加载中…</div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, nextTick, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { pubApi } from '../api'
import { ElMessage } from 'element-plus'

const route = useRoute()
const router = useRouter()

const SCAN_TOKEN_KEY = 'itagent_scan_token'
const SCANNED_BY_KEY = 'itagent_scanned_by'

const token = ref(localStorage.getItem(SCAN_TOKEN_KEY) || '')
const scannedBy = ref(localStorage.getItem(SCANNED_BY_KEY) || '')
const tokenInput = ref('')
const tokenError = ref('')
const validating = ref(false)

const task = ref(null)
const scanInput = ref('')
const assetData = ref(null)
const assetError = ref('')
const submitting = ref(false)

const scanInputRef = ref(null)
const tokenInputRef = ref(null)

const checkForm = reactive({ result: 20, actual_location: '', remark: '' })

// 三条路由共用本组件：/m/t/:token（换码入口）、/m/scan（扫码台）、/a/:number（标签直达）
const mode = computed(() => (route.path.startsWith('/a/') ? 'asset' : 'scan'))

watch(scannedBy, (v) => localStorage.setItem(SCANNED_BY_KEY, v || ''))

async function applyToken(raw) {
  const t = (raw || '').trim()
  if (!t) {
    tokenError.value = '请输入盘点码'
    return
  }
  validating.value = true
  tokenError.value = ''
  try {
    const res = await pubApi(`/api/v1/public/stocktakes/${encodeURIComponent(t)}`)
    token.value = t
    localStorage.setItem(SCAN_TOKEN_KEY, t)
    task.value = res.data
    ElMessage.success(`已接入盘点任务「${res.data?.name}」`)
    if (route.params.number) {
      loadAsset(route.params.number)
    } else {
      router.replace('/m/scan')
    }
  } catch (err) {
    tokenError.value = err.message || '盘点码无效或盘点已结束'
  } finally {
    validating.value = false
  }
}

async function refreshTask() {
  if (!token.value) {
    task.value = null
    return
  }
  try {
    const res = await pubApi(`/api/v1/public/stocktakes/${encodeURIComponent(token.value)}`)
    task.value = res.data
  } catch {
    // 盘点码失效（任务结束/取消/轮换）：清状态回录入页
    token.value = ''
    localStorage.removeItem(SCAN_TOKEN_KEY)
    task.value = null
    tokenError.value = '盘点码已失效（任务可能已结束），请向管理员获取新码'
  }
}

function backToScan() {
  scanInput.value = ''
  router.replace('/m/scan')
}

function goAsset(raw) {
  const tag = (raw || '').trim()
  if (!tag) return
  router.push(`/a/${encodeURIComponent(tag)}`)
}

async function loadAsset(tag) {
  assetData.value = null
  assetError.value = ''
  try {
    const res = await pubApi(`/api/v1/public/stocktakes/${encodeURIComponent(token.value)}/assets/${encodeURIComponent(tag)}`)
    assetData.value = res.data
    const item = res.data?.item
    checkForm.result = item && item.result !== 10 ? item.result : 20
    checkForm.actual_location = item?.actual_location || ''
    checkForm.remark = item?.remark || ''
  } catch (err) {
    assetError.value = err.message || '查询资产失败'
  }
}

async function submitCheck() {
  if (!scannedBy.value.trim()) {
    ElMessage.warning('请填写盘点人姓名')
    return
  }
  if (!assetData.value?.in_scope || !assetData.value?.asset?.asset_tag) return
  submitting.value = true
  try {
    await pubApi(`/api/v1/public/stocktakes/${encodeURIComponent(token.value)}/check`, {
      method: 'POST',
      body: JSON.stringify({
        scanned_by: scannedBy.value.trim(),
        checks: [{
          asset_tag: assetData.value.asset.asset_tag,
          result: checkForm.result,
          actual_location: checkForm.actual_location,
          remark: checkForm.remark,
        }],
      }),
    })
    ElMessage.success('核对结果已提交')
    refreshTask()
    backToScan()
  } catch (err) {
    ElMessage.error(err.message || '提交失败')
  } finally {
    submitting.value = false
  }
}

function resultText(r) {
  return { 20: '正常', 30: '丢失', 40: '损坏', 50: '报废' }[r] || '待盘'
}

function resultTagType(r) {
  return { 20: 'success', 30: 'danger', 40: 'warning', 50: 'danger' }[r] || 'info'
}

async function init() {
  // /m/t/:token —— 管理端二维码/链接入口：换存新码后跳扫码台
  if (route.params.token) {
    tokenInput.value = route.params.token
    await applyToken(route.params.token)
    if (token.value && !route.params.number) return
  }
  await refreshTask()
  if (mode.value === 'asset' && route.params.number && token.value) {
    await loadAsset(route.params.number)
  }
  await nextTick()
  if (!task.value) {
    tokenInputRef.value?.focus?.()
  } else if (mode.value === 'scan') {
    scanInputRef.value?.focus?.()
  }
}

watch(() => route.fullPath, () => {
  if (route.meta.public) init()
})

onMounted(init)
</script>

<style scoped>
.mobile-stocktake {
  max-width: 560px;
  margin: 0 auto;
  padding: 16px 14px 40px;
  min-height: 100vh;
  box-sizing: border-box;
}
.topbar {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  background: #1e293b;
  color: #f8fafc;
  border-radius: 12px;
  margin-bottom: 14px;
}
.brand {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 1px;
}
.task-info .task-name {
  font-size: 13px;
  font-weight: 600;
  color: #93c5fd;
}
.task-progress {
  font-size: 12px;
  color: #94a3b8;
}
.muted {
  color: #94a3b8;
  font-size: 12px;
}
.card {
  background: #fff;
  border-radius: 12px;
  padding: 16px;
  margin-bottom: 14px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}
.card-title {
  font-size: 15px;
  font-weight: 700;
  color: #1f2937;
  margin-bottom: 10px;
}
.card-hint {
  font-size: 12px;
  color: #9ca3af;
  margin-bottom: 10px;
  line-height: 1.6;
}
.token-entry {
  padding-top: 8vh;
}
.error-text {
  color: #dc2626;
  font-size: 13px;
  margin-top: 10px;
  line-height: 1.6;
}
.asset-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.asset-tag {
  font-family: monospace;
  font-size: 20px;
  font-weight: 800;
  color: #2563eb;
  word-break: break-all;
}
.asset-rows .row {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  padding: 6px 0;
  border-bottom: 1px dashed #e5e7eb;
}
.asset-rows .row span {
  color: #9ca3af;
}
.asset-rows .row b {
  color: #1f2937;
  text-align: right;
  max-width: 65%;
  word-break: break-all;
}
.mono {
  font-family: monospace;
}
.hw-warn {
  margin-top: 12px;
  font-size: 12px;
  color: #b45309;
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: 8px;
  padding: 10px;
  line-height: 1.6;
}
.result-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}
.result-btn {
  padding: 14px 0;
  font-size: 15px;
  font-weight: 700;
  border: 1px solid #d1d5db;
  border-radius: 10px;
  background: #f9fafb;
  color: #374151;
  cursor: pointer;
}
.result-btn.ok.active { background: #16a34a; border-color: #16a34a; color: #fff; }
.result-btn.lost.active { background: #dc2626; border-color: #dc2626; color: #fff; }
.result-btn.damaged.active { background: #ea580c; border-color: #ea580c; color: #fff; }
.result-btn.scrapped.active { background: #7f1d1d; border-color: #7f1d1d; color: #fff; }
.block-btn {
  width: 100%;
  margin-top: 12px;
}
.block-btn + .block-btn {
  margin-left: 0;
}
.plain-btn {
  margin-top: 8px;
}
.loading-card {
  text-align: center;
  color: #9ca3af;
}
</style>
