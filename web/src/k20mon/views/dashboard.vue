<template>
  <div class="k20mon-page">
    <div class="k20mon-toolbar">
      <div>
        <div class="eyebrow"><span class="eyebrow-mark"></span>K20MON / DEVICE HEALTH</div>
        <h1>设备仪表盘</h1>
        <div class="dashboard-subline">
          <p v-if="current">最近采样：{{ formatTime(current.timestamp) }}</p>
          <p v-else>等待设备采集数据</p>
          <span class="sample-status" :class="`is-${connectionState}`">
            <span class="sample-status-dot"></span>{{ connectionLabel }}
          </span>
        </div>
      </div>
      <el-button class="refresh-button" type="primary" @click="refresh">
        <el-icon><Refresh /></el-icon>
        刷新
      </el-button>
    </div>

    <el-card shadow="never" class="wifi-access-card">
      <div class="wifi-access-head">
        <div>
          <div class="wifi-access-title">
            <el-icon><Connection /></el-icon>
            <span>Wi-Fi 内网访问</span>
          </div>
          <div class="wifi-access-subtitle">
            {{ wifiStatusLabel }}
            <span v-if="wifiLastUpdated"> · 更新于 {{ formatTime(wifiLastUpdated) }}</span>
          </div>
        </div>
        <el-button
          v-if="wifiAddress"
          class="wifi-copy-button"
          type="primary"
          @click="copyWifiAddress"
        >
          <el-icon><CopyDocument /></el-icon>
          复制访问地址
        </el-button>
      </div>
      <div class="wifi-access-grid">
        <div>
          <span>网络</span>
          <strong>{{ wifiStatus?.wifi?.ssid || wifiStatus?.wifi?.connection || '--' }}</strong>
        </div>
        <div>
          <span>接口</span>
          <strong>{{ wifiStatus?.wifi?.interface || '--' }}</strong>
        </div>
        <div>
          <span>IP 地址</span>
          <strong>{{ wifiStatus?.wifi?.ip || '--' }}</strong>
        </div>
        <div class="wifi-address-value">
          <span>内网访问</span>
          <strong>{{ wifiAddress || '--' }}</strong>
        </div>
      </div>
      <div v-if="wifiError" class="wifi-access-error">{{ wifiError }}</div>
    </el-card>

    <el-alert
      v-if="current?.warnings?.length"
      class="k20mon-alert"
      type="warning"
      :closable="false"
      show-icon
      title="部分指标不可用"
    >
      <template #default>
        <span v-for="warning in current.warnings" :key="warning">{{ warning }}</span>
      </template>
    </el-alert>

    <div class="dashboard-context">
      <div class="context-item">
        <span class="context-kicker">历史采样覆盖</span>
        <strong>{{ batteryHistoryWindowLabel }}</strong>
      </div>
      <div class="context-item">
        <span class="context-kicker">历史样本</span>
        <strong>{{ batteryHistory.sampleCount ?? '--' }} 条</strong>
      </div>
      <div class="context-item context-note">
        <span>充放电容量按当前数据库保留期内的连续采样估算，数据越完整，结果越稳定。</span>
      </div>
    </div>

    <div class="metric-grid">
      <el-card shadow="never" class="metric-card metric-card-battery">
        <template #header><span>电池</span></template>
        <div class="metric-value">{{ percent(current?.battery?.capacityPercent) }}</div>
        <div class="metric-label">{{ current?.battery?.status || '未知状态' }}</div>
        <div class="metric-meta">
          {{ number(current?.battery?.voltageV, 2, 'V') }}
          · {{ number(current?.battery?.currentMa, 0, 'mA') }}
        </div>
        <div class="metric-meta">
          满充 {{ chargeCapacity(current?.battery) }}
          · 健康 {{ number(current?.battery?.healthPercent, 1, '%') }}
        </div>
      </el-card>
      <el-card shadow="never" class="metric-card metric-card-charger">
        <template #header><span>充电</span></template>
        <div class="metric-value">{{ current?.charger?.pdNegotiated ? 'PD' : '普通' }}</div>
        <div class="metric-label">
          {{ current?.charger?.online === true ? '已连接' : '未连接' }}
        </div>
        <div class="metric-meta">
          {{ number(current?.charger?.inputPowerW, 2, 'W') }}
          · {{ current?.charger?.usbType || '未知协议' }}
        </div>
      </el-card>
      <el-card shadow="never" class="metric-card metric-card-power">
        <template #header><span>手机功耗</span></template>
        <div class="metric-value">{{ number(displayedConsumptionW, 2, 'W') }}</div>
        <div class="metric-label">{{ consumptionLabel }}</div>
        <div class="metric-meta">
          电池 {{ number(current?.battery?.powerW, 2, 'W') }}
          · 输入 {{ number(current?.charger?.inputPowerW, 2, 'W') }}
        </div>
      </el-card>
      <el-card shadow="never" class="metric-card metric-card-temperature">
        <template #header><span>最高温度</span></template>
        <div class="metric-value">{{ number(current?.system?.hottestC, 1, '°C') }}</div>
        <div class="metric-label">{{ current?.system?.hottestZone || '无温度区数据' }}</div>
        <div class="metric-meta">
          电池 {{ number(current?.battery?.temperatureC, 1, '°C') }}
        </div>
      </el-card>
      <el-card shadow="never" class="metric-card metric-card-system">
        <template #header><span>系统负载</span></template>
        <div class="metric-value">{{ number(current?.system?.cpuUsagePercent, 1, '%') }}</div>
        <div class="metric-label">
          Load {{ number(current?.system?.load1, 2, '') }}
        </div>
        <div class="metric-meta">
          {{ current?.system?.cpuCores ?? '--' }} 核
          · {{ number(current?.system?.frequencyMhz?.averageMhz, 0, 'MHz') }}
        </div>
      </el-card>
      <el-card shadow="never" class="metric-card metric-card-gpu gpu-card">
        <template #header>
          <div class="card-heading">
            <span>显卡 GPU</span>
            <el-tag size="small" :type="gpu.available ? 'success' : 'info'">
              {{ gpu.available ? '在线' : '不可用' }}
            </el-tag>
          </div>
        </template>
        <div class="metric-value">{{ number(gpu.currentFrequencyMhz, 0, 'MHz') }}</div>
        <div class="metric-label">{{ gpuLabel }}</div>
        <div class="metric-meta">
          最大 {{ number(gpu.maximumFrequencyMhz, 0, 'MHz') }}
          · {{ gpu.governor || '未知调速器' }}
          <span v-if="gpu.utilizationPercent !== null && gpu.utilizationPercent !== undefined">
            · 使用率 {{ number(gpu.utilizationPercent, 0, '%') }}
          </span>
        </div>
      </el-card>
      <el-card shadow="never" class="metric-card metric-card-memory">
        <template #header><span>内存</span></template>
        <div class="metric-value">{{ number(current?.system?.memory?.usedPercent, 1, '%') }}</div>
        <div class="metric-label">
          {{ number(current?.system?.memory?.usedMb, 0, 'MB') }} 已使用
        </div>
        <div class="metric-meta">
          可用 {{ number(current?.system?.memory?.availableMb, 0, 'MB') }}
        </div>
      </el-card>
      <el-card shadow="never" class="metric-card metric-card-network">
        <template #header><span>网络</span></template>
        <div class="metric-value">{{ current?.network?.defaultInterface || '--' }}</div>
        <div class="metric-label">
          ↓ {{ rate(current?.network?.rxBps) }} · ↑ {{ rate(current?.network?.txBps) }}
        </div>
        <div class="metric-meta">运行 {{ uptime(current?.system?.uptimeSeconds) }}</div>
      </el-card>
    </div>

    <div class="detail-grid">
      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>实时环境摘要（最近 24 小时）</span>
            <el-icon><DataLine /></el-icon>
          </div>
        </template>
        <el-table :data="summaryRows" size="small">
          <el-table-column prop="name" label="指标" min-width="150" />
          <el-table-column label="平均" min-width="110">
            <template #default="{ row }">{{ row.avg }}</template>
          </el-table-column>
          <el-table-column label="最低" min-width="110">
            <template #default="{ row }">{{ row.min }}</template>
          </el-table-column>
          <el-table-column label="最高" min-width="110">
            <template #default="{ row }">{{ row.max }}</template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>接口状态</span>
            <el-icon><Connection /></el-icon>
          </div>
        </template>
        <el-table :data="current?.network?.interfaces || []" size="small">
          <el-table-column prop="name" label="接口" min-width="100" />
          <el-table-column prop="state" label="状态" min-width="90" />
          <el-table-column label="接收" min-width="110">
            <template #default="{ row }">{{ bytes(row.rxBytes) }}</template>
          </el-table-column>
          <el-table-column label="发送" min-width="110">
            <template #default="{ row }">{{ bytes(row.txBytes) }}</template>
          </el-table-column>
          <el-table-column label="链路" min-width="100">
            <template #default="{ row }">{{ number(row.speedMbps, 0, 'Mbps') }}</template>
          </el-table-column>
        </el-table>
      </el-card>
    </div>

    <div class="detail-grid detail-grid-three">
      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>CPU 频率与调速器</span>
            <el-icon><Cpu /></el-icon>
          </div>
        </template>
        <div class="detail-list">
          <div><span>平均频率</span><strong>{{ number(current?.system?.frequencyMhz?.averageMhz, 0, 'MHz') }}</strong></div>
          <div><span>最低频率</span><strong>{{ number(current?.system?.frequencyMhz?.minimumMhz, 0, 'MHz') }}</strong></div>
          <div><span>最高频率</span><strong>{{ number(current?.system?.frequencyMhz?.maximumMhz, 0, 'MHz') }}</strong></div>
          <div><span>调速器</span><strong>{{ governorsText }}</strong></div>
        </div>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>显卡详情</span>
            <el-icon><Monitor /></el-icon>
          </div>
        </template>
        <div class="detail-list">
          <div><span>型号</span><strong>{{ gpu.model || '--' }}</strong></div>
          <div><span>驱动</span><strong>{{ gpu.driver || '--' }}</strong></div>
          <div><span>设备</span><strong>{{ gpu.device || '--' }}</strong></div>
          <div><span>温度</span><strong>{{ number(gpu.temperatureC, 1, '°C') }}</strong></div>
        </div>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>内存与 Swap</span>
            <el-icon><Odometer /></el-icon>
          </div>
        </template>
        <div class="progress-row">
          <div class="progress-title"><span>内存</span><strong>{{ number(current?.system?.memory?.usedPercent, 1, '%') }}</strong></div>
          <el-progress :percentage="progress(current?.system?.memory?.usedPercent)" :show-text="false" />
          <div class="progress-meta">
            {{ number(current?.system?.memory?.usedMb, 0, 'MB') }} / {{ number(current?.system?.memory?.totalMb, 0, 'MB') }}
          </div>
        </div>
        <div class="progress-row">
          <div class="progress-title"><span>Swap</span><strong>{{ number(current?.system?.memory?.swapUsedPercent, 1, '%') }}</strong></div>
          <el-progress :percentage="progress(current?.system?.memory?.swapUsedPercent)" :show-text="false" status="warning" />
          <div class="progress-meta">
            {{ number(current?.system?.memory?.swapUsedMb, 0, 'MB') }} / {{ number(current?.system?.memory?.swapTotalMb, 0, 'MB') }}
          </div>
        </div>
      </el-card>
    </div>

    <div class="detail-grid detail-grid-four">
      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>电池容量与健康</span>
            <el-icon><Lightning /></el-icon>
          </div>
        </template>
        <div class="detail-list">
          <div><span>当前电量</span><strong>{{ percent(current?.battery?.capacityPercent) }}</strong></div>
          <div><span>当前功耗</span><strong>{{ number(current?.battery?.consumptionW, 2, 'W') }}</strong></div>
          <div><span>剩余电荷</span><strong>{{ chargeCapacity(current?.battery, 'now') }}</strong></div>
          <div><span>{{ fullCapacityLabel }}</span><strong>{{ chargeCapacity(current?.battery, 'full') }}</strong></div>
          <div><span>设计容量</span><strong>{{ chargeCapacity(current?.battery, 'design') }}</strong></div>
          <div><span>健康度</span><strong>{{ number(batteryHealth, 1, '%') }}</strong></div>
          <div>
            <span>{{ current?.battery?.cycleCount != null ? '系统循环次数' : `${batteryHistoryWindowLabel}等效循环` }}</span>
            <strong>{{ current?.battery?.cycleCount ?? number(batteryHistory.equivalentCycles, 2, ' 次') }}</strong>
          </div>
          <div><span>{{ batteryHistoryWindowLabel }}充入</span><strong>{{ historicalCapacity(batteryHistory.chargeAddedUah) }}</strong></div>
          <div><span>{{ batteryHistoryWindowLabel }}放电</span><strong>{{ historicalCapacity(batteryHistory.dischargeUsedUah) }}</strong></div>
          <div><span>{{ batteryHistoryWindowLabel }}放电能量</span><strong>{{ number(batteryHistory.dischargeEnergyWh, 2, ' Wh') }}</strong></div>
          <div><span>{{ batteryHistoryWindowLabel }}平均功耗</span><strong>{{ number(batteryHistory.averageConsumptionW, 2, ' W') }}</strong></div>
        </div>
        <div class="metric-meta battery-source-note">
          {{ batteryCapacitySource }}
        </div>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>充电器与 Type-C</span>
            <el-icon><Lightning /></el-icon>
          </div>
        </template>
        <div class="detail-list">
          <div><span>状态</span><strong>{{ current?.charger?.status || '--' }}</strong></div>
          <div><span>协议</span><strong>{{ current?.charger?.usbType || '--' }}</strong></div>
          <div><span>输入功率</span><strong>{{ number(current?.charger?.inputPowerW, 2, 'W') }}</strong></div>
          <div><span>接口</span><strong>{{ current?.charger?.typeC?.name || '--' }}</strong></div>
          <div><span>Type-C</span><strong>{{ current?.charger?.typeC?.online ? '在线' : '离线' }}</strong></div>
          <div><span>限流</span><strong>{{ current?.charger?.throttled ? current.charger.throttledReason : '未触发' }}</strong></div>
        </div>
      </el-card>

      <el-card shadow="never" class="temperature-card">
        <template #header>
          <div class="card-heading">
            <span>温度区</span>
            <el-icon><DataAnalysis /></el-icon>
          </div>
        </template>
        <el-table :data="temperatureRows" size="small" max-height="260">
          <el-table-column prop="name" label="区域" min-width="150" />
          <el-table-column label="温度" min-width="100">
            <template #default="{ row }">{{ number(row.value, 1, '°C') }}</template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-heading">
            <span>设备概况</span>
            <el-icon><InfoFilled /></el-icon>
          </div>
        </template>
        <div class="detail-list">
          <div><span>CPU 核心</span><strong>{{ current?.system?.cpuCores ?? '--' }}</strong></div>
          <div><span>运行时间</span><strong>{{ uptime(current?.system?.uptimeSeconds) }}</strong></div>
          <div><span>默认网卡</span><strong>{{ current?.network?.defaultInterface || '--' }}</strong></div>
          <div><span>下载速率</span><strong>{{ rate(current?.network?.rxBps) }}</strong></div>
          <div><span>上传速率</span><strong>{{ rate(current?.network?.txBps) }}</strong></div>
          <div><span>采样时间</span><strong>{{ formatTime(current?.timestamp) }}</strong></div>
        </div>
      </el-card>

    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  Connection,
  CopyDocument,
  Cpu,
  DataAnalysis,
  DataLine,
  InfoFilled,
  Lightning,
  Monitor,
  Odometer,
  Refresh
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getCurrentMetrics, getMetricsSummary, getWifiStatus } from '@/k20mon/api/metrics'
import { readDashboardCache, writeDashboardCache } from '@/k20mon/utils/dashboard-cache'
import { createDashboardRefresher } from '@/k20mon/utils/dashboard-refresh'

