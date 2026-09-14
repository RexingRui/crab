import demoData from '../static/demo.json'
import { formatDate, statusText } from './format'

/* 审核演示模式：审核员的 openid 不在白名单里，直接提审只会看到空白。
 * 这里用本地假数据把三个页面渲染出来，功能可浏览，写操作一律拒绝。 */

const DAY = 86400000

function dayAt(offset, time = '09:00') {
  const d = new Date(Date.now() + (offset || 0) * DAY)
  const [h, m] = String(time).split(':')
  d.setHours(Number(h) || 9, Number(m) || 0, 0, 0)
  return d
}

function stamp(offset, time) {
  return offset === null || offset === undefined ? '' : formatDate(dayAt(offset, time), 'YYYY-MM-DD HH:mm')
}

function dateOnly(offset) {
  return offset === null || offset === undefined ? '' : formatDate(dayAt(offset), 'YYYY-MM-DD')
}

/* JSON 里存的是相对天数，用的时候才落成具体日期，
 * 这样不管哪天打开，「今天要发的」都还是今天。 */
function materialize(order) {
  return {
    ...order,
    order_no: formatDate(dayAt(order.created_day_offset), 'YYYYMMDD') + '-' + order.order_no,
    created_at: stamp(order.created_day_offset, (order.logs[0] && order.logs[0].time) || '09:00'),
    plan_ship_date: dateOnly(order.plan_ship_day_offset),
    shipped_at: stamp(order.shipped_day_offset, '16:20'),
    received_at: stamp(order.received_day_offset, '18:05'),
    ship_status_text: statusText('ship', order.ship_status),
    pay_status_text: statusText('pay', order.pay_status),
    payments: (order.payments || []).map((p) => ({ ...p, paid_at: stamp(p.day_offset, p.time) })),
    logs: (order.logs || []).map((l) => ({ ...l, created_at: stamp(l.day_offset, l.time) }))
  }
}

let cache = null

function dataset() {
  if (!cache) {
    cache = {
      specs: demoData.specs,
      orders: demoData.orders.map(materialize)
    }
  }
  return cache
}

export const DEMO_NOTICE = demoData.notice

function matchOrder(order, params = {}) {
  const kw = String(params.keyword || '').trim()
  if (kw) {
    const hay = [order.order_no, order.receiver_name, order.receiver_phone, order.tracking_no].join(' ')
    if (!hay.includes(kw)) return false
  }
  if (params.ship_status && order.ship_status !== params.ship_status) return false
  if (params.pay_status && order.pay_status !== params.pay_status) return false
  return true
}

function listOrders(params = {}) {
  const pageSize = Number(params.page_size) || 20
  const page = Number(params.page) || 1
  const all = dataset().orders.filter((o) => matchOrder(o, params))
  return {
    list: all.slice((page - 1) * pageSize, page * pageSize),
    total: all.length,
    page,
    page_size: pageSize
  }
}

function dashboard() {
  const { orders } = dataset()
  const today = formatDate(new Date(), 'YYYY-MM-DD')
  const unpaid = orders.filter((o) => o.ship_status !== 'cancelled' && o.unpaid_amount > 0)
  return {
    today: today,
    to_ship_count: orders.filter((o) => o.ship_status === 'pending').length,
    unpaid_amount: unpaid.reduce((sum, o) => sum + o.unpaid_amount, 0),
    unpaid_count: unpaid.length,
    shipped_count: orders.filter((o) => o.ship_status === 'shipped').length
  }
}

function shipPlan() {
  const { orders } = dataset()
  const today = formatDate(new Date(), 'YYYY-MM-DD')
  return {
    list: orders
      .filter((o) => o.ship_status === 'pending' && o.plan_ship_date && o.plan_ship_date <= today)
      .sort((a, b) => a.plan_ship_date.localeCompare(b.plan_ship_date))
  }
}

const READ_ROUTES = [
  [/^\/api\/stats\/dashboard/, dashboard],
  [/^\/api\/stats\/ship-plan/, shipPlan],
  [/^\/api\/specs$/, () => ({ list: dataset().specs })],
  [/^\/api\/orders\/(\d+)$/, (m) => dataset().orders.find((o) => String(o.id) === m[1]) || null],
  [/^\/api\/orders$/, (m, options) => listOrders(options.data)]
]

/**
 * 演示模式下接管所有请求：读接口给假数据，写接口一律拒绝。
 */
export function handleInDemo(options) {
  const url = String(options.url || '').split('?')[0]
  const method = (options.method || 'GET').toUpperCase()

  if (method !== 'GET') {
    return Promise.reject({ code: 40300, msg: '演示模式下不能修改', demo: true })
  }

  for (const [pattern, handler] of READ_ROUTES) {
    const m = pattern.exec(url)
    if (m) {
      // 假装有一点点网络延迟，页面的 loading 态才看得出来
      return new Promise((resolve) => setTimeout(() => resolve(handler(m, options)), 120))
    }
  }
  return Promise.resolve(null)
}
