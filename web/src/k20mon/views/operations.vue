<template>
  <div class="operations-page">
    <div class="page-heading">
      <div>
        <div class="eyebrow">K20MON</div>
        <h1>操作中心</h1>
        <p>所有动作通过设备端 root 代理执行，敏感字段不会写入操作日志。</p>
      </div>
    </div>

    <el-alert
      class="operation-alert"
      type="warning"
      :closable="false"
      show-icon
      title="请确认设备状态后再执行写操作"
    />

    <div class="operation-grid">
      <el-card shadow="never">
        <template #header><span>充电守护</span></template>
        <el-form label-position="top">
          <el-form-item label="启用">
            <el-switch v-model="charge.enabled" />
          </el-form-item>
          <div class="form-grid">
            <el-form-item label="限制阈值（%）">
              <el-input-number v-model="charge.low_threshold" :min="51" :max="100" />
            </el-form-item>
            <el-form-item label="恢复阈值（%）">
              <el-input-number v-model="charge.resume_threshold" :min="50" :max="99" />
            </el-form-item>
            <el-form-item label="低电流（uA）">
              <el-input-number v-model="charge.low_current_ua" :min="50000" :max="10000000" :step="50000" />
            </el-form-item>
            <el-form-item label="正常电流（uA）">
              <el-input-number v-model="charge.normal_current_ua" :min="50000" :max="10000000" :step="50000" />
            </el-form-item>
          </div>
          <div class="actions">
            <el-button @click="loadChargeGuard">读取</el-button>
            <el-button type="primary" @click="saveChargeGuard">保存</el-button>
          </div>
          <pre v-if="chargeResult">{{ JSON.stringify(chargeResult, null, 2) }}</pre>
        </el-form>
      </el-card>

      <el-card shadow="never">
        <template #header><span>Wi-Fi</span></template>
        <el-form label-position="top">
          <div class="status-strip" :class="{ 'is-online': wifiStatus?.wifi?.connected }">
            <div>
              <strong>{{ wifiStatus?.wifi?.ssid || wifiStatus?.wifi?.connection || '未连接 Wi-Fi' }}</strong>
              <span>
                {{ wifiStatus?.wifi?.interface || '自动选择接口' }}
                · {{ wifiStatus?.wifi?.state || '状态未知' }}
              </span>
              <span v-if="wifiStatus?.wifi?.ip" class="wifi-ip">
                内网 IP：{{ wifiStatus.wifi.ip }}
                · 访问地址：http://{{ wifiStatus.wifi.ip }}:8899
              </span>
            </div>
            <div class="status-actions">
              <el-button v-if="wifiStatus?.wifi?.ip" text @click="copyWifiAddress">复制地址</el-button>
              <el-button text :loading="wifiLoading" @click="loadWifiStatus">刷新状态</el-button>
            </div>
          </div>
          <el-form-item label="接口选择">
            <el-switch v-model="wifi.autoSelect" active-text="自动选择已连接/可用接口" />
          </el-form-item>
          <el-form-item label="网络接口">
            <el-input v-model="wifi.interface" :disabled="wifi.autoSelect" placeholder="留空自动选择" />
          </el-form-item>
          <div class="actions">
            <el-button @click="doWifiScan">扫描网络</el-button>
            <el-button @click="doWifiDisconnect">断开接口</el-button>
          </div>
          <el-form-item label="SSID">
            <el-select
              v-model="wifi.ssid"
              filterable
              allow-create
              default-first-option
              clearable
              placeholder="扫描后选择，或手动输入"
            >
              <el-option
                v-for="network in wifiNetworks"
                :key="`${network.ssid}-${network.security}`"
                :label="`${network.ssid} · ${network.signal ?? '--'}% · ${network.security}`"
                :value="network.ssid"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="wifi.password" type="password" show-password autocomplete="new-password" />
          </el-form-item>
          <el-button type="primary" @click="doWifiConnect">连接 Wi-Fi</el-button>
          <el-table v-if="wifiNetworks.length" class="wifi-table" :data="wifiNetworks" size="small" max-height="220">
            <el-table-column label="SSID" min-width="150">
              <template #default="{ row }">
                <el-button text type="primary" @click="wifi.ssid = row.ssid">{{ row.ssid }}</el-button>
              </template>
            </el-table-column>
            <el-table-column prop="signal" label="信号" width="80" />
            <el-table-column prop="security" label="安全" min-width="100" />
            <el-table-column prop="frequency_mhz" label="频段" width="100" />
          </el-table>
          <pre v-if="wifiResult">{{ JSON.stringify(wifiResult, null, 2) }}</pre>
        </el-form>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>Cloudflare Tunnel</span>
            <el-button text :loading="cloudflareLoading" @click="loadCloudflareStatus">刷新状态</el-button>
          </div>
        </template>
        <el-alert
          :type="cloudflare?.cloudflare?.running ? 'success' : 'warning'"
          :closable="false"
          show-icon
          :title="cloudflare?.cloudflare?.running ? '隧道正在运行' : '隧道未运行或状态不可用'"
        />
        <div class="detail-list">
          <div><span>模式</span><strong>{{ cloudflare?.cloudflare?.mode || '--' }}</strong></div>
          <div><span>隧道 ID</span><strong>{{ cloudflare?.cloudflare?.tunnel_id || '--' }}</strong></div>
          <div><span>Token</span><strong>{{ cloudflare?.cloudflare?.token_configured ? '已配置（已脱敏）' : '未配置' }}</strong></div>
          <div><span>配置文件</span><strong>{{ cloudflare?.cloudflare?.config_file || '--' }}</strong></div>
        </div>
        <el-table v-if="cloudflareIngress.length" class="cloudflare-table" :data="cloudflareIngress" size="small">
          <el-table-column prop="hostname" label="域名" min-width="160" />
          <el-table-column prop="service" label="服务" min-width="160" />
        </el-table>
        <el-alert
          v-if="cloudflare?.cloudflare?.reason"
          class="inline-alert"
          type="info"
          :closable="false"
          :title="cloudflare.cloudflare.reason"
        />
      </el-card>

      <el-card shadow="never" class="rate-sync-card">
        <template #header>
          <div class="card-heading">
            <span>动态定价</span>
            <div class="status-actions">
              <el-tag :type="rateSyncData?.timer?.active ? 'success' : 'warning'" size="small">
                {{ rateSyncData?.timer?.active ? '定时器运行中' : '定时器未运行' }}
              </el-tag>
              <el-button text :loading="rateSyncLoading" @click="loadRateSyncStatus">刷新状态</el-button>
            </div>
          </div>
        </template>
        <el-alert
          :type="rateSyncServiceAlert.type"
          :closable="false"
          show-icon
          :title="rateSyncServiceAlert.title"
        />
        <div class="detail-list">
          <div><span>执行周期</span><strong>{{ rateSyncData?.config?.interval || '--' }}</strong></div>
          <div><span>下次执行</span><strong>{{ rateSyncData?.timer?.next_trigger || '--' }}</strong></div>
          <div><span>上次触发</span><strong>{{ rateSyncData?.timer?.last_trigger || '--' }}</strong></div>
          <div><span>当前售价</span><strong>{{ rateSyncData?.pricing?.price ?? '--' }}</strong></div>
          <div><span>当前成本</span><strong>{{ rateSyncData?.pricing?.cost ?? '--' }}</strong></div>
          <div><span>毛利率 / 风控</span><strong>{{ rateSyncData?.pricing?.current_margin_pct ?? '--' }}% / {{ rateSyncData?.pricing?.risk ?? '--' }}</strong></div>
        </div>
        <el-form label-position="top" class="rate-sync-form">
          <div class="form-grid">
            <el-form-item label="启用自动定价">
              <el-switch v-model="rateSync.enabled" />
            </el-form-item>
            <el-form-item label="定价模式">
              <el-radio-group v-model="rateSync.mode">
                <el-radio-button value="instant">跟随当前成本</el-radio-button>
                <el-radio-button value="rolling">按历史最高成本</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="目标毛利率">
              <el-input-number v-model="rateSync.margin" :min="0.01" :max="0.95" :step="0.01" :precision="2" />
            </el-form-item>
            <el-form-item label="风控余量">
              <el-input-number v-model="rateSync.buffer" :min="0" :max="0.94" :step="0.01" :precision="2" />
            </el-form-item>
            <el-form-item label="实时上游探针">
              <el-switch v-model="rateSync.probe" />
            </el-form-item>
            <el-form-item label="多账号取值">
              <el-select v-model="rateSync.cost_select">
                <el-option label="取最高成本（保守）" value="max" />
                <el-option label="取最低成本" value="min" />
              </el-select>
            </el-form-item>
            <el-form-item label="探针超时（秒）">
              <el-input-number v-model="rateSync.probe_timeout" :min="1" :max="120" />
            </el-form-item>
            <el-form-item label="执行周期">
              <el-select v-model="rateSync.interval">
                <el-option label="30 秒" value="30s" />
                <el-option label="60 秒" value="60s" />
                <el-option label="5 分钟" value="5min" />
                <el-option label="10 分钟" value="10min" />
                <el-option label="30 分钟" value="30min" />
                <el-option label="1 小时" value="1h" />
              </el-select>
            </el-form-item>
            <el-form-item label="历史回看（小时）">
              <el-input-number v-model="rateSync.lookback_hours" :min="1" :max="720" />
            </el-form-item>
            <el-form-item label="最小变价比例">
              <el-input-number v-model="rateSync.min_delta" :min="0" :max="1" :step="0.01" :precision="2" />
            </el-form-item>
            <el-form-item label="价格步长">
              <el-input-number v-model="rateSync.step" :min="0.0001" :max="1" :step="0.01" :precision="4" />
            </el-form-item>
          </div>
          <div class="actions">
            <el-button type="primary" :loading="rateSyncSaving" @click="saveRateSync">保存并应用</el-button>
            <el-button :loading="rateSyncRunning" :disabled="!rateSync.enabled" @click="doRateSync">立即执行一次</el-button>
          </div>
        </el-form>
        <el-alert
          v-if="rateSyncData?.preview_error"
          class="inline-alert"
          type="info"
          :closable="false"
          :title="rateSyncData.preview_error"
        />
        <pre v-if="rateSyncResult">{{ JSON.stringify(rateSyncResult, null, 2) }}</pre>
        <details v-if="rateSyncData?.recent_logs?.length" class="rate-sync-logs">
          <summary>查看最近执行日志</summary>
          <pre>{{ rateSyncData.recent_logs.join('\n') }}</pre>
        </details>
      </el-card>

      <el-card shadow="never" class="frp-card">
        <template #header>
          <div class="card-heading">
            <span>frp 隧道</span>
            <el-button text @click="loadFrpConfig">读取配置</el-button>
          </div>
        </template>
        <el-input
          v-model="frpText"
          type="textarea"
          :rows="10"
          spellcheck="false"
          placeholder="代理返回的 tunnels JSON"
        />
        <div class="actions">
          <el-button type="primary" @click="applyFrp">保存配置</el-button>
          <el-button @click="doFrpRestart">重启 frpc</el-button>
        </div>
        <pre v-if="frpResult">{{ JSON.stringify(frpResult, null, 2) }}</pre>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  applyFrpConfig,
  connectWifi,
  disconnectWifi,
  getChargeGuard,
  getCloudflareStatus,
  getRateSyncStatus,
  getFrpConfig,
  getWifiStatus,
  restartFrp,
  runRateSync,
  scanWifi,
  setChargeGuard,
  configureRateSync
} from '@/k20mon/api/metrics'

