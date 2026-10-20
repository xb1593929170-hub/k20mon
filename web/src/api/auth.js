// 登录态：localStorage 持久化 + 登录/取用户信息接口。
// token 走 x-token 请求头，和后端 JWT 中间件一致。
import client from './client'

export const SESSION_KEY = 'k20mon.session.v1'

export const getSession = () => {
  try {
    const raw = localStorage.getItem(SESSION_KEY)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

export const saveSession = (session) => {
  try {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session))
  } catch {
    // 隐私模式下 localStorage 可能不可写，退化成仅内存登录态
  }
}

export const clearSession = () => {
  try {
    localStorage.removeItem(SESSION_KEY)
  } catch {
    // 同上
  }
}

export const login = (data) => client({
  url: '/base/login',
  method: 'post',
  data
})

export const getUserInfo = () => client({
  url: '/user/getUserInfo',
  method: 'get'
})
