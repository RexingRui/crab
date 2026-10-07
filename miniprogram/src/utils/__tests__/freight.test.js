import { describe, it, expect } from 'vitest'
import { suggestBuyerFee, freightProblem } from '../freight'

describe('suggestBuyerFee', () => {
  it('16 只运费 60，卖家补 40，买家补 20', () => {
    expect(suggestBuyerFee(4000, 'actual', 7500, 6000)).toBe(2000)
  })
  it('按原价算用原价', () => {
    expect(suggestBuyerFee(4000, 'list', 7500, 6000)).toBe(3500)
  })
  it('没超过补贴就是 0', () => {
    expect(suggestBuyerFee(2000, 'actual', 0, 1800)).toBe(0)
  })
  it('没填金额就是 0', () => {
    expect(suggestBuyerFee(2000, 'list', 0, 3000)).toBe(0)
  })
})

describe('freightProblem', () => {
  it('实付必填', () => {
    expect(freightProblem({ basis: 'actual', listFen: 0, costFen: null, buyerFen: 0 })).toBeTruthy()
  })
  it('按原价要有原价', () => {
    expect(freightProblem({ basis: 'list', listFen: 0, costFen: 3000, buyerFen: 0 })).toMatch('原价')
  })
  it('买家补的可以超过实付和原价（大额券寄的单）', () => {
    expect(freightProblem({ basis: 'actual', listFen: 5800, costFen: 4000, buyerFen: 6000 })).toBe('')
  })
  it('买家补的不能是负数', () => {
    expect(freightProblem({ basis: 'actual', listFen: 0, costFen: 3000, buyerFen: -1 })).toBeTruthy()
  })
  it('正常情况没问题', () => {
    expect(freightProblem({ basis: 'actual', listFen: 7500, costFen: 6000, buyerFen: 2000 })).toBe('')
  })
})
