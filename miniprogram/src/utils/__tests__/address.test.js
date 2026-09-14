import { describe, it, expect } from 'vitest'
import { parseAddress, looksLikeAddressText } from '../address'

const EXPECT = {
  name: '张三',
  phone: '13800138000',
  address: '江苏省苏州市工业园区xx路88号3栋201'
}

describe('技术方案第 4.3.1 节要求的四种真实输入', () => {
  const cases = [
    ['空格分隔，姓名在前', '张三 13800138000 江苏省苏州市工业园区xx路88号3栋201'],
    ['带标签词与全角冒号', '收货人：张三  电话：13800138000  地址：江苏省苏州市工业园区xx路88号3栋201'],
    ['地址在前、号码带连字符', '江苏省苏州市工业园区xx路88号3栋201 张三 138-0013-8000'],
    ['逗号分隔', '张三,13800138000,江苏省苏州市工业园区xx路88号3栋201']
  ]
  it.each(cases)('%s', (_title, input) => {
    expect(parseAddress(input)).toEqual(EXPECT)
  })
})

describe('parseAddress 其余情形', () => {
  it('号码里的空格分隔也要认', () => {
    expect(parseAddress('张三 138 0013 8000 江苏省苏州市工业园区xx路88号3栋201')).toEqual(EXPECT)
  })

  it('姓名和地址粘在一起时从开头切人名', () => {
    expect(parseAddress('张三13800138000江苏省苏州市工业园区xx路88号3栋201')).toEqual(EXPECT)
  })

  it('不把省名误当人名', () => {
    const r = parseAddress('13800138000 江苏省苏州市工业园区xx路88号3栋201')
    expect(r.name).toBe('')
    expect(r.phone).toBe('13800138000')
    expect(r.address).toBe(EXPECT.address)
  })

  it('三项任一为空，也要把能解析的填回去', () => {
    expect(parseAddress('李四 13900139000')).toEqual({ name: '李四', phone: '13900139000', address: '' })
    const r = parseAddress('上海市浦东新区世纪大道100号')
    expect(r.phone).toBe('')
    expect(r.address).toBe('上海市浦东新区世纪大道100号')
  })

  it('换行分隔的多行文本', () => {
    expect(parseAddress('收货人\n张三\n手机\n13800138000\n地址\n江苏省苏州市工业园区xx路88号3栋201')).toEqual(EXPECT)
  })

  it('四字姓名', () => {
    const r = parseAddress('欧阳青青 13700137000 浙江省杭州市西湖区文三路9号')
    expect(r.name).toBe('欧阳青青')
    expect(r.address).toBe('浙江省杭州市西湖区文三路9号')
  })

  it('第一个手机号优先，座机不误伤', () => {
    expect(parseAddress('张三 0512-66668888 13800138000 江苏省苏州市工业园区xx路88号3栋201').phone)
      .toBe('13800138000')
  })

  it('非法输入返回空结构而不是抛错', () => {
    const empty = { name: '', phone: '', address: '' }
    expect(parseAddress('')).toEqual(empty)
    expect(parseAddress(null)).toEqual(empty)
    expect(parseAddress(123)).toEqual(empty)
  })
})

describe('looksLikeAddressText 决定要不要弹横幅', () => {
  it('长度 > 15 且有号码特征才提示', () => {
    expect(looksLikeAddressText('张三 13800138000 江苏省苏州市工业园区xx路88号')).toBe(true)
    expect(looksLikeAddressText('13800138000')).toBe(false)
    expect(looksLikeAddressText('明天记得去阳澄湖看看水温怎么样了')).toBe(false)
    expect(looksLikeAddressText('')).toBe(false)
    expect(looksLikeAddressText(undefined)).toBe(false)
  })
})
