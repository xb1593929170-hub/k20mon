const getRequestClient = () => {
  if (typeof globalThis.__K20MON_REQUEST__ !== 'function') {
    throw new Error('K20Mon request client is not configured')
  }
  return globalThis.__K20MON_REQUEST__
}

/**
 * 获取 K20 主机当前只读指标。
 *
 * @returns {Promise<object>}
 */
const requestPayload = (config) => getRequestClient()(config).then((result) => {
  if (result && typeof result === 'object' && result.code !== undefined && result.data !== undefined) {
    if (result.code !== 0) throw new Error(result.msg || '请求失败')
    return result.data
  }
  return result
})

export const getCurrentMetrics = () => {
  return requestPayload({
    url: '/k20mon/metrics/current',
    method: 'get',
    donNotShowLoading: true
  })
}

export const getMetricsHistory = (params) => {
  return requestPayload({
    url: '/k20mon/metrics/history',
    method: 'get',
    params,
    donNotShowLoading: true
  })
}

export const getMetricsSummary = (params) => {
  return requestPayload({
    url: '/k20mon/metrics/summary',
    method: 'get',
    params,
    donNotShowLoading: true
  })
}

export const getChargeGuard = () => controlRequest('/k20mon/operations/charge-guard', 'get')
export const setChargeGuard = (data) => controlRequest('/k20mon/operations/charge-guard', 'put', data)
export const getWifiStatus = () => controlRequest('/k20mon/operations/wifi/status', 'get')
export const scanWifi = (params) => controlRequest('/k20mon/operations/wifi/scan', 'post', undefined, params)
export const connectWifi = (data) => controlRequest('/k20mon/operations/wifi/connect', 'post', data)
export const disconnectWifi = (data) => controlRequest('/k20mon/operations/wifi/disconnect', 'post', data)
export const getCloudflareStatus = () => controlRequest('/k20mon/operations/cloudflare/status', 'get')
export const getRateSyncStatus = () => controlRequest('/k20mon/operations/rate-sync', 'get')
export const configureRateSync = (data) => controlRequest('/k20mon/operations/rate-sync/config', 'put', data)
export const runRateSync = () => controlRequest('/k20mon/operations/rate-sync/run', 'post')
export const getServicesStatus = () => controlRequest('/k20mon/operations/services/status', 'get')
export const updateServiceOwner = (data) => controlRequest('/k20mon/operations/services/owner', 'put', data)
export const createService = (data) => controlRequest('/k20mon/operations/services', 'post', data)
export const updateService = (data) => controlRequest('/k20mon/operations/services', 'put', data)
export const deleteService = (id) => controlRequest(`/k20mon/operations/services/${encodeURIComponent(id)}`, 'delete')
export const updateServiceOrder = (ids) => controlRequest('/k20mon/operations/services/order', 'post', { ids })
export const getFrpConfig = () => controlRequest('/k20mon/operations/frp/config', 'get')
export const applyFrpConfig = (data) => controlRequest('/k20mon/operations/frp/config', 'put', data)
export const restartFrp = () => controlRequest('/k20mon/operations/frp/restart', 'post')

const controlRequest = (url, method, data, params) => requestPayload({
  url,
  method,
  data,
  params,
  donNotShowLoading: true
})
