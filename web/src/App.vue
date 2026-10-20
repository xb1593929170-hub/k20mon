<template>
  <div class="standalone-app">
    <section v-if="!sessionReady || !session" class="standalone-login">
      <div class="login-panel">
        <div class="brand-mark">K20</div>
        <p class="eyebrow">DEVICE MONITOR</p>
        <h1>K20Mon</h1>
        <p class="login-copy">登录后查看设备状态、历史采样和服务宫格。</p>

        <el-form @submit.prevent="submitLogin">
          <el-form-item label="用户名">
            <el-input v-model="loginForm.username" autocomplete="username" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input
              v-model="loginForm.password"
              type="password"
              show-password
              autocomplete="current-password"
              @keyup.enter="submitLogin"
            />
          </el-form-item>
          <el-alert
            v-if="loginError"
            class="login-error"
            type="error"
            :closable="false"
            :title="loginError"
          />
          <el-button
            class="login-submit"
            type="primary"
            native-type="submit"
            :loading="loginLoading"
          >
            登录
          </el-button>
        </el-form>
      </div>
    </section>

    <template v-else>
      <header class="standalone-header">
        <div class="header-brand">
          <div class="brand-mark brand-mark-small">K20</div>
          <div>
            <strong>K20Mon</strong>
            <span>设备监控</span>
          </div>
        </div>
        <div class="header-actions">
          <span class="user-label">{{ displayName }}</span>
          <el-button text @click="logout">退出</el-button>
        </div>
      </header>

      <div class="standalone-body">
        <nav class="standalone-nav" aria-label="K20Mon 导航">
          <button
            v-for="item in navItems"
            :key="item.key"
            type="button"
            :class="{ active: activeView === item.key }"
            @click="activeView = item.key"
          >
            <el-icon><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
          </button>
        </nav>

        <main class="standalone-content">
          <component :is="activeComponent" />
        </main>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, markRaw, onMounted, onUnmounted, reactive, ref } from 'vue'
import { DataLine, Grid, Setting } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import K20MonDashboard from '@/k20mon/views/dashboard.vue'
import K20MonOperations from '@/k20mon/views/operations.vue'
import K20MonServices from '@/k20mon/views/services.vue'
import { clearSession, getSession, getUserInfo, login, saveSession } from './api/auth'

defineOptions({ name: 'K20MonApp' })

const session = ref(null)
const sessionReady = ref(false)
const activeView = ref('dashboard')
const loginLoading = ref(false)
const loginError = ref('')
const loginForm = reactive({
  username: 'admin',
  password: ''
})

const navItems = [
  { key: 'dashboard', label: '仪表盘', icon: markRaw(DataLine), component: markRaw(K20MonDashboard) },
  { key: 'operations', label: '操作中心', icon: markRaw(Setting), component: markRaw(K20MonOperations) },
  { key: 'services', label: '服务宫格', icon: markRaw(Grid), component: markRaw(K20MonServices) }
]

const activeComponent = computed(() => (
  navItems.find((item) => item.key === activeView.value)?.component || K20MonDashboard
))

const displayName = computed(() => (
  session.value?.user?.nickName ||
  session.value?.user?.userName ||
  session.value?.user?.username ||
  session.value?.user?.Name ||
  session.value?.user?.name ||
  '管理员'
))

const hydrateSession = async () => {
  const stored = getSession()
  if (!stored?.token) {
    sessionReady.value = true
    return
  }

  try {
    const result = await getUserInfo()
    if (result?.code !== 0) throw new Error(result?.msg || '登录状态已失效')
    session.value = { ...stored, user: result.data?.userInfo || stored.user }
    saveSession(session.value)
  } catch {
    clearSession()
  } finally {
    sessionReady.value = true
  }
}

const submitLogin = async () => {
  loginError.value = ''
  if (!loginForm.username || !loginForm.password) {
    loginError.value = '请输入用户名和密码'
    return
  }

  loginLoading.value = true
  try {
    const result = await login(loginForm)
    if (result?.code !== 0 || !result.data?.token) {
      throw new Error(result?.msg || '登录失败')
    }
    session.value = {
      token: result.data.token,
      user: result.data.user || {}
    }
    saveSession(session.value)
    ElMessage.success('登录成功')
  } catch (error) {
    loginError.value = error.message || '登录失败'
  } finally {
    loginLoading.value = false
  }
}

const logout = () => {
  clearSession()
  session.value = null
  activeView.value = 'dashboard'
}

const handleSessionExpired = () => {
  logout()
  loginError.value = '登录状态已失效，请重新登录'
}

onMounted(() => {
  hydrateSession()
  window.addEventListener('k20mon:session-expired', handleSessionExpired)
  document.title = 'K20Mon'
})

onUnmounted(() => {
  window.removeEventListener('k20mon:session-expired', handleSessionExpired)
})
</script>

<style scoped>
.standalone-app {
  min-height: 100vh;
  color: #1f2937;
  background: #f3f5f7;
}

.standalone-login {
  display: grid;
  min-height: 100vh;
  place-items: center;
  padding: 24px;
  background: #eef2f5;
}

.login-panel {
  width: min(100%, 420px);
  padding: 36px;
  background: #fff;
  border: 1px solid #e1e7ec;
  border-radius: 8px;
  box-shadow: 0 18px 45px rgba(26, 39, 52, 0.1);
}

.brand-mark {
  display: grid;
  width: 52px;
  height: 52px;
  place-items: center;
  color: #fff;
  font-size: 16px;
  font-weight: 800;
  letter-spacing: 0;
  background: #1f6feb;
  border-radius: 8px;
}

.brand-mark-small {
  width: 34px;
  height: 34px;
  font-size: 11px;
}

.eyebrow {
  margin: 22px 0 6px;
  color: #607080;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.12em;
}

h1 {
  margin: 0;
  font-size: 30px;
  letter-spacing: 0;
}

.login-copy {
  margin: 10px 0 28px;
  color: #6b7785;
}

.login-error {
  margin: 4px 0 16px;
}

.login-submit {
  width: 100%;
}

.standalone-header {
  display: flex;
  min-height: 64px;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  background: #fff;
  border-bottom: 1px solid #e1e7ec;
}

.header-brand,
.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-brand strong,
.header-brand span {
  display: block;
}

.header-brand span,
.user-label {
  color: #718096;
  font-size: 12px;
}

.standalone-body {
  display: flex;
  min-height: calc(100vh - 64px);
}

.standalone-nav {
  display: flex;
  width: 180px;
  flex: 0 0 180px;
  flex-direction: column;
  gap: 6px;
  padding: 20px 12px;
  background: #17212b;
}

.standalone-nav button {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 11px 12px;
  color: #bcc8d4;
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 6px;
}

.standalone-nav button:hover,
.standalone-nav button.active {
  color: #fff;
  background: #253849;
}

.standalone-content {
  width: 0;
  flex: 1;
  min-width: 0;
  padding: 24px;
  overflow: auto;
}

@media (max-width: 720px) {
  .standalone-header {
    padding: 0 14px;
  }

  .standalone-body {
    display: block;
  }

  .standalone-nav {
    display: grid;
    width: auto;
    grid-template-columns: repeat(3, 1fr);
    padding: 8px;
  }

  .standalone-nav button {
    justify-content: center;
    padding: 9px 4px;
    font-size: 12px;
  }

  .standalone-content {
    width: auto;
    padding: 14px;
  }

  .user-label {
    display: none;
  }

  .login-panel {
    padding: 28px 22px;
  }
}
</style>
