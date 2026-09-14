import { useEffect, useState } from 'react'
import { View, Text, Input } from '@tarojs/components'
import Sheet from '../Sheet'
import { fenToYuan, yuanToFen } from '../../utils/format'
import './index.scss'

// 词条里不留电商语境的字眼；其他渠道进来的钱记「转账」或「其他」＋备注
const METHODS = [
  { key: 'wechat', label: '微信' },
  { key: 'cash', label: '现金' },
  { key: 'transfer', label: '转账' },
  { key: 'other', label: '其他' }
]

/** 记一笔收款。金额默认带入还差的部分，支持填负数做退款。 */
export default function PaymentSheet({ visible, order, submitting, onClose, onSubmit }) {
  const [amount, setAmount] = useState('')
  const [method, setMethod] = useState('wechat')
  const [remark, setRemark] = useState('')

  useEffect(() => {
    if (!visible) return
    const rest = order ? Math.max(0, order.unpaid_amount || 0) : 0
    setAmount(rest ? fenToYuan(rest, { symbol: false, alwaysCents: true }) : '')
    setMethod('wechat')
    setRemark('')
  }, [visible, order])

  const fen = yuanToFen(amount)
  const ready = fen !== 0

  return (
    <Sheet
      visible={visible}
      title='记一笔收款'
      onClose={onClose}
      footer={
        <View
          className={`btn btn--primary btn--block ${ready && !submitting ? '' : 'btn--off'}`}
          onClick={ready && !submitting ? () => onSubmit({ amount: fen, method, remark }) : undefined}
        >
          {submitting ? '提交中' : '记下'}
        </View>
      }
    >
      <Text className='sub'>金额（填负数就是退回去的钱）</Text>
      <Input
        className='payment-sheet__amount num'
        type='digit'
        value={amount}
        placeholder='0.00'
        onInput={(e) => setAmount(e.detail.value)}
      />

      <Text className='sub payment-sheet__gap'>方式</Text>
      <View className='payment-sheet__chips'>
        {METHODS.map((m) => (
          <Text
            key={m.key}
            className={`payment-sheet__chip ${method === m.key ? 'payment-sheet__chip--on' : ''}`}
            onClick={() => setMethod(m.key)}
          >
            {m.label}
          </Text>
        ))}
      </View>

      <Text className='sub payment-sheet__gap'>备注</Text>
      <Input
        className='payment-sheet__input'
        value={remark}
        placeholder='比如：定金'
        onInput={(e) => setRemark(e.detail.value)}
      />
    </Sheet>
  )
}
