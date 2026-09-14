import { message } from 'antd'

const TOKEN_KEY = 'crab_admin_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(token) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

export function clearToken() {
  setToken('')
}

/** 401 时把人踢回登录页，其余错误统一弹一条 message */
async function call(url, { method = 'GET', body, silent } = {}) {
  let res
  try {
    res = await fetch(url, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(getToken() ? { Authorization: `Bearer ${getToken()}` } : {})
      },
      body: body === undefined ? undefined : JSON.stringify(body)
    })
  } catch (e) {
    if (!silent) message.error('网络不通，检查一下后端是不是没起')
    throw e
  }

  if (res.status === 401) {
    clearToken()
    if (!location.pathname.endsWith('/login')) location.href = '/admin/login'
    throw new Error('未登录')
  }

  let payload = null
  try { payload = await res.json() } catch (e) { payload = null }

  if (!res.ok || !payload || payload.code !== 0) {
    const msg = (payload && payload.msg) || `请求失败（${res.status}）`
    if (!silent) message.error(msg)
    const err = new Error(msg)
    err.code = payload && payload.code
    throw err
  }
  return payload.data
}

function qs(params = {}) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') search.append(k, v)
  })
  const s = search.toString()
  return s ? `?${s}` : ''
}

export const api = {
  login: (body) => call('/api/admin/login', { method: 'POST', body, silent: true }),

  orders: (params) => call(`/api/orders${qs(params)}`),
  order: (id) => call(`/api/orders/${id}`),
  updateOrder: (id, body) => call(`/api/orders/${id}`, { method: 'PUT', body }),
  removeOrder: (id) => call(`/api/orders/${id}`, { method: 'DELETE' }),

  ship: (id, body) => call(`/api/orders/${id}/ship`, { method: 'POST', body }),
  batchShip: (body) => call('/api/orders/batch-ship', { method: 'POST', body }),
  confirmReceive: (id) => call(`/api/orders/${id}/receive`, { method: 'POST', body: {} }),
  addPayment: (id, body) => call(`/api/orders/${id}/payments`, { method: 'POST', body }),

  specs: () => call('/api/specs'),
  createSpec: (body) => call('/api/specs', { method: 'POST', body }),
  updateSpec: (id, body) => call(`/api/specs/${id}`, { method: 'PUT', body }),
  removeSpec: (id) => call(`/api/specs/${id}`, { method: 'DELETE' }),

  stats: (params) => call(`/api/stats/summary${qs(params)}`),

  // 导出走浏览器下载，带上当前筛选条件
  exportUrl: (params) => `/api/orders/export${qs(params)}`
}

/** 导出要带 Authorization 头，只能先抓成 blob 再存 */
export async function downloadExport(params) {
  const res = await fetch(api.exportUrl(params), {
    headers: getToken() ? { Authorization: `Bearer ${getToken()}` } : {}
  })
  if (!res.ok) throw new Error('导出失败')
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `蟹记订单-${new Date().toISOString().slice(0, 10)}.csv`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
