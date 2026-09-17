import { useCallback, useState } from 'react'
import Taro, { useRouter, useDidShow } from '@tarojs/taro'
import { View, Text } from '@tarojs/components'
import DemoBanner from '../../components/DemoBanner'
import StatusTag from '../../components/StatusTag'
import ShipSheet from '../../components/ShipSheet'
import PaymentSheet from '../../components/PaymentSheet'
import api from '../../utils/api'
import { fenToYuan, formatDate, maskPhone, overdueDays, itemName } from '../../utils/format'
import { getTrackUrl, whenReady } from '../../utils/session'
import { guardDemo } from '../../hooks/useDemoMode'
import { toast } from '../../utils/request'
import './index.scss'

export default function Detail() {
  const router = useRouter()
  const id = router.params.id
  const [order, setOrder] = useState(null)
  const [shipOpen, setShipOpen] = useState(false)
  const [payOpen, setPayOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [logsOpen, setLogsOpen] = useState(false)
  const [flash, setFlash] = useState(false)

  const load = useCallback(async () => {
    await whenReady()
    try {
      setOrder(await api.order(id))
    } catch (e) { /* 提示已统一处理 */ }
  }, [id])

  useDidShow(() => { load() })

  if (!order) {
    return (
      <View className='page'>
        <DemoBanner />
        <Text className='detail__loading sub'>正在取这一单…</Text>
      </View>
    )
  }

  const overdue = overdueDays(order.expect_ship_date, order.ship_status)

  /* 状态变更后让标签闪一下，是这一页唯一的动效 */
  async function afterChange(msg) {
    await load()
    setFlash(true)
    setTimeout(() => setFlash(false), 700)
    Taro.showToast({ title: msg, icon: 'success' })
  }

  async function doShip(payload) {
    if (guardDemo(toast)) return
    setSubmitting(true)
    try {
      await api.ship(order.id, payload)
      setShipOpen(false)
      await afterChange('发出去了')
    } finally {
      setSubmitting(false)
    }
  }

  async function doPayment(payload) {
    if (guardDemo(toast)) return
    setSubmitting(true)
    try {
      await api.addPayment(order.id, payload)
      setPayOpen(false)
      await afterChange('记上了')
    } finally {
      setSubmitting(false)
    }
  }

  async function doReceive() {
    if (guardDemo(toast)) return
    const { confirm } = await Taro.showModal({ title: '确认收货？', content: '买家已经收到货了' })
    if (!confirm) return
    await api.confirmReceive(order.id)
    await afterChange('已收货')
  }

  function copy(text, tip) {
    Taro.setClipboardData({ data: text }).then(() => toast(tip))
  }

  /* 个人主体小程序没有 web-view，买家链接只能复制文本发过去 */
  function copyTrackText() {
    const link = `${getTrackUrl()}/t?no=${order.order_no}`
    copy(
      `您的大闸蟹订单 ${order.order_no}\n查看物流：${link}\n（需输入手机号后四位）`,
      '已复制，去微信粘贴给买家'
    )
  }

  async function more() {
    const actions = []
    if (order.ship_status === 'shipped') actions.push('回退到待发货')
    if (order.ship_status === 'received') actions.push('回退到已发货')
    if (order.ship_status === 'pending' || order.ship_status === 'shipped') actions.push('取消这一单')
    actions.push('删除这一单')
    const { tapIndex } = await Taro.showActionSheet({ itemList: actions }).catch(() => ({ tapIndex: -1 }))
    if (tapIndex < 0) return
    const picked = actions[tapIndex]
    if (guardDemo(toast)) return

    if (picked === '回退到待发货' || picked === '回退到已发货') {
      const to = picked === '回退到待发货' ? 'pending' : 'shipped'
      const { confirm } = await Taro.showModal({
        title: '回退状态？',
        content: to === 'pending' ? '运单号会被清掉' : '退回到已发货'
      })
      if (!confirm) return
      // 后端要求带上回退目标和原因
      await api.revertShip(order.id, to, '点错了')
      await afterChange('回退了')
      return
    }

    if (picked === '取消这一单') {
      const { confirm, content } = await Taro.showModal({
        title: '取消这一单？',
        editable: true,
        placeholderText: '写个原因，比如：客户改期'
      })
      if (!confirm) return
      await api.cancelOrder(order.id, String(content || '').trim() || '卖家取消')
      await afterChange('取消了')
      return
    }

    const { confirm } = await Taro.showModal({
      title: '删除这一单？',
      content: '删掉就找不回来了',
      confirmColor: '#D2542A'
    })
    if (!confirm) return
    await api.removeOrder(order.id)
    Taro.showToast({ title: '删掉了', icon: 'success' })
    setTimeout(() => Taro.navigateBack(), 500)
  }

  return (
    <View className='page page--with-bar'>
      <DemoBanner />

      <View className='section card detail__head'>
        <View className='row'>
          <Text className='detail__no num'>{order.order_no}</Text>
          <View className='detail__head-actions'>
            <Text
              className='detail__link'
              onClick={() => Taro.navigateTo({ url: `/pages/edit/index?id=${order.id}` })}
            >
              编辑
            </Text>
            <Text className='detail__link' onClick={more}>···</Text>
          </View>
        </View>
        <View className='detail__tags'>
          <StatusTag kind='ship' value={order.ship_status} text={order.ship_status_text} flash={flash} />
          <StatusTag
            kind='pay'
            value={order.pay_status}
            text={order.pay_status_text}
            flash={flash}
            muted={order.ship_status === 'cancelled'}
          />
          {overdue > 0 ? <Text className='detail__overdue'>超期{overdue}天</Text> : null}
        </View>
        {/* 买家自己填的单：运费还没加、价格也没跟人确认过，编辑一遍再发 */}
        {order.source === 'web' ? (
          <Text className='detail__src'>买家自己登记的，运费和金额记得确认</Text>
        ) : null}
      </View>

      <View className='section card'>
        <View className='detail__line'>
          <Text className='detail__name'>{order.receiver_name}</Text>
          <Text className='detail__phone num'>{maskPhone(order.phone)}</Text>
          <Text
            className='detail__link'
            onClick={() => Taro.makePhoneCall({ phoneNumber: order.phone })}
          >
            拨打
          </Text>
        </View>
        <View className='detail__line detail__line--top'>
          <Text className='detail__address'>{order.address}</Text>
          <Text className='detail__link' onClick={() => copy(
            `${order.receiver_name} ${order.phone} ${order.address}`, '地址复制好了'
          )}>复制</Text>
        </View>
        {order.wechat_remark ? (
          <View className='detail__line'>
            <Text className='sub'>微信备注：{order.wechat_remark}</Text>
          </View>
        ) : null}
      </View>

      <View className='section card detail__items'>
        {(order.items || []).map((it, i) => (
          <View className='detail__item' key={`${it.spec_label}-${i}`}>
            <Text className='detail__item-name'>{itemName(it)}</Text>
            <Text className='detail__item-qty num'>×{it.quantity}</Text>
            <Text className='detail__item-price num'>{fenToYuan(it.unit_price)}</Text>
            <Text className='detail__item-amount num'>{fenToYuan(it.amount)}</Text>
          </View>
        ))}

        <View className='divider' />

        <View className='detail__sum'>
          {order.freight_fee ? <SumLine label='运费' value={`+${fenToYuan(order.freight_fee)}`} /> : null}
          {order.discount ? <SumLine label='优惠' value={`−${fenToYuan(order.discount)}`} /> : null}
          <View className='detail__total'>
            <Text className='group-title'>应收</Text>
            <Text className='money-lg'>{fenToYuan(order.payable_amount)}</Text>
          </View>
          <View className='detail__paid'>
            <Text className='sub'>已收 {fenToYuan(order.paid_amount)}</Text>
            {order.unpaid_amount > 0 ? (
              <Text className='detail__rest num'>还差 {fenToYuan(order.unpaid_amount)}</Text>
            ) : order.unpaid_amount < 0 ? (
              // 后端允许超付，多出来的部分要说清楚，不能显示成「还差 -50」
              <Text className='detail__over num'>多收 {fenToYuan(-order.unpaid_amount)}</Text>
            ) : (
              <Text className='sub'>已收清</Text>
            )}
          </View>
        </View>
      </View>

      {order.ship_status === 'shipped' || order.ship_status === 'received' ? (
        <View className='section card detail__ship'>
          <Text className='group-title'>物流</Text>
          <View className='detail__line'>
            <Text>{order.ship_company}</Text>
            <Text className='detail__tracking num'>{order.tracking_no}</Text>
            <Text className='detail__link' onClick={() => copy(order.tracking_no, '运单号复制好了')}>复制</Text>
          </View>
          {order.ship_time ? <Text className='sub'>{formatDate(order.ship_time, 'M月D日 HH:mm')} 发出</Text> : null}
        </View>
      ) : null}

      {(order.payments || []).length ? (
        <View className='section card'>
          <Text className='group-title detail__block-title'>收款记录</Text>
          {order.payments.map((p) => (
            <View className='detail__pay' key={p.id}>
              <Text className='sub num'>{formatDate(p.paid_at, 'M/D HH:mm')}</Text>
              <Text className='detail__pay-amount num'>{fenToYuan(p.amount)}</Text>
              <Text className='sub'>{p.pay_method_text || p.pay_method}</Text>
              <Text className='sub'>{p.remark}</Text>
            </View>
          ))}
        </View>
      ) : null}

      {order.remark ? (
        <View className='section card detail__remark'>
          <Text className='sub detail__remark-label'>备注</Text>
          <Text className='detail__remark-text'>{order.remark}</Text>
        </View>
      ) : null}

      <View className='section card'>
        <View className='row detail__logs-head' onClick={() => setLogsOpen((v) => !v)}>
          <Text className='group-title'>操作记录</Text>
          <Text className='detail__link'>{logsOpen ? '收起' : '展开'}</Text>
        </View>
        {logsOpen ? (
          <View className='detail__logs'>
            {(order.logs || []).map((log, i) => (
              <View className='detail__log' key={i}>
                <Text className='sub num'>{formatDate(log.created_at, 'M/D HH:mm')}</Text>
                <Text className='detail__log-text'>{log.action}{log.detail ? ` · ${log.detail}` : ''}</Text>
              </View>
            ))}
            {(order.logs || []).length === 0 ? <Text className='sub'>还没有记录。</Text> : null}
          </View>
        ) : null}
      </View>

      <View className='bar-bottom'>
        <View className='btn btn--ghost btn--sm' onClick={copyTrackText}>发链接给买家</View>
        <View className='btn btn--ghost btn--sm' onClick={() => setPayOpen(true)}>记一笔收款</View>
        {order.ship_status === 'pending' ? (
          <View className='btn btn--primary btn--sm' onClick={() => setShipOpen(true)}>发货</View>
        ) : null}
        {order.ship_status === 'shipped' ? (
          <View className='btn btn--primary btn--sm' onClick={doReceive}>确认收货</View>
        ) : null}
      </View>

      <ShipSheet
        visible={shipOpen}
        order={order}
        submitting={submitting}
        onClose={() => setShipOpen(false)}
        onSubmit={doShip}
      />
      <PaymentSheet
        visible={payOpen}
        order={order}
        submitting={submitting}
        onClose={() => setPayOpen(false)}
        onSubmit={doPayment}
      />
    </View>
  )
}

function SumLine({ label, value }) {
  return (
    <View className='detail__sum-line'>
      <Text className='sub'>{label}</Text>
      <Text className='num sub'>{value}</Text>
    </View>
  )
}
