/* 运费建议值。规则在后端（按只数分档、按建单时的版本），这里只拿后端算好的
 * freight_seller_cap（这一单卖家最多补多少）做一次减法，好让卖家边填边看到建议。 */

/** 买家建议补多少（分）：口径金额超过卖家补贴的部分。 */
export function suggestBuyerFee(sellerCap, basis, listFen, costFen) {
  const amount = basis === 'list' ? listFen : costFen
  if (!amount || amount <= 0) return 0
  return Math.max(0, amount - (sellerCap || 0))
}

/** 按原价算却没填原价、或实付没填，都不能提交。返回出错原因，没问题返回空串。 */
export function freightProblem({ basis, listFen, costFen, buyerFen }) {
  if (!costFen && costFen !== 0) return '实付运费还没填'
  if (costFen < 0 || listFen < 0 || buyerFen < 0) return '金额不能是负数'
  if (basis === 'list' && !listFen) return '按原价算，要先填原价'
  const cap = basis === 'list' ? listFen : costFen
  if (buyerFen > cap) return `买家补的不能超过${basis === 'list' ? '原价' : '实付'}`
  return ''
}
