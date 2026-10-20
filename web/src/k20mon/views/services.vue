<template>
  <div class="services-page">
    <header class="services-header">
      <div>
        <div class="eyebrow">K20MON</div>
        <h1>服务宫格</h1>
        <p v-if="checkedAt">最近检查：{{ formatTime(checkedAt) }}</p>
        <p v-else>管理已发布服务并检查访问状态</p>
      </div>
      <div class="header-actions">
        <el-tooltip :content="showUrls ? '隐藏访问地址' : '显示访问地址'">
          <el-button
            text
            circle
            :icon="showUrls ? Lock : Unlock"
            :aria-label="showUrls ? '隐藏访问地址' : '显示访问地址'"
            @click="toggleUrlVisibility"
          />
        </el-tooltip>
        <el-button :icon="Plus" @click="openCreate">添加服务</el-button>
        <el-button type="primary" @click="refresh">
          <el-icon><Refresh /></el-icon>
          刷新状态
        </el-button>
      </div>
    </header>

    <el-alert
      v-if="error"
      class="service-alert"
      type="error"
      :closable="false"
      show-icon
      :title="error"
    />

    <div class="summary-strip">
      <div class="summary-item">
        <span class="summary-dot is-online"></span>
        <strong>{{ result?.online ?? '--' }}</strong>
        <span>可访问</span>
      </div>
      <div class="summary-item">
        <span class="summary-dot is-offline"></span>
        <strong>{{ result?.offline ?? '--' }}</strong>
        <span>不可访问</span>
      </div>
      <div class="summary-item">
        <strong>{{ result?.total ?? '--' }}</strong>
        <span>已登记服务</span>
      </div>
      <div class="service-controls">
        <el-input
          v-model="search"
          class="search-input"
          clearable
          placeholder="搜索服务或域名"
          :prefix-icon="Search"
        />
        <el-select v-model="sortBy" class="sort-select" aria-label="服务排序" @change="persistSortBy">
          <el-option label="默认顺序" value="created" />
          <el-option label="按名称" value="name" />
          <el-option label="按状态" value="status" />
          <el-option label="按响应时间" value="response" />
          <el-option label="按分类" value="category" />
        </el-select>
        <el-tooltip :content="sortDirection === 'asc' ? '升序排列' : '降序排列'">
          <el-button
            text
            circle
            :icon="sortDirection === 'asc' ? SortUp : SortDown"
            aria-label="切换排序方向"
            @click="toggleSortDirection"
          />
        </el-tooltip>
        <el-button-group class="view-switch">
          <el-tooltip content="列表视图">
            <el-button
              :type="viewMode === 'list' ? 'primary' : 'default'"
              :icon="List"
              aria-label="列表视图"
              @click="setViewMode('list')"
            />
          </el-tooltip>
          <el-tooltip content="宫格视图">
            <el-button
              :type="viewMode === 'grid' ? 'primary' : 'default'"
              :icon="Grid"
              aria-label="宫格视图"
              @click="setViewMode('grid')"
            />
          </el-tooltip>
        </el-button-group>
      </div>
    </div>

    <el-tabs v-model="activeTab" class="owner-tabs">
      <el-tab-pane :label="`我的服务 (${mineServices.length})`" name="mine" />
      <el-tab-pane :label="`别人的地址 (${otherServices.length})`" name="other" />
      <el-tab-pane :label="`全部 (${services.length})`" name="all" />
    </el-tabs>

    <div v-if="loading && !services.length" class="empty-state">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span>正在检查服务</span>
    </div>
    <el-empty v-else-if="!filteredServices.length" description="没有匹配的服务" />

    <div v-else ref="gridRef" :class="['service-grid', `is-${viewMode}`]">
      <article
        v-for="service in sortedServices"
        :key="service.id"
        class="service-tile"
        :data-service-id="service.id"
      >
        <div class="tile-topline">
          <div class="tile-tags">
            <el-tag size="small" effect="plain">{{ service.category || '服务' }}</el-tag>
            <el-tag
              size="small"
              :type="statusType(service.state)"
              effect="light"
            >
              <span class="status-dot" :class="`status-${service.state || 'unknown'}`"></span>
              {{ statusLabel(service.state) }}
            </el-tag>
          </div>
          <el-tooltip v-if="sortBy === 'created'" content="拖动排序">
            <button
              class="drag-handle"
              :class="{ 'is-disabled': reordering }"
              type="button"
              aria-label="拖动排序"
            >
              <el-icon><Sort /></el-icon>
            </button>
          </el-tooltip>
        </div>

        <div class="tile-title-row">
          <div class="service-icon" :class="`icon-${service.owner_type}`">
            <el-icon><component :is="serviceIcon(service)" /></el-icon>
          </div>
          <div class="service-heading">
            <h2>{{ service.name }}</h2>
            <p>{{ service.description || service.category || '已发布服务' }}</p>
          </div>
        </div>

        <a
          v-if="showUrls"
          class="service-url"
          :href="service.url"
          target="_blank"
          rel="noopener noreferrer"
        >
          <span>{{ displayHost(service.url) }}</span>
          <el-icon><TopRight /></el-icon>
        </a>
        <button
          v-else
          class="service-url service-url-hidden"
          type="button"
          @click="openService(service.url)"
        >
          <span>访问地址已隐藏</span>
          <el-icon><TopRight /></el-icon>
        </button>

        <div class="service-meta">
          <span>HTTP {{ service.http_code ?? '--' }}</span>
          <span>{{ service.response_ms ?? '--' }} ms</span>
          <span v-if="service.reason" class="failure-reason" :title="service.reason">
            {{ service.reason }}
          </span>
        </div>

        <footer class="tile-actions">
          <el-button
            text
            :type="service.owner_type === 'mine' ? 'primary' : 'warning'"
            :loading="updatingId === service.id"
            @click="toggleOwner(service)"
          >
            {{ service.owner_type === 'mine' ? '标为别人的地址' : '标为我的服务' }}
          </el-button>
          <el-button text :icon="Edit" @click="openEdit(service)">编辑</el-button>
          <el-button text type="danger" :icon="Delete" @click="removeService(service)">删除</el-button>
          <el-button
            type="primary"
            plain
            :icon="TopRight"
            @click="openService(service.url)"
          >
            打开
          </el-button>
        </footer>
      </article>
    </div>

    <div class="page-footnote">
      <span>状态每 30 秒自动检查一次</span>
      <span>配置保存到 MySQL，点击链接将在新窗口打开</span>
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogMode === 'create' ? '添加服务' : '编辑服务'"
      width="560px"
      destroy-on-close
    >
      <el-form label-width="92px">
        <el-form-item label="服务 ID">
          <el-input v-model="form.id" :disabled="dialogMode === 'edit'" placeholder="例如 my-service" />
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="form.name" maxlength="120" show-word-limit />
        </el-form-item>
        <el-form-item label="访问地址">
          <el-input v-model="form.url" type="url" placeholder="https://example.com" />
        </el-form-item>
        <el-form-item label="分类">
          <el-input v-model="form.category" maxlength="60" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" maxlength="240" show-word-limit />
        </el-form-item>
        <el-form-item label="归属">
          <el-radio-group v-model="form.owner_type">
            <el-radio label="mine">我的服务</el-radio>
            <el-radio label="other">别人的地址</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveService">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Connection,
  Delete,
  DataBoard,
  Edit,
  Files,
  Grid,
  Lock,
  Loading,
  List,
  Monitor,
  Plus,
  Refresh,
  Sort,
  Search,
  SortDown,
  SortUp,
  Tools,
  TopRight,
  Unlock
} from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import Sortable from 'sortablejs'
import {
  createService,
  deleteService,
  getServicesStatus,
  updateServiceOrder,
  updateService,
  updateServiceOwner
} from '@/k20mon/api/metrics'