defineOptions({ name: 'K20MonOperations' })

const charge = reactive({
  enabled: false,
  low_threshold: 80,
  resume_threshold: 65,
  low_current_ua: 100000,
  normal_current_ua: 3000000
})
const chargeResult = ref(null)
const wifi = reactive({ autoSelect: true, interface: '', ssid: '', password: '' })
const wifiResult = ref(null)
const wifiStatus = ref(null)
const wifiLoading = ref(false)
const cloudflare = ref(null)
const cloudflareLoading = ref(false)
const frpText = ref('[]')
const frpResult = ref(null)
const rateSyncStatus = ref(null)
const rateSyncLoading = ref(false)
const rateSyncSaving = ref(false)
const rateSyncRunning = ref(false)
const rateSyncResult = ref(null)
const rateSync = reactive({
  enabled: false,
  mode: 'instant',
  margin: 0.35,
  buffer: 0.05,
  probe: true,
  cost_select: 'max',
  probe_timeout: 15,
  lookback_hours: 24,
  min_delta: 0.02,
  step: 0.01,
  account_id: null,
  group_id: 2,
  interval: '60s'
})

const wifiNetworks = computed(() => wifiResult.value?.networks || [])
const cloudflareIngress = computed(() => cloudflare.value?.cloudflare?.ingress || [])
const rateSyncData = computed(() => rateSyncStatus.value?.rate_sync || null)
const rateSyncServiceAlert = computed(() => {
  const service = rateSyncData.value?.service
  if (service?.active) {
    return { type: 'warning', title: '定价任务正在执行' }
  }
  if (service?.result === 'success' || service?.exit_status === '0' || service?.exit_status === 0) {
    return { type: 'success', title: '定价任务最近一次执行成功' }
  }
  if (service?.result && service.result !== 'success') {
    return { type: 'error', title: `定价任务最近一次失败（${service.result}）` }
  }
  return { type: 'info', title: '暂无可用的定价执行记录' }
})
const wifiAddress = computed(() => {
  const ip = wifiStatus.value?.wifi?.ip
  return ip ? `http://${ip}:8899` : ''
})

