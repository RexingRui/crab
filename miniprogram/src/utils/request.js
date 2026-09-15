import Taro from '@tarojs/taro'
import {
  getToken, setToken, clearToken, getBaseUrl,
  enterDemoMode, isDemoMode, setOpenid, writeStorage
} from './session'
import { handleInDemo } from './demo'

/* 统一封装：自动带 token、40100 换票重试一次、非 0 code 统一提示、并发只弹一次 loading。
 * 页面里不要再直接调 Taro.request，也不要重复写错误处理。 */

let pending = 0
let loginTask = null

function showLoading() {
  pending += 1
  if (pending === 1) Taro.showLoading({ title: '加载中', mask: true })
}

function hideLoading() {
  pending = Math.max(0, pending - 1)
  if (pending === 0) Taro.hideLoading()
}

export function toast(msg, icon = 'none') {
  Taro.showToast({ title: msg || '出错了', icon, duration: 2200 })
}

/* 不做任何拦截的裸请求，只有登录接口和内部重试用得上 */
function raw({ url, method = 'GET', data, header, skipAuth }) {
  const finalHeader = { 'content-type': 'application/json', ...(header || {}) }
  if (!skipAuth) {
    const token = getToken()
    if (token) finalHeader.Authorization = `Bearer ${token}`
  }
  return Taro.request({
    url: getBaseUrl() + url,
    method,
    data,
    header: finalHeader,
    timeout: 15000
  }).then((res) => {
    const body = res.data || {}
    if (res.statusCode >= 500) {
      throw { code: res.statusCode, msg: '服务器开小差了，等会儿再试' }
    }
    if (body.code === 0) return body.data
    throw { code: body.code, msg: body.msg || '请求失败', data: body.data }
  }, (err) => {
    const errMsg = (err && err.errMsg) || ''
    // 用户看到的是笼统的「网络不通」，但排查时得知道微信到底报的什么：
    // 域名没进白名单是 request:fail url not in domain list，DNS / 证书问题又是另一套。
    // 手机上打开调试，这行会出现在 vConsole 的 Log 面板里。
    console.error('[request] 请求失败', getBaseUrl() + url, errMsg)
    const timeout = errMsg.includes('timeout')
    throw { code: -1, msg: timeout ? '网络超时' : '网络不通，检查一下信号' }
  })
}

/* Taro.login → POST /api/login，全局只跑一份，并发请求共用同一个 Promise */
export function login() {
  if (loginTask) return loginTask
  loginTask = Taro.login()
    .then(({ code }) => {
      if (!code) throw { code: -1, msg: '拿不到登录凭证' }
      return raw({ url: '/api/login', method: 'POST', data: { code }, skipAuth: true })
    })
    .then((payload) => {
      setToken(payload && payload.token)
      if (payload && payload.openid) {
        setOpenid(payload.openid)
        writeStorage('openid', payload.openid)
      }
      return payload
    })
    .finally(() => { loginTask = null })
  return loginTask
}

/**
 * @param {object} options { url, method, data, loading, silent, retried }
 * silent 为 true 时不弹错误提示，由调用方自己处理（提交类接口用得上）。
 */
export function request(options) {
  if (isDemoMode()) return handleInDemo(options)

  const needLoading = options.loading !== false
  if (needLoading) showLoading()

  return raw(options).then(
    (data) => {
      if (needLoading) hideLoading()
      return data
    },
    (err) => {
      if (needLoading) hideLoading()

      // 票据过期：换一张再试一次，只试一次
      if (err && err.code === 40100 && !options.retried) {
        clearToken()
        return login()
          .then(() => request({ ...options, retried: true }))
          .catch(() => {
            clearToken()
            if (!options.silent) toast('登录失效了，重新进一次')
            throw err
          })
      }

      // 不在管理员白名单里 → 转只读演示模式，页面照常渲染
      if (err && err.code === 40300) {
        enterDemoMode()
        return handleInDemo(options)
      }

      if (!options.silent) toast(err && err.msg)
      throw err
    }
  )
}

export const get = (url, data, options) => request({ url, method: 'GET', data, ...options })
export const post = (url, data, options) => request({ url, method: 'POST', data, ...options })
export const put = (url, data, options) => request({ url, method: 'PUT', data, ...options })
export const del = (url, data, options) => request({ url, method: 'DELETE', data, ...options })
