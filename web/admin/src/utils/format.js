/* 与小程序端 utils/format.js 同一套规则：金额一律是「分」的整数，
 * 分转元用整数运算，禁止 parseFloat 之后再乘除。 */

export function fenToYuan(fen, { symbol = true, alwaysCents = false } = {}) {
  const n = Math.round(Number(fen) || 0)
  const neg = n < 0
  const abs = Math.abs(n)
  const yuan = Math.floor(abs / 100)
  const cent = abs % 100
  const body = cent === 0 && !alwaysCents
    ? group(yuan)
    : `${group(yuan)}.${String(cent).padStart(2, '0')}`
  return `${neg ? '-' : ''}${symbol ? '¥' : ''}${body}`
}

function group(int) {
  return String(int).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

export function yuanToFen(input) {
  const s = String(input ?? '').trim().replace(/[¥,\s]/g, '')
  if (!s) return 0
  const m = /^(-)?(\d*)(?:\.(\d*))?$/.exec(s)
  if (!m) return 0
  const sign = m[1] ? -1 : 1
  const yuan = m[2] ? parseInt(m[2], 10) : 0
  const cent = (m[3] || '').slice(0, 2).padEnd(2, '0')
  return sign * (yuan * 100 + parseInt(cent, 10))
}

export const SHIP_TEXT = { pending: '待发货', shipped: '已发货', received: '已收货', cancelled: '已取消' }
export const PAY_TEXT = { unpaid: '未收款', partial: '收了定金', paid: '已收清' }

// 完成态一律灰，只有需要动手的才有颜色
export const SHIP_COLOR = { pending: 'claw', shipped: 'shell', received: 'muted', cancelled: 'muted' }
export const PAY_COLOR = { unpaid: 'boiled', partial: 'roe', paid: 'muted' }

export function statusText(kind, value, fallback) {
  if (fallback) return fallback
  return (kind === 'pay' ? PAY_TEXT : SHIP_TEXT)[value] || value || ''
}

export function statusColor(kind, value) {
  return (kind === 'pay' ? PAY_COLOR : SHIP_COLOR)[value] || 'muted'
}

/** 约定发货日已过且仍待发 → 超期天数 */
export function overdueDays(planShipDate, shipStatus) {
  if (shipStatus !== 'pending' || !planShipDate) return 0
  const d = new Date(String(planShipDate).replace(/-/g, '/'))
  if (isNaN(d.getTime())) return 0
  const plan = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const diff = Math.round((today - plan) / 86400000)
  return diff > 0 ? diff : 0
}