defineOptions({ name: 'K20MonServices' })

const result = ref(null)
const loading = ref(false)
const error = ref('')
const search = ref('')
const activeTab = ref('mine')
const updatingId = ref('')
const saving = ref(false)
const reordering = ref(false)
const gridRef = ref(null)
const dialogVisible = ref(false)
const dialogMode = ref('create')
const form = ref(emptyForm())
const showUrls = ref(typeof window === 'undefined'
  ? true
  : window.localStorage.getItem('k20mon-service-urls-visible') !== 'false')
const viewMode = ref(typeof window === 'undefined'
  ? 'list'
  : window.localStorage.getItem('k20mon-service-view') === 'grid' ? 'grid' : 'list')
const sortBy = ref(typeof window === 'undefined'
  ? 'created'
  : window.localStorage.getItem('k20mon-service-sort') || 'created')
const sortDirection = ref(typeof window === 'undefined'
  ? 'asc'
  : window.localStorage.getItem('k20mon-service-sort-direction') === 'desc' ? 'desc' : 'asc')
let timer = null

function emptyForm() {
  return {
    id: '',
    name: '',
    description: '',
    category: '服务',
    owner_type: 'mine',
    url: '',
    enabled: true
  }
}

const services = computed(() => result.value?.services || [])
const mineServices = computed(() => services.value.filter((item) => item.owner_type === 'mine'))
const otherServices = computed(() => services.value.filter((item) => item.owner_type === 'other'))
const checkedAt = computed(() => result.value?.checked_at ? new Date(result.value.checked_at * 1000) : null)
const filteredServices = computed(() => {
  const query = search.value.trim().toLowerCase()
  return services.value.filter((item) => {
    const ownerMatches = activeTab.value === 'all' || item.owner_type === activeTab.value
    const textMatches = !query || [item.name, item.url, item.category, item.description]
      .some((value) => String(value || '').toLowerCase().includes(query))
    return ownerMatches && textMatches
  })
})
const sortedServices = computed(() => {
  const items = filteredServices.value
  if (sortBy.value === 'created') return items

  const statusRank = { online: 0, error: 1, offline: 2, disabled: 3 }
  const direction = sortDirection.value === 'desc' ? -1 : 1
  return items
    .map((item, index) => ({ item, index }))
    .sort((left, right) => {
      let comparison = 0
      if (sortBy.value === 'name' || sortBy.value === 'category') {
        const leftValue = String(left.item[sortBy.value] || '')
        const rightValue = String(right.item[sortBy.value] || '')
        comparison = leftValue.localeCompare(rightValue, 'zh-CN', { sensitivity: 'base' })
      } else if (sortBy.value === 'status') {
        comparison = (statusRank[left.item.state] ?? 99) - (statusRank[right.item.state] ?? 99)
      } else if (sortBy.value === 'response') {
        const leftValue = Number(left.item.response_ms)
        const rightValue = Number(right.item.response_ms)
        const leftMissing = !Number.isFinite(leftValue)
        const rightMissing = !Number.isFinite(rightValue)
        if (leftMissing || rightMissing) {
          if (leftMissing && rightMissing) return left.index - right.index
          return leftMissing ? 1 : -1
        }
        comparison = leftValue - rightValue
      }
      return comparison * direction || left.index - right.index
    })
    .map(({ item }) => item)
})