defineOptions({ name: 'K20MonDashboard' })

const CURRENT_CACHE_KEY = 'k20mon.dashboard.current.v1'
const SUMMARY_CACHE_KEY = 'k20mon.dashboard.summary.v1'
const WIFI_CACHE_KEY = 'k20mon.dashboard.wifi.v1'
const current = ref(readDashboardCache(window.localStorage, CURRENT_CACHE_KEY))
const summary = ref(readDashboardCache(window.localStorage, SUMMARY_CACHE_KEY))
const wifiStatus = ref(readDashboardCache(window.localStorage, WIFI_CACHE_KEY))
let timer = null
let wifiTimer = null
const wifiError = ref('')

const battery = computed(() => current.value?.battery || {})
const batteryHistory = computed(() => summary.value?.batteryHistory || {})
const batteryHealth = computed(() => battery.value.healthPercent ?? batteryHistory.value.estimatedHealthPercent)
const displayedConsumptionW = computed(() => battery.value.consumptionW ?? batteryHistory.value.averageConsumptionW)
const batteryHistoryWindowLabel = computed(() => {
  const hours = Number(batteryHistory.value.windowHours)
  if (!Number.isFinite(hours) || hours <= 0) return '历史'
  if (hours < 24) return `近 ${Math.max(1, Math.round(hours))} 小时`
  const days = hours / 24
  return `近 ${days >= 10 ? Math.round(days) : Number(days.toFixed(1))} 天`
})
const connectionState = computed(() => {
  if (!current.value?.timestamp) return 'offline'
  const ageSeconds = (Date.now() - new Date(current.value.timestamp).getTime()) / 1000
  if (!Number.isFinite(ageSeconds) || ageSeconds > 120) return 'offline'
  if (ageSeconds > 20) return 'delayed'
  return 'live'
})
const connectionLabel = computed(() => ({
  live: '实时采样',
  delayed: '数据稍有延迟',
  offline: '显示最后快照'
}[connectionState.value]))
const gpu = computed(() => current.value?.system?.gpu || {})
const gpuLabel = computed(() => gpu.value.model || gpu.value.driver || gpu.value.device || 'GPU 数据不可用')
const consumptionLabel = computed(() => {
  if (battery.value.consumptionW === null || battery.value.consumptionW === undefined) {
    return batteryHistory.value.averageConsumptionW == null ? '暂无可用功耗数据' : `${batteryHistoryWindowLabel.value}记录平均`
  }
  return battery.value.status === 'Charging' ? '充电中估算负载' : '当前设备负载'
})
const governorsText = computed(() => current.value?.system?.governors?.length
  ? current.value.system.governors.join('、')
  : '--')
