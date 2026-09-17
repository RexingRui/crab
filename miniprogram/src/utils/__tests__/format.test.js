import { describe, it, expect } from 'vitest'
import {
  fenToYuan, yuanToFen, overdueDays, statusText, statusColor, maskPhone, maskName, formatDate,
  itemName
} from '../format'

describe('金额换算走整数运算', () => {
  it('分转元', () => {
    expect(fenToYuan(79000)).toBe('¥790')
    expect(fenToYuan(456012)).toBe('¥4,560.12')
    expect(fenToYuan(5)).toBe('¥0.05')
    expect(fenToYuan(-4000)).toBe('-¥40')
    expect(fenToYuan(0)).toBe('¥0')
    expect(fenToYuan(79000, { symbol: false })).toBe('790')
    expect(fenToYuan(79000, { alwaysCents: true })).toBe('¥790.00')
  })

  it('元转分不经过浮点乘法', () => {
    expect(yuanToFen('88')).toBe(8800)
    expect(yuanToFen('88.5')).toBe(8850)
    expect(yuanToFen('0.07')).toBe(7)
    expect(yuanToFen('1.005')).toBe(100)
    expect(yuanToFen('¥1,180.05')).toBe(118005)
    expect(yuanToFen('-40')).toBe(-4000)
    expect(yuanToFen('')).toBe(0)
    expect(yuanToFen('abc')).toBe(0)
  })
})

describe('日期与超期', () => {
  it('formatDate 认得带 T 和空格两种写法', () => {
    expect(formatDate('2026-09-14 09:12:00', 'YYYY-MM-DD')).toBe('2026-09-14')
    expect(formatDate('2026-09-14 09:12:00', 'M月D日 HH:mm')).toBe('9月14日 09:12')
    expect(formatDate('', 'YYYY-MM-DD')).toBe('')
  })

  it('超期天数只在待发货时计算', () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 86400000)
    expect(overdueDays(twoDaysAgo, 'pending')).toBe(2)
    expect(overdueDays(twoDaysAgo, 'shipped')).toBe(0)
    expect(overdueDays(new Date(Date.now() + 86400000), 'pending')).toBe(0)
    expect(overdueDays('', 'pending')).toBe(0)
  })
})

describe('状态文案与配色', () => {
  it('后端给的文案优先，本地映射兜底', () => {
    expect(statusText('ship', 'pending')).toBe('待发货')
    expect(statusText('ship', 'pending', '待发出')).toBe('待发出')
    expect(statusText('pay', 'partial')).toBe('收了定金')
  })

  it('完成态用灰，只有待办才有颜色', () => {
    expect(statusColor('ship', 'pending')).toBe('claw')
    expect(statusColor('ship', 'received')).toBe('muted')
    expect(statusColor('ship', 'cancelled')).toBe('muted')
    expect(statusColor('pay', 'unpaid')).toBe('boiled')
    expect(statusColor('pay', 'paid')).toBe('muted')
  })
})

describe('脱敏', () => {
  it('手机号与姓名', () => {
    expect(maskPhone('13800138000')).toBe('138****8000')
    expect(maskName('张三')).toBe('张*')
  })
})

describe('itemName', () => {
  it('套餐不加「公母」前缀，档名本来就写了盒里装什么', () => {
    expect(itemName({
      gender: 'mixed', gender_text: '公母',
      spec_label: '8只装 母2.5两/公3.5两（公6母2）'
    })).toBe('8只装 母2.5两/公3.5两（公6母2）')
  })

  it('按只卖的照旧带前缀', () => {
    expect(itemName({ gender: 'male', gender_text: '公', spec_label: '4.5两' })).toBe('公 4.5两')
  })

  it('空值不炸', () => {
    expect(itemName(null)).toBe('')
    expect(itemName({ gender: 'female', spec_label: '3.5两' })).toBe('3.5两')
  })
})
