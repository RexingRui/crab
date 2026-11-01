import demoData from '../static/demo.json'
import { formatDate, statusText, todayStr } from './format'

/* 审核演示模式：审核员的 openid 不在白名单里，登录会拿到 40300，直接提审只会看到空白。
 * 这里用本地假数据把页面渲染出来，功能可浏览，写操作一律拒绝。
 * 假数据的形状与后端 DTO 保持一致，页面代码不用为演示模式分叉。 */

const DAY = 86400000

function dayAt(offset, time = '09:00') {
  const d = new Date(Date.now() + (offset || 0) * DAY)
  const [h, m] = String(time).split(':')
  d.setHours(Number(h) || 9, Number(m) || 0, 0, 0)
  return d
}

function stamp(offset, time) {
  return offset === null || offset === undefined ? null : formatDate(dayAt(offset, time), 'YYYY-MM-DD HH:mm:ss')
}

function dateOnly(offset) {
  return offset === null || offset === undefined ? null : formatDate(dayAt(offset), 'YYYY-MM-DD')
}

/* JSON 里存的是相对天数，用的时候才落成具体日期，
 * 这样不管哪天提审，「今天要发的」都还是今天。 */
function materialize(order) {
  const created = order.logs[0] ? order.logs[0].time : '09:00'
  return {
    ...order,
    order_no: formatDate(dayAt(order.created_day_offset), 'YYYYMMDD') + '-' + order.order_no,
    created_at: stamp(order.created_day_offset, created),
    updated_at: stamp(order.created_day_offset, created),
    expect_ship_date: dateOnly(order.expect_ship_day_offset),
    ship_time: stamp(order.ship_day_offset, '16:20'),
    receive_time: stamp(order.receive_day_offset, '18:05'),
    ship_status_text: statusText('ship', order.ship_status),
    pay_status_text: statusText('pay', order.pay_status),
    // 演示数据不带运费明细：一律当作还没填，卖家补贴按第一档
    freight_list: null,
    freight_cost: null,
    freight_pending: order.ship_status !== 'cancelled',
    freight_seller_cap: 2000,
    crab_count: (order.items || []).reduce((n, i) => n + i.quantity, 0),
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

function splitCSV(v) {
  return String(v || '').split(',').map((s) => s.trim()).filter(Boolean)
}

function matchOrder(order, params = {}) {
  const kw = String(params.keyword || '').trim()
  if (kw) {
    const hay = [order.order_no, order.receiver_name, order.phone, order.wechat_nick,
      order.wechat_remark, order.tracking_no].join(' ')
    if (!hay.includes(kw)) return false
  }
  const ship = splitCSV(params.ship_status)
  if (ship.length && !ship.includes(order.ship_status)) return false
  const pay = splitCSV(params.pay_status)
  if (pay.length && !pay.includes(order.pay_status)) return false
  if (params.expect_ship_date && order.expect_ship_date !== params.expect_ship_date) return false
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

/* 按规格聚合，口径同后端 SpecStatsBetween：性别 + 克重 + 品相 */
function bySpec(orders) {
  const map = {}
  orders.forEach((o) => (o.items || []).forEach((i) => {
    const key = `${i.gender}-${i.spec_gram}-${i.grade || 'normal'}`
    const row = map[key] || (map[key] = {
      gender: i.gender, spec_gram: i.spec_gram, grade: i.grade || 'normal',
      spec_label: i.spec_label, quantity: 0, amount: 0
    })
    row.quantity += i.quantity
    row.amount += i.amount || 0
  }))
  return Object.values(map).sort((a, b) => b.amount - a.amount)
}

/* 与后端 /api/stats/dashboard 同构 */
function dashboard(params = {}) {
  const { orders } = dataset()
  const today = todayStr()
  const start = params.start || today
  const end = params.end || today
  const inRange = (v) => {
    const day = String(v || '').slice(0, 10)
    return day >= start && day <= end
  }
  const live = orders.filter((o) => o.ship_status !== 'cancelled')
  const ranged = live.filter((o) => inRange(o.created_at))
  const received = orders.reduce((sum, o) => sum + (o.payments || [])
    .filter((p) => inRange(p.paid_at))
    .reduce((s, p) => s + p.amount, 0), 0)
  const unpaid = live.filter((o) => o.unpaid_amount > 0)
  const createdToday = live.filter((o) => String(o.created_at || '').slice(0, 10) === today)

  return {
    today: {
      new_orders: createdToday.length,
      shipped_orders: live.filter((o) => String(o.ship_time || '').slice(0, 10) === today).length,
      revenue: 0
    },
    pending: {
      to_ship_count: live.filter((o) => o.ship_status === 'pending').length,
      to_ship_today_count: live.filter((o) => o.ship_status === 'pending' && o.expect_ship_date === today).length,
      shipped_not_received_count: live.filter((o) => o.ship_status === 'shipped').length,
      unpaid_order_count: unpaid.length,
      unpaid_amount: unpaid.reduce((sum, o) => sum + o.unpaid_amount, 0)
    },
    range: {
      start,
      end,
      order_count: ranged.length,
      payable_total: ranged.reduce((sum, o) => sum + o.payable_amount, 0),
      paid_total: ranged.reduce((sum, o) => sum + o.paid_amount, 0),
      crab_count: ranged.reduce((sum, o) => sum + o.items.reduce((n, i) => n + i.quantity, 0), 0),
      by_spec: bySpec(ranged),
      received_total: received,
      cancelled_count: orders.filter((o) => o.ship_status === 'cancelled' && inRange(o.created_at)).length
    },
    // 演示数据不带运费明细，运费一律为空
    freight: {
      range_count: 0, list_total: 0, cost_total: 0, buyer_total: 0, seller_total: 0, saved_total: 0,
      unsettled_count: 0, unsettled_cost: 0, unsettled_list: 0, shipped_pending_count: 0
    }
  }
}

/* 与后端 /api/stats/ship-plan 同构：某天约定发货、且仍待发的单 */
function shipPlan(params = {}) {
  const date = params.date || todayStr()
  const list = dataset().orders.filter(
    (o) => o.ship_status === 'pending' && o.expect_ship_date && o.expect_ship_date <= date
  )
  return { date, list, total: list.length }
}

function specs(params = {}) {
  const all = dataset().specs
  const list = String(params.all) === '1' ? all : all.filter((s) => s.enabled !== false)
  return { list, total: list.length }
}

const READ_ROUTES = [
  [/^\/api\/stats\/dashboard/, (m, o) => dashboard(o.data)],
  [/^\/api\/stats\/ship-plan/, (m, o) => shipPlan(o.data)],
  [/^\/api\/specs$/, (m, o) => specs(o.data)],
  [/^\/api\/addresses$/, () => ({ list: [], total: 0 })],
  [/^\/api\/orders\/by-no\/(.+)$/, (m) => dataset().orders.find((o) => o.order_no === m[1]) || null],
  [/^\/api\/orders\/(\d+)$/, (m) => dataset().orders.find((o) => String(o.id) === m[1]) || null],
  [/^\/api\/orders$/, (m, o) => listOrders(o.data)]
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
