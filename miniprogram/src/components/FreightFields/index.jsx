import { useEffect, useState } from 'react'
import { View, Text, Input } from '@tarojs/components'
import { fenToYuan, yuanToFen } from '../../utils/format'
import { suggestBuyerFee, freightProblem } from '../../utils/freight'
import './index.scss'

const BASES = [
  { key: 'actual', label: '按实付算' },
  { key: 'list', label: '按原价算' }
]

const yuan = (fen) => fenToYuan(fen, { symbol: false, alwaysCents: true })

/**
 * 运费输入：原价、实付、按哪个算、买家补多少。
 * 买家补的金额默认跟着建议值走；卖家一改就以卖家为准，可以一键恢复建议。
 * optional 为真时（发货抽屉里）可以整块留空，以后再补。
 * onChange 收到 { empty, problem, payload }，payload 直接就是接口要的字段。
 */
export default function FreightFields({ order, visible, optional, onChange }) {
  const [list, setList] = useState('')
  const [cost, setCost] = useState('')
  const [basis, setBasis] = useState('actual')
  const [buyer, setBuyer] = useState('')
  const [touched, setTouched] = useState(false)

  useEffect(() => {
    if (!visible || !order) return
    const filled = order.freight_cost != null
    setList(order.freight_list != null && order.freight_list !== order.freight_cost ? yuan(order.freight_list) : '')
    setCost(filled ? yuan(order.freight_cost) : '')
    setBasis(order.freight_basis || order.freight_default_basis || 'actual')
    setBuyer(filled ? yuan(order.freight_fee) : '')
    // 已经填过的单，买家补的金额是卖家定过的，别被建议值冲掉
    setTouched(filled)
  }, [visible, order])

  const cap = (order && order.freight_seller_cap) || 0
  const listFen = yuanToFen(list)
  const costFen = cost === '' ? null : yuanToFen(cost)
  const suggest = suggestBuyerFee(cap, basis, listFen, costFen || 0)
  const buyerFen = touched ? yuanToFen(buyer) : suggest
  const empty = cost === '' && list === ''
  const problem = empty && optional ? '' : freightProblem({ basis, listFen, costFen, buyerFen })

  useEffect(() => {
    if (!onChange) return
    onChange({
      empty,
      problem,
      payload: empty ? null : {
        freight_list: listFen || null,
        freight_cost: costFen,
        freight_basis: basis,
        freight_fee: buyerFen
      }
    })
    // 只看会影响结果的几个值，onChange 每次渲染都是新函数
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [empty, problem, listFen, costFen, basis, buyerFen])

  const seller = costFen == null ? 0 : costFen - buyerFen

  return (
    <View className='freight-fields'>
      <View className='freight-fields__pair'>
        <View className='freight-fields__cell'>
          <Text className='sub'>快递原价</Text>
          <Input
            className='freight-fields__input num'
            type='digit'
            value={list}
            placeholder='选填'
            onInput={(e) => setList(e.detail.value)}
          />
        </View>
        <View className='freight-fields__cell'>
          <Text className='sub'>用券后实付</Text>
          <Input
            className='freight-fields__input num'
            type='digit'
            value={cost}
            placeholder='0.00'
            onInput={(e) => setCost(e.detail.value)}
          />
        </View>
      </View>

      <View className='freight-fields__chips'>
        {BASES.map((b) => (
          <Text
            key={b.key}
            className={`freight-fields__chip ${basis === b.key ? 'freight-fields__chip--on' : ''}`}
            onClick={() => setBasis(b.key)}
          >
            {b.label}
          </Text>
        ))}
      </View>

      <View className='freight-fields__row'>
        <Text className='sub'>买家补</Text>
        <Input
          className='freight-fields__input freight-fields__input--short num'
          type='digit'
          value={touched ? buyer : yuan(suggest)}
          onInput={(e) => { setTouched(true); setBuyer(e.detail.value) }}
        />
        {touched && buyerFen !== suggest ? (
          <Text className='freight-fields__link' onClick={() => { setTouched(false); setBuyer('') }}>
            用建议 {fenToYuan(suggest)}
          </Text>
        ) : null}
      </View>

      <Text className='sub freight-fields__hint'>
        这单 {(order && order.crab_count) || 0} 只，卖家最多补 {fenToYuan(cap)}
        {costFen != null ? `，你实际担 ${fenToYuan(seller)}` : ''}。原价、实付只有你看得到。
      </Text>
      {problem ? <Text className='freight-fields__problem'>{problem}</Text> : null}
    </View>
  )
}