const refresh = async () => {
  if (reordering.value) return
  loading.value = true
  error.value = ''
  try {
    result.value = await getServicesStatus()
  } catch (requestError) {
    error.value = requestError?.message || '读取服务状态失败'
  } finally {
    loading.value = false
  }
}

const saveVisibleOrder = async (visible) => {
  if (reordering.value) return
  const original = services.value.slice()
  const visibleIds = new Set(visible.map((item) => item.id))
  let visibleIndex = 0
  const reordered = original.map((item) => visibleIds.has(item.id) ? visible[visibleIndex++] : item)
  result.value = { ...result.value, services: reordered }
  reordering.value = true
  try {
    await updateServiceOrder(reordered.map((item) => item.id))
    ElMessage.success('服务顺序已保存')
  } catch (requestError) {
    result.value = { ...result.value, services: original }
    ElMessage.error(requestError?.message || '保存服务顺序失败')
  } finally {
    reordering.value = false
    nextTick(initializeSortable)
  }
}

let sortable = null
const initializeSortable = () => {
  if (sortable) {
    sortable.destroy()
    sortable = null
  }
  if (!gridRef.value || sortBy.value !== 'created' || reordering.value) return
  sortable = Sortable.create(gridRef.value, {
    animation: 140,
    draggable: '.service-tile',
    handle: '.drag-handle',
    ghostClass: 'is-drag-over',
    onEnd: (event) => {
      if (event.oldIndex === event.newIndex) return
      const visible = sortedServices.value.slice()
      const [moved] = visible.splice(event.oldIndex, 1)
      visible.splice(event.newIndex, 0, moved)
      saveVisibleOrder(visible)
    }
  })
}