const temperatureRows = computed(() => Object.entries(current.value?.system?.temperatures || {})
  .map(([name, value]) => ({ name, value }))
  .sort((left, right) => right.value - left.value))

const refreshMetrics = createDashboardRefresher({
  getCurrentMetrics,
  getMetricsSummary,
  onCurrent: (value) => {
    current.value = value
    writeDashboardCache(window.localStorage, CURRENT_CACHE_KEY, value)
  },
  onSummary: (value) => {
    summary.value = value
    writeDashboardCache(window.localStorage, SUMMARY_CACHE_KEY, value)
  },
  onError: (error) => console.warn('K20Mon metrics refresh failed', error)
})
const refresh = () => refreshMetrics({ forceSummary: true })

const loadWifiStatus = async () => {
  try {
    let payload = await getWifiStatus()
    // Accept both control responses and an extra API envelope from older deployments.
    for (let index = 0; index < 2 && payload?.data && !payload?.wifi; index += 1) {
      payload = payload.data
    }
    if (payload?.wifi) {
      wifiStatus.value = payload
      writeDashboardCache(window.localStorage, WIFI_CACHE_KEY, payload)
      wifiError.value = ''
    } else {
      wifiError.value = 'Wi-Fi 状态接口未返回采集数据'
    }
  } catch (error) {
    wifiError.value = error?.message || '读取 Wi-Fi 状态失败，当前显示最近一次快照'
  }
}

