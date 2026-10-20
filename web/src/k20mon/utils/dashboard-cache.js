export const readDashboardCache = (storage, key) => {
  try {
    const value = storage?.getItem(key)
    return value ? JSON.parse(value) : null
  } catch {
    return null
  }
}

export const writeDashboardCache = (storage, key, value) => {
  try {
    storage?.setItem(key, JSON.stringify(value))
  } catch {
    // Keep the dashboard usable when browser storage is unavailable or full.
  }
}
