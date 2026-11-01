import { useEffect, useState } from 'react'
import { View, Text, Input } from '@tarojs/components'
import Sheet, { CURSOR_SPACING } from '../Sheet'
import { fenToYuan, yuanToFen } from '../../utils/format'
import './index.scss'

// 词条里不留电商语境的字眼；其他渠道进来的钱记「转账」或「其他」＋备注
const METHODS = [
  { key: 'wechat', label: '微信' },
  { key: 'cash', label: '现金' },
  { key: 'transfer', label: '转账' },
  { key: 'other', label: '其他' }
]

/** 记一笔收款或退款。
 *
 * 数字键盘（type='digit'）没有负号，没法直接填负数，所以退款单独做成一个选项：
 * 金额永远填正数，选「退回」时提交成负数。后端的口径不变（负数就是退款）。 */
export default function PaymentSheet({ visible, order, submitting, onClose, onSubmit }) {
  const [refund, setRefund] = useState(false)
  const [amount, setAmount] = useState('')
  const [method, setMethod] = useState('wechat')
  const [remark, setRemark] = useState('')
  const [prefill, setPrefill] = useState('')

  useEffect(() => {
    if (!visible) return
    // 还差钱就默认收这么多；多收了就默认退多出来的部分；正好收清就留空
    const rest = order ? order.unpaid_amount || 0 : 0
    const text = rest ? fenToYuan(Math.abs(rest), { symbol: false, alwaysCents: true }) : ''
    setRefund(rest < 0)
    setAmount(text)
    setPrefill(text)
    setMethod('wechat')
    setRemark('')
  }, [visible, order])

  // 带出来的默认金额只对默认方向有意义（还差的钱不是该退的钱），换方向就清掉；自己填的不动
  function pickDirection(next) {
    if (next === refund) return
    setRefund(next)
    if (amount === prefill) setAmount('')
  }

  // 粘贴进来的「-50」也按 50 算，正负只看上面选的是收还是退
  const fen = Math.abs(yuanToFen(amount)) * (refund ? -1 : 1)
  const ready = fen !== 0

  return (
    <Sheet
      visible={visible}
      title={refund ? '记一笔退款' : '记一笔收款'}
      onClose={onClose}
      footer={
        <View
          className={`btn btn--primary btn--block ${ready && !submitting ? '' : 'btn--off'}`}
          onClick={ready && !submitting ? () => onSubmit({ amount: fen, pay_method: method, paid_at: null, remark }) : undefined}
        >
          {submitting ? '提交中' : '记下'}
        </View>
      }
    >
      <View className='payment-sheet__chips payment-sheet__dir'>
        <Text
          className={`payment-sheet__chip ${refund ? '' : 'payment-sheet__chip--on'}`}
          onClick={() => pickDirection(false)}
        >
          收到
        </Text>
        <Text
          className={`payment-sheet__chip ${refund ? 'payment-sheet__chip--on' : ''}`}
          onClick={() => pickDirection(true)}
        >
          退回
        </Text>
      </View>

      <Text className='sub payment-sheet__gap'>
        {refund ? '退回去多少（记错了的收款也用这个冲掉）' : '收到多少'}
      </Text>
      <Input
        cursorSpacing={CURSOR_SPACING}
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
        cursorSpacing={CURSOR_SPACING}
        className='payment-sheet__input'
        value={remark}
        placeholder='比如：定金、补运费'
        onInput={(e) => setRemark(e.detail.value)}
      />
    </Sheet>
  )
}