const confirmAction = async (message) => {
  try {
    await ElMessageBox.confirm(message, '确认操作', {
      type: 'warning',
      confirmButtonText: '继续',
      cancelButtonText: '取消'
    })
    return true
  } catch {
    return false
  }
}

const loadChargeGuard = async () => {
  try {
    const result = await getChargeGuard()
    chargeResult.value = result
    if (result?.config) Object.assign(charge, result.config)
  } catch (error) {
    chargeResult.value = { ok: false, error: error?.message || '读取充电守护配置失败' }
  }
}

const saveChargeGuard = async () => {
  if (!(await confirmAction('确定要保存充电守护配置吗？'))) return
  try {
    chargeResult.value = await setChargeGuard({ ...charge })
  } catch (error) {
    chargeResult.value = { ok: false, error: error?.message || '保存充电守护配置失败' }
  }
}

const doWifiScan = async () => {
  try {
    const interfaceName = wifi.autoSelect ? undefined : wifi.interface
    wifiResult.value = await scanWifi(interfaceName ? { interface: interfaceName } : undefined)
    if (wifiResult.value?.interface && !wifi.autoSelect) wifi.interface = wifiResult.value.interface
  } catch (error) {
    wifiResult.value = { ok: false, error: error?.message || '扫描 Wi-Fi 失败' }
  }
}

