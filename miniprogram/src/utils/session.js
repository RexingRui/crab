import Taro from '@tarojs/taro'
import { DEFAULT_BASE_URL, DEFAULT_TRACK_URL, STORAGE_KEYS } from './config'

/* 登录票据与运行模式，用一个模块托管，别再散在 getApp() 里 */
const state = {
  token: '',
  openid: '',
  demoMode: false,
  ready: null
}

const listeners = new Set()

function emit() {
  listeners.forEach((fn) => fn({ demoMode: state.demoMode }))
}

export function onSessionChange(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function readStorage(key, fallback = '') {
  try {
    return Taro.getStorageSync(key) || fallback
  } catch (e) {
    return fallback
  }
}

export function writeStorage(key, value) {
  try {
    Taro.setStorageSync(key, value)
  } catch (e) {
    /* 存不进去就下次再说，不打断流程 */
  }
}

export function getToken() {
  if (!state.token) state.token = readStorage(STORAGE_KEYS.token)
  return state.token
}

export function setToken(token) {
  state.token = token || ''
  writeStorage(STORAGE_KEYS.token, state.token)
}

export function clearToken() {
  setToken('')
}

export function setOpenid(openid) {
  state.openid = openid || ''
}

/** request_id 要用到 openid 后六位，拿不到就退回一个随机串 */
export function openidTail() {
  const id = state.openid || readStorage('openid')
  if (id) return String(id).slice(-6)
  return Math.random().toString(36).slice(-6)
}

export function getBaseUrl() {
  return readStorage(STORAGE_KEYS.baseUrl, DEFAULT_BASE_URL)
}

export function setBaseUrl(url) {
  writeStorage(STORAGE_KEYS.baseUrl, url)
}

export function getTrackUrl() {
  return readStorage(STORAGE_KEYS.trackUrl, DEFAULT_TRACK_URL)
}

export function setTrackUrl(url) {
  writeStorage(STORAGE_KEYS.trackUrl, url)
}

export function isDemoMode() {
  return state.demoMode
}

export function enterDemoMode() {
  if (state.demoMode) return
  state.demoMode = true
  emit()
}

export function exitDemoMode() {
  if (!state.demoMode) return
  state.demoMode = false
  emit()
}

/* 页面在发请求前 await 一次，避免首页比登录先出手 */
export function setReady(promise) {
  state.ready = promise
}

export function whenReady() {
  return state.ready || Promise.resolve()
}
