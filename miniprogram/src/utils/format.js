/* 金额一律以「分」为单位的整数在前端流转，禁止 parseFloat 之后再乘除。 */

export function fenToYuan(fen, opts) {
  const o = opts || {};
  const n = Math.round(Number(fen) || 0);
  const neg = n < 0;
  const abs = Math.abs(n);
  const yuan = Math.floor(abs / 100);
  const cent = abs % 100;
  let body;
  if (cent === 0 && !o.alwaysCents) {
    body = groupThousands(yuan);
  } else {
    body = groupThousands(yuan) + '.' + String(cent).padStart(2, '0');
  }
  const sign = neg ? '-' : '';
  return o.symbol === false ? sign + body : sign + '¥' + body;
}

function groupThousands(intValue) {
  const s = String(intValue);
  let out = '';
  for (let i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 === 0) out += ',';
    out += s[i];
  }
  return out;
}

/* 用户输入的元 → 分。整数运算，先按小数点切开再补位。 */
export function yuanToFen(input) {
  const s = String(input == null ? '' : input).trim().replace(/[¥,\s]/g, '');
  if (!s) return 0;
  const m = /^(-)?(\d*)(?:\.(\d*))?$/.exec(s);
  if (!m) return 0;
  const sign = m[1] ? -1 : 1;
  const yuan = m[2] ? parseInt(m[2], 10) : 0;
  const centStr = (m[3] || '').slice(0, 2).padEnd(2, '0');
  return sign * (yuan * 100 + parseInt(centStr, 10));
}

function pad2(n) { return String(n).padStart(2, '0'); }

function toDate(value) {
  if (value instanceof Date) return value;
  if (typeof value === 'number') return new Date(value);
  if (!value) return null;
  // iOS 不认 "2026-09-14 09:12:00"，统一换成 ISO 形态
  const s = String(value).replace(/-/g, '/').replace(/T/, ' ').replace(/\.\d+.*$/, '').replace(/Z$/, '');
  const d = new Date(s);
  return isNaN(d.getTime()) ? null : d;
}

/* pattern: YYYY-MM-DD / M月D日 / M月D日 HH:mm / HH:mm */
export function formatDate(value, pattern) {
  const d = toDate(value);
  if (!d) return '';
  const p = pattern || 'YYYY-MM-DD';
  return p
    .replace('YYYY', d.getFullYear())
    .replace('MM', pad2(d.getMonth() + 1))
    .replace('DD', pad2(d.getDate()))
    .replace(/\bM\b/, d.getMonth() + 1)
    .replace(/\bD\b/, d.getDate())
    .replace('HH', pad2(d.getHours()))
    .replace('mm', pad2(d.getMinutes()));
}

const WEEK = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六'];

export function weekdayText(value) {
  const d = toDate(value);
  return d ? WEEK[d.getDay()] : '';
}

export function todayStr() { return formatDate(new Date(), 'YYYY-MM-DD'); }

/* 相对日期：今天 / 昨天 / 9月12日 */
export function friendlyDay(value) {
  const d = toDate(value);
  if (!d) return '';
  const day = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = Math.round((today - day) / 86400000);
  if (diff === 0) return '今天';
  if (diff === 1) return '昨天';
  if (diff === 2) return '前天';
  return formatDate(d, 'M月D日');
}

/* 约定发货日已过且仍待发 → 超期天数 */
export function overdueDays(planShipDate, shipStatus) {
  if (shipStatus !== 'pending' || !planShipDate) return 0;
  const d = toDate(planShipDate);
  if (!d) return 0;
  const plan = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = Math.round((today - plan) / 86400000);
  return diff > 0 ? diff : 0;
}

/* 后端返回的 *_text 优先，本地映射只作兜底 */
export const SHIP_TEXT = { pending: '待发货', shipped: '已发货', received: '已收货', cancelled: '已取消' };
export const PAY_TEXT = { unpaid: '未收款', partial: '收了定金', paid: '已收清' };

/* 完成态一律灰色，只有需要动手的才有颜色 */
const SHIP_COLOR = { pending: 'claw', shipped: 'shell', received: 'muted', cancelled: 'muted' };
const PAY_COLOR = { unpaid: 'boiled', partial: 'roe', paid: 'muted' };

export function statusText(kind, value, fallbackText) {
  if (fallbackText) return fallbackText;
  const map = kind === 'pay' ? PAY_TEXT : SHIP_TEXT;
  return map[value] || value || '';
}

export function statusColor(kind, value) {
  const map = kind === 'pay' ? PAY_COLOR : SHIP_COLOR;
  return map[value] || 'muted';
}

export function maskPhone(phone) {
  const s = String(phone || '');
  return s.length === 11 ? s.slice(0, 3) + '****' + s.slice(7) : s;
}

export function maskName(name) {
  const s = String(name || '');
  return s ? s[0] + '*'.repeat(Math.max(s.length - 1, 1)) : '';
}
