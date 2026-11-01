import { useCallback, useState } from 'react'
import Taro, { useDidShow, usePullDownRefresh } from '@tarojs/taro'
import { View, Text } from '@tarojs/components'
import DemoBanner from '../../components/DemoBanner'
import api from '../../utils/api'
import { fenToYuan, formatDate, todayStr } from '../../utils/format'
import { whenReady } from '../../utils/session'
import './index.scss'

/* 总账：一段时间里记了多少单、收到多少钱、运费花了多少。
 * 首页只管「今天要做什么」，回头看账的数字都放这一页。 */

const DAY = 86400000

// 「全部」用一个足够早的起点，后端只认 YYYY-MM-DD
const PERIODS = [
  { key: 'all', label: '全部', start: () => '2020-01-01' },
  { key: 'month', label: '本月', start: () => formatDate(new Date(), 'YYYY-MM') + '-01' },
  { key: 'week', label: '近 7 天', start: () => formatDate(new Date(Date.now() - 6 * DAY), 'YYYY-MM-DD') },
  { key: 'today', label: '今天', start: () => todayStr() }
]

function Line({ label, value, hint, strong, tone, onClick }) {
  return (
    <View className={`stats__line ${onClick ? 'stats__line--link' : ''}`} onClick={onClick}>
      <View className='stats__line-label'>
        <Text>{label}</Text>
        {hint ? <Text className='sub stats__line-hint'>{hint}</Text> : null}
      </View>
      <Text className={`num stats__line-value ${strong ? 'stats__line-value--strong' : ''} ${tone ? `stats__line-value--${tone}` : ''}`}>
        {value}
      </Text>
      {onClick ? <Text className='stats__line-arrow'>›</Text> : null}
    </View>
  )
}

export default function Stats() {
  const [period, setPeriod] = useState('all')
  const [dash, setDash] = useState(null)

  const load = useCallback(async (key) => {
    await whenReady()
    const p = PERIODS.find((x) => x.key === key) || PERIODS[0]
    const d = await api.dashboard({ start: p.start(), end: todayStr() }).catch(() => null)
    if (d) setDash(d)
  }, [])

  useDidShow(() => { load(period) })

  usePullDownRefresh(() => {
    load(period).finally(() => Taro.stopPullDownRefresh())
  })

  function pick(key) {
    if (key === period) return
    setPeriod(key)
    setDash(null)
    load(key)
  }

  // switchTab 不能带参数，用事件把筛选条件递给列表页
  function goList(params) {
    Taro.switchTab({ url: '/pages/orders/index' }).then(() => {
      Taro.eventCenter.trigger('orders:filter', params)
    })
  }

  const range = (dash && dash.range) || {}
  const pending = (dash && dash.pending) || {}
  const freight = (dash && dash.freight) || {}
  const bySpec = range.by_spec || []
  const ready = Boolean(dash)
  const money = (v) => (ready ? fenToYuan(v || 0) : '—')
  const count = (v, unit) => (ready ? `${v || 0} ${unit}` : '—')

  return (
    <View className='page'>
      <DemoBanner />

      <View className='stats__periods'>
        {PERIODS.map((p) => (
          <Text
            key={p.key}
            className={`stats__period ${period === p.key ? 'stats__period--on' : ''}`}
            onClick={() => pick(p.key)}
          >
            {p.label}
          </Text>
        ))}
      </View>

      <View className='section'>
        <View className='section__head'>
          <Text className='group-title'>记了多少</Text>
          {range.cancelled_count ? <Text className='sub'>另有 {range.cancelled_count} 单取消了，不算在内</Text> : null}
        </View>
        <View className='card'>
          <Line label='订单' value={count(range.order_count, '单')} />
          <View className='divider' />
          <Line label='蟹' value={count(range.crab_count, '只')} />
          <View className='divider' />
          <Line label='应收合计' hint='货款 + 买家补的运费 − 优惠' value={money(range.payable_total)} strong />
        </View>
      </View>

      <View className='section'>
        <View className='section__head'>
          <Text className='group-title'>钱</Text>
        </View>
        <View className='card'>
          <Line label='收到的钱' hint='按收款那天算，退回的已扣掉' value={money(range.received_total)} strong />
          <View className='divider' />
          <Line
            label='还没收到'
            hint='眼下所有没收清的单，不分时间'
            value={money(pending.unpaid_amount)}
            tone={pending.unpaid_amount > 0 ? 'claw' : ''}
            onClick={() => goList({ pay_status: 'unpaid,partial' })}
          />
        </View>
      </View>

      <View className='section'>
        <View className='section__head'>
          <Text className='group-title'>运费</Text>
          {ready ? <Text className='sub'>{freight.range_count || 0} 单填了运费</Text> : null}
        </View>
        <View className='card'>
          <Line label='快递实付' hint='用券后真正花出去的' value={money(freight.cost_total)} strong />
          <View className='divider' />
          <Line label='买家补了' value={money(freight.buyer_total)} />
          <View className='divider' />
          <Line label='我贴了' hint='实付 − 买家补的，负数是赚了' value={money(freight.seller_total)} />
          {freight.saved_total ? (
            <>
              <View className='divider' />
              <Line label='用券省下' hint='快递原价 − 实付' value={money(freight.saved_total)} />
            </>
          ) : null}
          <View className='divider' />
          <Line
            label='还没和快递结'
            hint='眼下，不分时间'
            value={ready ? `${fenToYuan(freight.unsettled_cost || 0)} · ${freight.unsettled_count || 0} 单` : '—'}
            tone={freight.unsettled_count > 0 ? 'claw' : ''}
            onClick={() => goList({ freight: 'unsettled' })}
          />
          {freight.shipped_pending_count ? (
            <>
              <View className='divider' />
              <Line
                label='发了还没填运费'
                value={`${freight.shipped_pending_count} 单`}
                tone='claw'
                onClick={() => goList({ freight: 'pending', ship_status: 'shipped' })}
              />
            </>
          ) : null}
        </View>
      </View>

      {bySpec.length ? (
        <View className='section'>
          <View className='section__head'>
            <Text className='group-title'>按规格</Text>
          </View>
          <View className='card'>
            {bySpec.map((s, i) => (
              <View key={`${s.gender}-${s.spec_gram}-${s.grade}`}>
                {i ? <View className='divider' /> : null}
                <View className='stats__spec'>
                  <Text className='stats__spec-name'>
                    {s.gender === 'male' ? '公' : s.gender === 'female' ? '母' : ''}{s.spec_label}{s.grade === 'broken' ? '(残)' : ''}
                  </Text>
                  <Text className='num stats__spec-qty'>{s.quantity} 只</Text>
                  <Text className='num stats__spec-amount'>{fenToYuan(s.amount)}</Text>
                </View>
              </View>
            ))}
          </View>
        </View>
      ) : null}

      <Text className='stats__foot sub'>订单、只数、应收和按规格按「哪天记的单」算。</Text>
    </View>
  )
}