const wifiAddress = computed(() => {
  const ip = wifiStatus.value?.wifi?.ip
  return ip ? `http://${ip}:8899` : ''
})

const wifiLastUpdated = computed(() => (
  wifiStatus.value?.wifi?.updated_at
  || wifiStatus.value?.generated_at
  || null
))

const wifiStatusLabel = computed(() => {
  if (wifiStatus.value?.wifi?.connected) return '已连接，地址可用于内网访问'
  if (wifiError.value) return '采集异常，正在保留最近一次结果'
  return '未连接 Wi-Fi 或尚未采集到地址'
})

const copyWifiAddress = async () => {
  if (!wifiAddress.value) return
  try {
    await navigator.clipboard.writeText(wifiAddress.value)
    ElMessage.success('内网访问地址已复制')
  } catch {
    ElMessage.warning(wifiAddress.value)
  }
}

const number = (value, digits = 1, suffix = '') => {
  if (value === null || value === undefined || Number.isNaN(Number(value))) return '--'
  return `${Number(value).toFixed(digits)}${suffix}`
}

const percent = (value) => value === null || value === undefined ? '--' : `${value}%`

const chargeCapacity = (batteryValue, kind = 'full') => {
  const chargeKey = {
    now: 'chargeNowUah',
    full: 'chargeFullUah',
    design: 'chargeFullDesignUah'
  }[kind]
  const charge = batteryValue?.[chargeKey]
  if (charge !== null && charge !== undefined) return `${(Number(charge) / 1000).toFixed(0)} mAh`
  if (kind === 'now' && batteryHistory.value.estimatedRemainingUah !== null && batteryHistory.value.estimatedRemainingUah !== undefined) {
    return `约 ${(Number(batteryHistory.value.estimatedRemainingUah) / 1000).toFixed(0)} mAh`
  }
  if (kind === 'full' && batteryHistory.value.estimatedFullUah !== null && batteryHistory.value.estimatedFullUah !== undefined) {
    return `约 ${(Number(batteryHistory.value.estimatedFullUah) / 1000).toFixed(0)} mAh`
  }

  const energyKey = {
    now: 'energyNowUwh',
    full: 'energyFullUwh',
    design: 'energyFullDesignUwh'
  }[kind]
  const energy = batteryValue?.[energyKey]
  return energy === null || energy === undefined ? '--' : `${(Number(energy) / 1000000).toFixed(2)} Wh`
}

