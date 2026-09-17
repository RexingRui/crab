import { get, post, put, del } from './request'
import { todayStr } from './format'

/* 接口出入参集中在这里。字段名一律跟后端保持一致（见根目录 README 的 API 章节），
 * 不在前端另造一套词汇，省得两边对不上。
 * 约定：所有金额字段都是「分」为单位的整数。 */
export const api = {
  // 统计
  dashboard: (params) => get('/api/stats/dashboard', params),
  // date 必须是 YYYY-MM-DD：后端只认这个格式，且不传时默认给的是「明天」。
  // 顺手把 'today' 这种字面量兜住，免得又写出 date=today 这种 400。
  shipPlan: (date) => get('/api/stats/ship-plan', {
    date: !date || date === 'today' ? todayStr() : date
  }),

  // 订单
  orders: (params) => get('/api/orders', params),
  order: (id) => get(`/api/orders/${id}`),
  orderByNo: (orderNo) => get(`/api/orders/by-no/${orderNo}`),
  createOrder: (body) => post('/api/orders', body, { silent: true }),
  updateOrder: (id, body) => put(`/api/orders/${id}`, body),
  removeOrder: (id) => del(`/api/orders/${id}`),

  // 状态流转
  ship: (id, body) => post(`/api/orders/${id}/ship`, body),
  confirmReceive: (id) => post(`/api/orders/${id}/receive`, { receive_time: null }),
  cancelOrder: (id, reason) => post(`/api/orders/${id}/cancel`, { reason }),
  // to 只能是 pending / shipped，reason 必填
  revertShip: (id, to, reason) => post(`/api/orders/${id}/revert-ship`, { to, reason }),

  // 收款
  addPayment: (id, body) => post(`/api/orders/${id}/payments`, body),
  removePayment: (paymentId) => del(`/api/payments/${paymentId}`),

  // 规格价目表：录单页只要启用中的，设置页要连停用的一起看
  specs: () => get('/api/specs'),
  allSpecs: () => get('/api/specs', { all: 1 }),
  createSpec: (body) => post('/api/specs', body),
  updateSpec: (id, body) => put(`/api/specs/${id}`, body),
  // 后端是停用，不物理删
  disableSpec: (id) => del(`/api/specs/${id}`),

  // 地址簿，从历史订单聚合
  addresses: (keyword, limit = 20) => get('/api/addresses', { keyword, limit }),

  // 买家自助登记：签一条链接发给买家。remark 是先写好的备注名，买家改不了。
  // 返回 { token, path, expires_at }，域名在前端拼（和查单页共用一个）。
  createRegLink: (remark) => post('/api/reg-links', { remark })
}

export default api
