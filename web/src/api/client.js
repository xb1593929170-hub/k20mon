// 统一的 axios 实例：
//   - 请求带 x-token / x-user-id
//   - 响应直接返回后端 {code,data,msg} 包体
//   - 401 时清掉登录态并广播事件，由 App.vue 踢回登录页
import axios from 'axios'
import { SESSION_KEY, clearSession, getSession } from './auth'

const client = axios.create({
  timeout: 1000 * 60 * 10
})

client.interceptors.request.use((config) => {
  const session = getSession()
  const headers = {
    'Content-Type': 'application/json',
    ...config.headers
  }

  if (session?.token) headers['x-token'] = session.token
  if (session?.user?.ID) headers['x-user-id'] = session.user.ID

  config.baseURL = config.baseURL || import.meta.env.VITE_BASE_API || '/api'
  config.headers = headers
  return config
})

client.interceptors.response.use(
  (response) => {
    // 后端如果下发续签 token，就地刷新登录态
    const refreshed = response.headers['new-token']
    if (refreshed) {
      const session = getSession()
      if (session) {
        session.token = refreshed
        try {
          localStorage.setItem(SESSION_KEY, JSON.stringify(session))
        } catch {
          // 忽略存储失败
        }
      }
    }
    return response.data
  },
  (error) => {
    if (error.response?.status === 401) {
      clearSession()
      window.dispatchEvent(new CustomEvent('k20mon:session-expired'))
    }
    return Promise.reject(error)
  }
)

export default client