const batteryCapacitySource = computed(() => {
  const historyMethod = batteryHistory.value.usedDesignFallback
    ? '无电流值区间按 SOC 变化与设计容量估算。'
    : `充放电统计来自 MySQL ${batteryHistoryWindowLabel.value}的连续采样。`
  if (battery.value.chargeFullUah !== null && battery.value.chargeFullUah !== undefined) {
    return `满充容量与健康度来自系统电量计实测值；${historyMethod}`
  }
  if (battery.value.energyFullUwh !== null && battery.value.energyFullUwh !== undefined) {
    return `满充能量来自系统电量计；${historyMethod}`
  }
  if (batteryHistory.value.estimatedFullUah !== null && batteryHistory.value.estimatedFullUah !== undefined) {
    return `满充容量和健康度为连续充放电电流估算（${batteryHistory.value.capacityEstimateSamples} 个区间），不是驱动实测值；${historyMethod}`
  }
  return `驱动未提供实测满充容量/电流；剩余电荷按电量百分比与设计容量估算。连续记录到电流和至少 5% 充放电变化后才可估算健康度；${historyMethod}`
})
const fullCapacityLabel = computed(() => {
  if (battery.value.chargeFullUah != null || battery.value.energyFullUwh != null) return '实测满充'
  return batteryHistory.value.estimatedFullUah != null ? '估算满充' : '满充估算'
})

