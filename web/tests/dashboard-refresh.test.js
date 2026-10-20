import assert from 'node:assert/strict'
import { readDashboardCache, writeDashboardCache } from '../src/k20mon/utils/dashboard-cache.js'
import { createDashboardRefresher } from '../src/k20mon/utils/dashboard-refresh.js'

const deferred = () => {
  let resolve
  const promise = new Promise((done) => { resolve = done })
  return { promise, resolve }
}

const makeRefresher = (overrides = {}) => {
  const state = { current: null, summary: null, errors: [] }
  const refresh = createDashboardRefresher({
    getCurrentMetrics: async () => ({ timestamp: 'now' }),
    getMetricsSummary: async () => ({ sampleCount: 1 }),
    onCurrent: (value) => { state.current = value },
    onSummary: (value) => { state.summary = value },
    onError: (error) => { state.errors.push(error) },
    ...overrides
  })
  return { refresh, state }
}

const keepsCurrentMetricsWhenSummaryFails = async () => {
  const { refresh, state } = makeRefresher({
    getMetricsSummary: async () => { throw new Error('summary unavailable') }
  })

  await refresh({ forceSummary: true })

  assert.deepEqual(state.current, { timestamp: 'now' })
  assert.equal(state.errors.length, 1)
}

const doesNotStartOverlappingRequests = async () => {
  const current = deferred()
  const summary = deferred()
  let currentCalls = 0
  let summaryCalls = 0
  const { refresh } = makeRefresher({
    getCurrentMetrics: () => { currentCalls += 1; return current.promise },
    getMetricsSummary: () => { summaryCalls += 1; return summary.promise }
  })

  const first = refresh({ forceSummary: true })
  const second = refresh()
  await Promise.resolve()
  assert.equal(currentCalls, 1)
  assert.equal(summaryCalls, 1)

  current.resolve({ timestamp: 'now' })
  summary.resolve({ sampleCount: 1 })
  await Promise.all([first, second])
}

const refreshesSummaryAtConfiguredInterval = async () => {
  let now = 0
  let summaryCalls = 0
  const { refresh } = makeRefresher({
    now: () => now,
    summaryIntervalMs: 60_000,
    getMetricsSummary: async () => {
      summaryCalls += 1
      return { sampleCount: summaryCalls }
    }
  })

  await refresh({ forceSummary: true })
  now = 5_000
  await refresh()
  assert.equal(summaryCalls, 1)

  now = 60_000
  await refresh()
  assert.equal(summaryCalls, 2)
}

const dashboardCacheSurvivesReloadAndInvalidStorage = async () => {
  const values = new Map()
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value)
  }
  const snapshot = { timestamp: '2026-10-07T10:00:00Z', battery: { capacityPercent: 72 } }
  writeDashboardCache(storage, 'current', snapshot)
  assert.deepEqual(readDashboardCache(storage, 'current'), snapshot)
  assert.equal(readDashboardCache({ getItem: () => '{' }, 'current'), null)
  writeDashboardCache({ setItem: () => { throw new Error('storage full') } }, 'current', snapshot)
}

for (const [name, run] of [
  ['current metrics survive summary failures', keepsCurrentMetricsWhenSummaryFails],
  ['polling does not overlap requests', doesNotStartOverlappingRequests],
  ['summary polling respects its interval', refreshesSummaryAtConfiguredInterval],
  ['last successful data survives reload and storage errors', dashboardCacheSurvivesReloadAndInvalidStorage]
]) {
  await run()
  console.log(`PASS ${name}`)
}
