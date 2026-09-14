import { useCallback, useState } from 'react'
import Taro, { useDidShow, usePullDownRefresh } from '@tarojs/taro'
import { View, Text } from '@tarojs/components'
import DemoBanner from '../../components/DemoBanner'
import OrderCard from '../../components/OrderCard'
import StatusTag from '../../components/StatusTag'
import Empty from '../../components/Empty'
import ShipSheet from '../../components/ShipSheet'
import api from '../../utils/api'
import { fenToYuan, formatDate, friendlyDay, weekdayText } from '../../utils/format'
import { whenReady } from '../../utils/session'
import { guardDemo } from '../../hooks/useDemoMode'
import { toast } from '../../utils/request'
import './index.scss'

/* 这一页的主角是今天要发的货的清单本身，不是统计数字。
 * 早上打开是为了知道「今天发哪几单」，一个大数字回答不了这个问题。 */

const PLAN_LIMIT = 5

export default function Home() {
  const [dash, setDash] = useState(null)
  const [plan, setPlan] = useState([])
  const [recent, setRecent] = useState([])
  const [shipTarget, setShipTarget] = useState(null)
  const [submitting, setSubmitting] = useState(false)

  const load = useCallback(async () => {
    await whenReady()
    const [d, p, r] = await Promise.all([
      api.dashboard().catch(() => null),
      api.shipPlan('today').catch(() => null),
      api.orders({ page: 1, page_size: 10 }).catch(() => null)
    ])
    if (d) setDash(d)
    setPlan((p && p.list) || [])
    setRecent((r && r.list) || [])
  }, [])

  useDidShow(() => { load() })

  usePullDownRefresh(() => {
    load().finally(() => Taro.stopPullDownRefresh())
  })

  async function doShip(payload) {
    if (guardDemo(toast)) return
    setSubmitting(true)
    try {
      await api.ship(shipTarget.id, payload)
      setShipTarget(null)
      await load()
      Taro.showToast({ title: '发出去了', icon: 'success' })
    } finally {
      setSubmitting(false)
    }
  }

  // switchTab 不能带参数，用事件把筛选条件递给列表页
  function goList(params) {
    Taro.switchTab({ url: '/pages/orders/index' }).then(() => {
      Taro.eventCenter.trigger('orders:filter', params)
    })
  }

  const today = new Date()
  const shown = plan.slice(0, PLAN_LIMIT)
  const restCount = Math.max(0, plan.length - PLAN_LIMIT)

  return (
    <View className='page'>
      <DemoBanner />

      <View className='home__head'>
        <View>
          <Text className='page-title'>今天</Text>
          <Text className='home__date sub'>
            {formatDate(today, 'M月D日')} {weekdayText(today)}
          </Text>
        </View>
        <Text
          className='home__gear'
          onClick={() => Taro.navigateTo({ url: '/pages/settings/index' })}
        >
          设置
        </Text>
      </View>

      <View className='section'>
        <View className='section__head'>
          <Text className='group-title'>今天要发的 {plan.length} 单</Text>
        </View>

        {shown.length === 0 ? (
          <View className='card'>
            <Empty text='今天没有要发的货。'>
              <View
                className='btn btn--primary'
                onClick={() => Taro.switchTab({ url: '/pages/record/index' })}
              >
                记一笔
              </View>
            </Empty>
          </View>
        ) : (
          <>
            {shown.map((order) => (
              <OrderCard
                key={order.id}
                order={order}
                showShipButton
                onShip={setShipTarget}
                onClick={(o) => Taro.navigateTo({ url: `/pages/detail/index?id=${o.id}` })}
              />
            ))}
            {restCount > 0 ? (
              <Text
                className='home__more'
                onClick={() => goList({ ship_status: 'pending' })}
              >
                还有 {restCount} 单 ›
              </Text>
            ) : null}
          </>
        )}
      </View>

      <View className='section card home__stats'>
        <View className='home__stat' onClick={() => goList({ pay_status: 'unpaid' })}>
          <Text className='home__stat-label'>还没收到钱</Text>
          <Text className='home__stat-value money'>
            {fenToYuan((dash && dash.unpaid_amount) || 0)}
            <Text className='sub'> · {(dash && dash.unpaid_count) || 0} 笔</Text>
          </Text>
          <Text className='home__stat-arrow'>›</Text>
        </View>
        <View className='divider' />
        <View className='home__stat' onClick={() => goList({ ship_status: 'shipped' })}>
          <Text className='home__stat-label'>发了还没签收</Text>
          <Text className='home__stat-value num'>{(dash && dash.shipped_count) || 0} 单</Text>
          <Text className='home__stat-arrow'>›</Text>
        </View>
      </View>

      <View className='section'>
        <View className='section__head'>
          <Text className='group-title'>最近记的</Text>
          <Text className='home__link' onClick={() => Taro.switchTab({ url: '/pages/orders/index' })}>全部 ›</Text>
        </View>
        <View className='card'>
          {recent.length === 0 ? (
            <Text className='home__blank sub'>还什么都没记。</Text>
          ) : (
            recent.map((order) => (
              <View
                className='home__recent'
                key={order.id}
                onClick={() => Taro.navigateTo({ url: `/pages/detail/index?id=${order.id}` })}
              >
                <Text className='home__recent-day sub'>{friendlyDay(order.created_at)}</Text>
                <Text className='home__recent-name'>{order.receiver_name}</Text>
                <Text className='home__recent-money money'>{fenToYuan(order.total_amount)}</Text>
                <View className='home__recent-tags'>
                  <StatusTag kind='ship' value={order.ship_status} text={order.ship_status_text} />
                  <StatusTag
                    kind='pay'
                    value={order.pay_status}
                    text={order.pay_status_text}
                    muted={order.ship_status === 'cancelled'}
                  />
                </View>
              </View>
            ))
          )}
        </View>
      </View>

      <ShipSheet
        visible={Boolean(shipTarget)}
        order={shipTarget}
        submitting={submitting}
        onClose={() => setShipTarget(null)}
        onSubmit={doShip}
      />
    </View>
  )
}