const historicalCapacity = (value) => value === null || value === undefined
  ? '--'
  : `${(Number(value) / 1000).toFixed(0)} mAh`

const progress = (value) => {
  if (value === null || value === undefined || Number.isNaN(Number(value))) return 0
  return Math.max(0, Math.min(100, Number(value)))
}

const rate = (value) => {
  if (value === null || value === undefined) return '--'
  const numeric = Number(value)
  if (numeric >= 1024 * 1024) return `${(numeric / 1024 / 1024).toFixed(1)} MB/s`
  return `${(numeric / 1024).toFixed(1)} KB/s`
}

const bytes = (value) => {
  if (value === null || value === undefined) return '--'
  const numeric = Number(value)
  if (numeric >= 1024 ** 3) return `${(numeric / 1024 ** 3).toFixed(1)} GB`
  return `${(numeric / 1024 ** 2).toFixed(1)} MB`
}

const uptime = (value) => {
  if (value === null || value === undefined) return '--'
  const hours = Math.floor(Number(value) / 3600)
  const minutes = Math.floor((Number(value) % 3600) / 60)
  return `${hours}小时${minutes}分`
}

const formatTime = (value) => value ? new Date(value).toLocaleString('zh-CN') : '--'

const summaryValue = (value, digits = 1, suffix = '') => {
  if (!value || value.avg === null || value.avg === undefined) return { avg: '--', min: '--', max: '--' }
  return {
    avg: number(value.avg, digits, suffix),
    min: number(value.min, digits, suffix),
    max: number(value.max, digits, suffix)
  }
}

