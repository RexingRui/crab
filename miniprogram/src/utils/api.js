import { get, post, put, del } from './request'

/* 接口出入参集中在这里，后端字段有出入时只改这一个文件。
 * 约定：所有金额字段都是「分」为单位的整数。 */
export const api = {
  dashboard: () => get('/api/stats/dashboard'),
  shipPlan: (date = 'today') => get('/api/stats/ship-plan', { date }),

  orders: (params) => get('/api/orders', params),
  order: (id) => get(`/api/orders/${id}`),
  createOrder: (body) => post('/api/orders', body, { silent: true }),
  updateOrder: (id, body) => put(`/api/orders/${id}`, body),
  removeOrder: (id) => del(`/api/orders/${id}`),

  ship: (id, body) => post(`/api/orders/${id}/ship`, body),
  confirmReceive: (id) => post(`/api/orders/${id}/receive`, {}),
  revertShip: (id) => post(`/api/orders/${id}/revert-ship`, {}),
  addPayment: (id, body) => post(`/api/orders/${id}/payments`, body),

  specs: () => get('/api/specs'),
  createSpec: (body) => post('/api/specs', body),
  updateSpec: (id, body) => put(`/api/specs/${id}`, body),
  removeSpec: (id) => del(`/api/specs/${id}`)
}

export default api
