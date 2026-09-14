import { useEffect, useState } from 'react'
import { View, Text, Input } from '@tarojs/components'
import Sheet from '../Sheet'
import './index.scss'

const COMPANIES = ['顺丰速运', '京东物流', '中通快递', '圆通速递', '韵达快递', '德邦快递']

/** 发货抽屉：填快递公司 + 运单号，首页卡片和详情页共用 */
export default function ShipSheet({ visible, order, submitting, onClose, onSubmit }) {
  const [company, setCompany] = useState(COMPANIES[0])
  const [trackingNo, setTrackingNo] = useState('')

  useEffect(() => {
    if (!visible) return
    setCompany((order && order.ship_company) || COMPANIES[0])
    setTrackingNo((order && order.tracking_no) || '')
  }, [visible, order])

  const ready = Boolean(company && trackingNo.trim())

  return (
    <Sheet
      visible={visible}
      title={order ? `发货 · ${order.receiver_name}` : '发货'}
      onClose={onClose}
      footer={
        <View
          className={`btn btn--primary btn--block ${ready && !submitting ? '' : 'btn--off'}`}
          onClick={ready && !submitting ? () => onSubmit({ ship_company: company, tracking_no: trackingNo.trim() }) : undefined}
        >
          {submitting ? '提交中' : '确认发货'}
        </View>
      }
    >
      <Text className='sub'>快递公司</Text>
      <View className='ship-sheet__chips'>
        {COMPANIES.map((name) => (
          <Text
            key={name}
            className={`ship-sheet__chip ${company === name ? 'ship-sheet__chip--on' : ''}`}
            onClick={() => setCompany(name)}
          >
            {name}
          </Text>
        ))}
      </View>

      <Text className='sub ship-sheet__gap'>运单号</Text>
      <Input
        className='ship-sheet__input num'
        value={trackingNo}
        placeholder='粘贴或输入运单号'
        onInput={(e) => setTrackingNo(e.detail.value)}
      />
    </Sheet>
  )
}