const summaryRows = computed(() => {
  if (!summary.value) return []
  return [
    { name: '电量', ...summaryValue(summary.value.batteryCapacity, 0, '%') },
    { name: '电池功率', ...summaryValue(summary.value.batteryPowerW, 2, 'W') },
    { name: '手机功耗', ...summaryValue(summary.value.batteryConsumptionW, 2, 'W') },
    { name: '电池温度', ...summaryValue(summary.value.batteryTemperatureC, 1, '°C') },
    { name: 'CPU 使用率', ...summaryValue(summary.value.cpuUsagePercent, 1, '%') },
    { name: '最高温度', ...summaryValue(summary.value.hottestTemperatureC, 1, '°C') },
    { name: '内存使用率', ...summaryValue(summary.value.memoryUsedPercent, 1, '%') }
  ]
})

onMounted(() => {
  refresh()
  loadWifiStatus()
  timer = window.setInterval(() => refreshMetrics(), 5000)
  wifiTimer = window.setInterval(loadWifiStatus, 30000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  if (wifiTimer) window.clearInterval(wifiTimer)
})
</script>

<style scoped>
.k20mon-page {
  min-height: 100%;
  padding: 24px;
  color: #17212b;
  background: #f4f7fa;
}

.k20mon-toolbar,
.card-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.k20mon-toolbar {
  margin-bottom: 18px;
}

.k20mon-toolbar h1 {
  margin: 6px 0 2px;
  color: #142230;
  font-size: 32px;
  font-weight: 760;
  letter-spacing: 0;
}

.k20mon-toolbar p,
.metric-label,
.metric-meta {
  color: #718096;
  font-size: 13px;
}

.eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: #2a72d8;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.eyebrow-mark {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #2a72d8;
  box-shadow: 0 0 0 4px rgba(42, 114, 216, 0.12);
}

.dashboard-subline {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.dashboard-subline p {
  margin: 0;
}

.sample-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #5f7285;
  font-size: 12px;
  font-weight: 600;
}

.sample-status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #94a3b8;
}

.sample-status.is-live .sample-status-dot {
  background: #1aa36f;
  box-shadow: 0 0 0 4px rgba(26, 163, 111, 0.12);
}

.sample-status.is-delayed .sample-status-dot {
  background: #d38b27;
  box-shadow: 0 0 0 4px rgba(211, 139, 39, 0.12);
}

.refresh-button {
  min-width: 88px;
  border: 0;
  background: #2468c7;
  box-shadow: 0 8px 18px rgba(36, 104, 199, 0.18);
}

.refresh-button:hover {
  background: #1e59ad;
}

.k20mon-alert {
  margin-bottom: 16px;
  border: 1px solid #f2d89c;
  border-radius: 8px;
}

.k20mon-alert span + span::before {
  content: ' · ';
}

.wifi-access-card {
  margin-bottom: 18px;
  border: 1px solid #cbdced;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 8px 22px rgba(30, 49, 68, 0.06);
}

.wifi-access-card :deep(.el-card__body) {
  padding: 16px 20px;
}

.wifi-access-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.wifi-access-title {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #244c78;
  font-size: 16px;
  font-weight: 750;
}

.wifi-access-title .el-icon {
  color: #2a72d8;
}

.wifi-access-subtitle {
  margin-top: 4px;
  color: #73869a;
  font-size: 12px;
}

.wifi-access-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}

.wifi-access-grid > div {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid #e4ebf2;
  border-radius: 6px;
  background: #f8fafc;
}

.wifi-access-grid span {
  color: #7b8a99;
  font-size: 12px;
}