const doWifiConnect = async () => {
  if (!(await confirmAction(`确定要连接 Wi-Fi「${wifi.ssid || '未命名网络'}」吗？`))) return
  try {
    const result = await connectWifi({
      interface: wifi.autoSelect ? '' : wifi.interface,
      ssid: wifi.ssid,
      password: wifi.password
    })
    wifi.password = ''
    wifiResult.value = result
    await loadWifiStatus()
  } catch (error) {
    wifiResult.value = { ok: false, error: error?.message || '连接 Wi-Fi 失败' }
  }
}

const doWifiDisconnect = async () => {
  if (!(await confirmAction(`确定要断开接口「${wifi.interface || '自动选择'}」吗？`))) return
  try {
    wifiResult.value = await disconnectWifi({ interface: wifi.autoSelect ? '' : wifi.interface })
    await loadWifiStatus()
  } catch (error) {
    wifiResult.value = { ok: false, error: error?.message || '断开 Wi-Fi 失败' }
  }
}

const loadWifiStatus = async () => {
  wifiLoading.value = true
  try {
    wifiStatus.value = await getWifiStatus()
    const activeInterface = wifiStatus.value?.wifi?.interface
    if (activeInterface && wifi.autoSelect) wifi.interface = activeInterface
  } catch (error) {
    wifiStatus.value = { ok: false, error: error?.message || '读取 Wi-Fi 状态失败' }
  } finally {
    wifiLoading.value = false
  }
}

const copyWifiAddress = async () => {
  if (!wifiAddress.value) return
  try {
    await navigator.clipboard.writeText(wifiAddress.value)
    ElMessage.success('内网访问地址已复制')
  } catch {
    ElMessage.warning(wifiAddress.value)
  }
}

const loadCloudflareStatus = async () => {
  cloudflareLoading.value = true
  try {
    cloudflare.value = await getCloudflareStatus()
  } catch (error) {
    cloudflare.value = { ok: false, error: error?.message || '读取 Cloudflare 状态失败' }
  } finally {
    cloudflareLoading.value = false
  }
}

const loadFrpConfig = async () => {
  try {
    const result = await getFrpConfig()
    frpResult.value = result
    const tunnels = result?.tunnels
    if (tunnels) frpText.value = JSON.stringify(tunnels, null, 2)
  } catch (error) {
    frpResult.value = { ok: false, error: error?.message || '读取 frp 配置失败' }
  }
}

const applyFrp = async () => {
  if (!(await confirmAction('确定要应用新的 frp 隧道配置吗？'))) return
  let tunnels
  try {
    tunnels = JSON.parse(frpText.value)
  } catch {
    frpResult.value = { ok: false, error: 'frp 配置必须是合法 JSON' }
    return
  }
  if (!Array.isArray(tunnels) && tunnels && typeof tunnels === 'object') {
    tunnels = Object.entries(tunnels).map(([name, config]) => ({ name, ...config }))
  }
  try {
    frpResult.value = await applyFrpConfig({ tunnels })
  } catch (error) {
    frpResult.value = { ok: false, error: error?.message || '保存 frp 配置失败' }
  }
}