watch(sortedServices, () => nextTick(initializeSortable), { flush: 'post' })
watch([viewMode, sortBy], () => nextTick(initializeSortable), { flush: 'post' })

const toggleUrlVisibility = () => {
  showUrls.value = !showUrls.value
  window.localStorage.setItem('k20mon-service-urls-visible', String(showUrls.value))
}

const setViewMode = (mode) => {
  viewMode.value = mode
  window.localStorage.setItem('k20mon-service-view', mode)
}

const toggleSortDirection = () => {
  sortDirection.value = sortDirection.value === 'asc' ? 'desc' : 'asc'
  window.localStorage.setItem('k20mon-service-sort-direction', sortDirection.value)
}

const persistSortBy = () => {
  window.localStorage.setItem('k20mon-service-sort', sortBy.value)
}

const toggleOwner = async (service) => {
  const ownerType = service.owner_type === 'mine' ? 'other' : 'mine'
  updatingId.value = service.id
  try {
    await updateServiceOwner({ id: service.id, ownerType })
    service.owner_type = ownerType
    ElMessage.success(ownerType === 'mine' ? '已归入我的服务' : '已归入别人的地址')
  } catch (requestError) {
    ElMessage.error(requestError?.message || '更新服务归属失败')
  } finally {
    updatingId.value = ''
  }
}

const openCreate = () => {
  dialogMode.value = 'create'
  form.value = emptyForm()
  dialogVisible.value = true
}

const openEdit = (service) => {
  dialogMode.value = 'edit'
  form.value = {
    id: service.id,
    name: service.name,
    description: service.description || '',
    category: service.category || '服务',
    owner_type: service.owner_type === 'other' ? 'other' : 'mine',
    url: service.url,
    enabled: service.enabled !== false
  }
  dialogVisible.value = true
}

const saveService = async () => {
  const payload = {
    ...form.value,
    ownerType: form.value.owner_type
  }
  delete payload.owner_type
  if (!payload.id || !payload.name || !payload.url) {
    ElMessage.warning('请填写服务 ID、名称和访问地址')
    return
  }
  saving.value = true
  try {
    if (dialogMode.value === 'create') {
      await createService(payload)
      ElMessage.success('服务已添加')
    } else {
      await updateService(payload)
      ElMessage.success('服务配置已保存')
    }
    dialogVisible.value = false
    await refresh()
  } catch (requestError) {
    ElMessage.error(requestError?.message || '保存服务配置失败')
  } finally {
    saving.value = false
  }
}

