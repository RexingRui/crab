import { View, Text } from '@tarojs/components'
import StatusTag from '../StatusTag'
import { fenToYuan, overdueDays } from '../../utils/format'
import './index.scss'

/**
 * 列表里的一张单。只吃后端给的 items_summary，不为了显示明细去挨个请求详情。
 */
export default function OrderCard({ order, onClick, onShip, showShipButton }) {
  if (!order) return null
  const overdue = overdueDays(order.plan_ship_date, order.ship_status)
  const tone = overdue > 0 ? 'boiled' : order.ship_status === 'pending' ? 'claw' : 'none'

  return (
    <View className={`order-card order-card--bar-${tone}`} onClick={() => onClick && onClick(order)}>
      <View className='order-card__top'>
        <Text className='order-card__name'>{order.receiver_name}</Text>
        <View className='order-card__tags'>
          {overdue > 0 ? (
            <Text className='order-card__overdue'>超期{overdue}天</Text>
          ) : null}
          <StatusTag kind='ship' value={order.ship_status} text={order.ship_status_text} />
          <StatusTag
            kind='pay'
            value={order.pay_status}
            text={order.pay_status_text}
            muted={order.ship_status === 'cancelled'}
          />
        </View>
      </View>

      <Text className='order-card__summary'>{order.items_summary}</Text>

      <View className='order-card__bottom'>
        <Text className='order-card__no num'>{order.order_no}</Text>
        {showShipButton ? (
          <Text
            className='order-card__ship'
            onClick={(e) => {
              e.stopPropagation()
              onShip && onShip(order)
            }}
          >
            发货
          </Text>
        ) : (
          <Text className='order-card__money money'>{fenToYuan(order.total_amount)}</Text>
        )}
      </View>
    </View>
  )
}