const doFrpRestart = async () => {
  if (!(await confirmAction('确定要重启 frpc 吗？现有隧道可能短暂中断。'))) return
  try {
    frpResult.value = await restartFrp()
  } catch (error) {
    frpResult.value = { ok: false, error: error?.message || '重启 frpc 失败' }
  }
}

const hydrateRateSync = (result) => {
  const config = result?.rate_sync?.config
  if (config) Object.assign(rateSync, config)
}

const loadRateSyncStatus = async () => {
  rateSyncLoading.value = true
  try {
    rateSyncStatus.value = await getRateSyncStatus()
    hydrateRateSync(rateSyncStatus.value)
  } catch (error) {
    rateSyncResult.value = { ok: false, error: error?.message || '读取动态定价状态失败' }
  } finally {
    rateSyncLoading.value = false
  }
}

const saveRateSync = async () => {
  if (!(await confirmAction('保存后会修改 systemd 定时任务，并可能更新外部定价脚本的售价。确定继续吗？'))) return
  rateSyncSaving.value = true
  try {
    rateSyncResult.value = await configureRateSync({ ...rateSync })
    rateSyncStatus.value = rateSyncResult.value
    hydrateRateSync(rateSyncStatus.value)
  } catch (error) {
    rateSyncResult.value = { ok: false, error: error?.message || '保存动态定价配置失败' }
  } finally {
    rateSyncSaving.value = false
  }
}

const doRateSync = async () => {
  if (!(await confirmAction('立即执行会写入外部定价脚本的售价和风控阈值，确定继续吗？'))) return
  rateSyncRunning.value = true
  try {
    rateSyncResult.value = await runRateSync()
    rateSyncStatus.value = rateSyncResult.value
    hydrateRateSync(rateSyncStatus.value)
  } catch (error) {
    rateSyncResult.value = { ok: false, error: error?.message || '立即执行动态定价失败' }
  } finally {
    rateSyncRunning.value = false
  }
}

onMounted(() => {
  loadChargeGuard()
  loadWifiStatus()
  loadCloudflareStatus()
  loadFrpConfig()
  loadRateSyncStatus()
})
</script>

<style scoped>
.operations-page {
  min-height: 100%;
  padding: 20px;
  background: var(--el-bg-color-page);
}

.page-heading h1 {
  margin: 4px 0;
  color: var(--el-text-color-primary);
  font-size: 24px;
}

.page-heading p,
.eyebrow {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.eyebrow {
  color: var(--el-color-primary);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0;
}

.operation-alert {
  margin: 16px 0;
}

.operation-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.frp-card {
  grid-column: 1 / -1;
}

.rate-sync-card {
  grid-column: 1 / -1;
}

.rate-sync-form {
  margin-top: 16px;
}

.rate-sync-logs {
  margin-top: 16px;
}

.rate-sync-logs summary {
  color: var(--el-color-primary);
  cursor: pointer;
  font-size: 13px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}

.card-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.status-strip {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
  padding: 10px 12px;
  border-left: 3px solid var(--el-color-info);
  background: var(--el-fill-color-light);
}

.status-strip.is-online {
  border-left-color: var(--el-color-success);
}

.status-strip strong,
.status-strip span {
  display: block;
}

.status-strip strong {
  color: var(--el-text-color-primary);
  font-size: 14px;
}

.status-strip span {
  margin-top: 3px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.status-strip .wifi-ip {
  color: var(--el-color-primary);
  font-weight: 600;
}

.status-actions {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.el-select {
  width: 100%;
}

.wifi-table,
.cloudflare-table {
  margin-top: 16px;
}

.detail-list {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}

.detail-list > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.detail-list span {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.detail-list strong {
  color: var(--el-text-color-primary);
  font-size: 13px;
  font-weight: 600;
  text-align: right;
  overflow-wrap: anywhere;
}

.inline-alert {
  margin-top: 16px;
}

pre {
  max-height: 240px;
  margin: 16px 0 0;
  padding: 12px;
  overflow: auto;
  color: var(--el-text-color-regular);
  background: var(--el-fill-color-light);
  font-size: 12px;
  white-space: pre-wrap;
}

@media (max-width: 760px) {
  .operations-page {
    padding: 12px;
  }

  .operation-grid,
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