const removeService = async (service) => {
  try {
    await ElMessageBox.confirm(`确定删除服务“${service.name}”吗？`, '删除服务', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
    updatingId.value = service.id
    await deleteService(service.id)
    ElMessage.success('服务已删除')
    await refresh()
  } catch (requestError) {
    if (requestError !== 'cancel' && requestError !== 'close') {
      ElMessage.error(requestError?.message || '删除服务失败')
    }
  } finally {
    updatingId.value = ''
  }
}

const openService = (url) => {
  const opened = window.open(url, '_blank', 'noopener,noreferrer')
  if (!opened) ElMessage.warning('浏览器拦截了新窗口，请点击服务域名打开')
}

const displayHost = (url) => {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

const statusType = (state) => ({
  online: 'success',
  offline: 'danger',
  error: 'warning',
  disabled: 'info'
}[state] || 'info')

const statusLabel = (state) => ({
  online: '可访问',
  offline: '不可访问',
  error: '服务错误',
  disabled: '已停用'
}[state] || '未检查')

// 根据服务 id 和分类猜一个图标。只做关键词匹配，不绑定任何具体服务名。
const serviceIcon = (service) => {
  const key = `${service.id} ${service.category}`.toLowerCase()
  if (key.includes('monitor') || key.includes('监控') || key.includes('status')) return Monitor
  if (key.includes('api') || key.includes('gateway') || key.includes('网关')) return Connection
  if (key.includes('panel') || key.includes('admin') || key.includes('管理')) return Monitor
  if (key.includes('file') || key.includes('storage') || key.includes('存储')) return Files
  if (key.includes('auth') || key.includes('vault') || key.includes('password') || key.includes('安全')) return Lock
  if (key.includes('tool') || key.includes('工具')) return Tools
  if (key.includes('media') || key.includes('媒体') || key.includes('video')) return DataBoard
  return Grid
}

const formatTime = (value) => value ? value.toLocaleString('zh-CN') : '--'

onMounted(() => {
  refresh()
  timer = window.setInterval(refresh, 30000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  if (sortable) sortable.destroy()
})
</script>

<style scoped>
.services-page {
  min-height: 100%;
  padding: 20px;
  background: var(--el-bg-color-page);
}

.services-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.eyebrow {
  color: var(--el-color-primary);
  font-size: 11px;
  font-weight: 700;
}

.services-header h1 {
  margin: 4px 0;
  color: var(--el-text-color-primary);
  font-size: 24px;
}

.services-header p {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.service-alert {
  margin-bottom: 16px;
}

.summary-strip {
  display: flex;
  align-items: center;
  gap: 22px;
  min-height: 54px;
  padding: 8px 0;
  border-top: 1px solid var(--el-border-color-lighter);
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.service-controls {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
  margin-left: auto;
}

.summary-item {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  white-space: nowrap;
}

.summary-item strong {
  color: var(--el-text-color-primary);
  font-size: 16px;
}

.summary-dot,
.status-dot {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--el-color-info);
}

.summary-dot.is-online,
.status-online {
  background: var(--el-color-success);
}

.summary-dot.is-offline,
.status-offline,
.status-error {
  background: var(--el-color-danger);
}

.search-input {
  width: 250px;
  max-width: 100%;
}

.sort-select {
  width: 132px;
}

.view-switch :deep(.el-button) {
  padding-right: 9px;
  padding-left: 9px;
}

.owner-tabs {
  margin: 8px 0 16px;
}

.owner-tabs :deep(.el-tabs__header) {
  margin-bottom: 0;
}

.service-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.service-grid.is-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.service-tile {
  display: flex;
  min-width: 0;
  min-height: 224px;
  flex-direction: column;
  overflow: hidden;
  padding: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-top: 3px solid var(--el-color-success-light-5);
  background: var(--el-bg-color);
}

.service-grid.is-list .service-tile {
  display: grid;
  grid-template-areas:
    "topline url meta actions"
    "title url meta actions";
  grid-template-columns: minmax(220px, 1.25fr) minmax(190px, 1fr) minmax(130px, 0.8fr) minmax(300px, 1.4fr);
  grid-template-rows: auto auto;
  min-height: 0;
  align-items: center;
  padding: 12px 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  border-left: 3px solid var(--el-color-success-light-5);
}

.service-grid.is-list .tile-topline {
  grid-area: topline;
  justify-content: flex-start;
}

.service-grid.is-list .tile-title-row {
  grid-area: title;
  margin-top: 8px;
}

.service-grid.is-list .service-url {
  grid-area: url;
  margin-top: 0;
}

.service-grid.is-list .service-meta {
  grid-area: meta;
  flex-direction: column;
  gap: 4px;
  margin-top: 0;
}

.service-grid.is-list .tile-actions {
  grid-area: actions;
  margin-top: 0;
  padding-top: 0;
  border-top: 0;
}

.tile-topline,
.tile-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.tile-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}

.drag-handle {
  display: inline-flex;
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 0;
  color: var(--el-text-color-secondary);
  background: transparent;
  cursor: grab;
  touch-action: none;
}

.drag-handle:active {
  cursor: grabbing;
}

.drag-handle.is-disabled {
  cursor: wait;
  opacity: 0.45;
}

.service-tile.is-drag-over {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 1px;
}

.tile-actions {
  display: grid;
  grid-template-columns: minmax(0, 1fr) repeat(3, minmax(0, auto));
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.tile-topline :deep(.el-tag) {
  max-width: 48%;
}

.status-dot {
  display: inline-block;
  margin-right: 5px;
  vertical-align: 1px;
}

.tile-title-row {
  justify-content: flex-start;
  margin-top: 18px;
}

.service-icon {
  display: grid;
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  place-items: center;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  font-size: 19px;
}

.service-icon.icon-other {
  color: var(--el-color-warning-dark-2);
  background: var(--el-color-warning-light-9);
}

.service-heading {
  min-width: 0;
}

.service-heading h2 {
  margin: 0;
  color: var(--el-text-color-primary);
  font-size: 17px;
  overflow-wrap: anywhere;
}

.service-heading p {
  margin: 4px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.service-url {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  max-width: 100%;
  margin-top: 18px;
  color: var(--el-color-primary);
  font-size: 13px;
  overflow-wrap: anywhere;
  text-decoration: none;
}

.service-url-hidden {
  width: 100%;
  padding: 0;
  border: 0;
  background: transparent;
  cursor: pointer;
  text-align: left;
}

.service-url span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.service-url:hover {
  text-decoration: underline;
}

.service-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  min-height: 18px;
  margin-top: 8px;
  color: var(--el-text-color-secondary);
  font-size: 11px;
}

.failure-reason {
  max-width: 100%;
  overflow: hidden;
  color: var(--el-color-danger);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tile-actions {
  margin-top: auto;
  padding-top: 14px;
  border-top: 1px solid var(--el-border-color-extra-light);
}

.tile-actions :deep(.el-button) {
  min-width: 0;
  margin-left: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tile-actions :deep(.el-button:first-child) {
  width: 100%;
  text-align: left;
}

.empty-state {
  display: flex;
  min-height: 200px;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--el-text-color-secondary);
}

.page-footnote {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 18px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

@media (max-width: 1100px) {
  .service-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 680px) {
  .services-page {
    padding: 12px;
  }

  .services-header {
    align-items: stretch;
    flex-direction: column;
  }

  .summary-strip {
    flex-wrap: wrap;
    gap: 12px 18px;
  }

  .service-controls {
    width: 100%;
    margin-left: 0;
  }

  .search-input {
    flex: 1 1 220px;
    width: auto;
  }

  .sort-select {
    flex: 0 1 132px;
  }

  .service-grid.is-list .service-tile {
    display: flex;
    padding: 16px;
    border-top: 3px solid var(--el-color-success-light-5);
    border-left: 1px solid var(--el-border-color-lighter);
  }

  .service-grid.is-list .tile-topline,
  .service-grid.is-list .tile-title-row,
  .service-grid.is-list .service-url,
  .service-grid.is-list .service-meta,
  .service-grid.is-list .tile-actions {
    grid-area: auto;
  }

  .service-grid.is-list .tile-title-row {
    margin-top: 18px;
  }

  .service-grid.is-list .service-url {
    margin-top: 18px;
  }

  .service-grid.is-list .service-meta {
    flex-direction: row;
    margin-top: 8px;
  }

  .service-grid.is-list .tile-actions {
    margin-top: auto;
    padding-top: 14px;
    border-top: 1px solid var(--el-border-color-extra-light);
  }

  .service-grid {
    grid-template-columns: 1fr;
  }

  .page-footnote {
    flex-direction: column;
  }
}
</style>