.wifi-access-grid strong {
  min-width: 0;
  color: #1f3852;
  font-size: 15px;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.wifi-address-value strong {
  color: #176f58;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}

.wifi-access-error {
  margin-top: 10px;
  color: #a15b22;
  font-size: 12px;
}

.dashboard-context {
  display: flex;
  align-items: stretch;
  gap: 0;
  min-height: 76px;
  margin-bottom: 18px;
  padding: 0 4px;
  border: 1px solid #dfe7ef;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 5px 16px rgba(30, 49, 68, 0.04);
}

.context-item {
  display: flex;
  min-width: 150px;
  flex-direction: column;
  justify-content: center;
  gap: 3px;
  padding: 14px 20px;
  border-right: 1px solid #e8eef4;
}

.context-item:last-child {
  border-right: 0;
}

.context-kicker {
  color: #8291a1;
  font-size: 11px;
  font-weight: 650;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.context-item strong {
  color: #223244;
  font-size: 17px;
  font-weight: 750;
}

.context-note {
  flex: 1;
  color: #6e7e8e;
  font-size: 12px;
  line-height: 1.5;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.metric-card {
  position: relative;
  overflow: hidden;
  min-height: 172px;
  border: 1px solid #dfe7ef;
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 8px 22px rgba(30, 49, 68, 0.045);
  transition: transform 160ms ease, box-shadow 160ms ease;
}

.metric-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 26px rgba(30, 49, 68, 0.09);
}

.metric-card::before {
  position: absolute;
  top: 0;
  right: 0;
  left: 0;
  height: 3px;
  content: '';
  background: #8aa2b9;
}

.metric-card-battery::before { background: #2a72d8; }
.metric-card-charger::before { background: #1aa36f; }
.metric-card-power::before { background: #d38b27; }
.metric-card-temperature::before { background: #d95f59; }
.metric-card-system::before { background: #6a65c7; }
.metric-card-gpu::before { background: #187d87; }
.metric-card-memory::before { background: #5c83b4; }
.metric-card-network::before { background: #7c8693; }

.metric-card :deep(.el-card__header) {
  padding: 16px 20px 12px;
  color: #536476;
  font-size: 14px;
  font-weight: 700;
  border-bottom: 1px solid #edf1f5;
}

.metric-card :deep(.el-card__body) {
  padding: 15px 20px 19px;
}

.metric-value {
  margin: 2px 0 7px;
  color: #172b3e;
  font-size: 30px;
  font-weight: 780;
  letter-spacing: 0;
}

.metric-meta {
  margin-top: 12px;
  line-height: 1.5;
}

.detail-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: 16px;
  margin-top: 16px;
}

.detail-grid-three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.detail-grid-four {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.detail-grid :deep(.el-card) {
  border: 1px solid #dfe7ef;
  border-radius: 8px;
  box-shadow: 0 8px 22px rgba(30, 49, 68, 0.04);
}

.detail-grid :deep(.el-card__header) {
  padding: 15px 18px;
  color: #536476;
  font-size: 14px;
  font-weight: 700;
  border-bottom-color: #edf1f5;
}

.detail-grid :deep(.el-card__body) {
  padding: 16px 18px;
}

.detail-list {
  display: grid;
  gap: 10px;
}

.detail-list > div,
.progress-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.detail-list > div {
  display: grid;
  grid-template-columns: minmax(96px, max-content) minmax(0, 1fr);
  align-items: start;
}

.detail-list span,
.progress-meta {
  color: #7b8a99;
  font-size: 13px;
  white-space: nowrap;
}

.detail-list strong {
  min-width: 0;
  color: #263b50;
  font-size: 13px;
  font-weight: 600;
  text-align: right;
  overflow-wrap: anywhere;
}

.battery-source-note {
  margin-top: 12px;
  line-height: 1.5;
}

.progress-row + .progress-row {
  margin-top: 18px;
}

.progress-title {
  margin-bottom: 6px;
  color: var(--el-text-color-regular);
  font-size: 13px;
}

.progress-meta {
  margin-top: 5px;
  text-align: right;
}

@media (max-width: 1100px) {
  .metric-grid,
  .detail-grid,
  .detail-grid-three,
  .detail-grid-four {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 680px) {
  .k20mon-page {
    padding: 12px;
  }

  .k20mon-toolbar h1 {
    font-size: 26px;
  }

  .dashboard-context {
    flex-wrap: wrap;
    padding: 0;
  }

  .context-item {
    min-width: 50%;
    border-bottom: 1px solid #e8eef4;
  }

  .context-item:nth-child(2) {
    border-right: 0;
  }

  .context-note {
    min-width: 100%;
    border-bottom: 0;
  }

  .k20mon-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .wifi-access-head {
    align-items: flex-start;
    flex-direction: column;
  }

  .wifi-copy-button {
    width: 100%;
  }

  .wifi-access-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .metric-grid,
  .detail-grid,
  .detail-grid-three,
  .detail-grid-four {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 420px) {
  .wifi-access-grid {
    grid-template-columns: 1fr;
  }
}
</style>
