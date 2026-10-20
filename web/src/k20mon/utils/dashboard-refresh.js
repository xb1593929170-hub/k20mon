export const createDashboardRefresher = ({
  getCurrentMetrics,
  getMetricsSummary,
  onCurrent,
  onSummary,
  onError,
  summaryIntervalMs = 60_000,
  now = () => Date.now()
}) => {
  let currentInFlight = null
  let summaryInFlight = null
  let nextSummaryAt = 0

  return async ({ forceSummary = false } = {}) => {
    const requests = []

    if (!currentInFlight) {
      currentInFlight = Promise.resolve()
        .then(getCurrentMetrics)
        .then(onCurrent)
        .catch((error) => onError(error, 'current'))
        .finally(() => { currentInFlight = null })
      requests.push(currentInFlight)
    }

    if (!summaryInFlight && (forceSummary || now() >= nextSummaryAt)) {
      nextSummaryAt = now() + summaryIntervalMs
      summaryInFlight = Promise.resolve()
        .then(() => getMetricsSummary({ limit: 1000 }))
        .then(onSummary)
        .catch((error) => onError(error, 'summary'))
        .finally(() => { summaryInFlight = null })
      requests.push(summaryInFlight)
    }

    await Promise.all(requests)
  }
}
