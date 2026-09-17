import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Taro, { useDidShow, usePullDownRefresh, useReachBottom } from '@tarojs/taro'
import { View, Text, Input } from '@tarojs/components'
import DemoBanner from '../../components/DemoBanner'
import OrderCard from '../../components/OrderCard'
import Empty from '../../components/Empty'
import Sheet from '../../components/Sheet'
import api from '../../utils/api'
import { friendlyDay, formatDate } from '../../utils/format'
import { whenReady } from '../../utils/session'
import './index.scss'

const PAGE_SIZE = 20

// 快捷筛选 chip，可多选（发货状态与收款状态各取一个）
const CHIPS = [
  { key: 'pending', field: 'ship_status', value: 'pending', label: '待发货' },
  { key: 'unpaid', field: 'pay_status', value: 'unpaid', label: '未收款' },
  { key: 'shipped', field: 'ship_status', value: 'shipped', label: '已发货' }
]

const SHIP_OPTIONS = [
  ['', '不限'], ['pending', '待发货'], ['shipped', '已发货'], ['received', '已收货'], ['cancelled', '已取消']
]
const PAY_OPTIONS = [
  ['', '不限'], ['unpaid', '未收款'], ['partial', '收了定金'], ['paid', '已收清']
]
const SOURCE_OPTIONS = [
  ['', '不限'], ['web', '买家登记'], ['manual', '自己录的']
]

const EMPTY_FILTERS = { ship_status: '', pay_status: '', source: '' }

export default function Orders() {
  const [keyword, setKeyword] = useState('')
  const [filters, setFilters] = useState(EMPTY_FILTERS)
  const [list, setList] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [panelOpen, setPanelOpen] = useState(false)
  const [draft, setDraft] = useState(EMPTY_FILTERS)

  const debounceTimer = useRef(null)
  const requestSeq = useRef(0)
  const firstShow = useRef(true)

  // 首页的汇总行点进来时带着筛选条件
  useEffect(() => {
    const apply = (params = {}) => {
      setKeyword('')
      setFilters({
        ship_status: params.ship_status || '',
        pay_status: params.pay_status || '',
        source: params.source || ''
      })
    }
    Taro.eventCenter.on('orders:filter', apply)
    return () => Taro.eventCenter.off('orders:filter', apply)
  }, [])

  const query = useMemo(
    () => ({ keyword: keyword.trim(), ...filters }),
    [keyword, filters]
  )

  const load = useCallback(async (targetPage) => {
    const seq = ++requestSeq.current
    setLoading(true)
    try {
      await whenReady()
      const res = await api.orders({ ...query, page: targetPage, page_size: PAGE_SIZE })
      if (seq !== requestSeq.current) return // 慢响应盖掉新结果
      const rows = (res && res.list) || []
      setTotal((res && res.total) || 0)
      setPage(targetPage)
      setList((prev) => (targetPage === 1 ? rows : prev.concat(rows)))
    } catch (e) {
      /* 提示已在 request.js 统一处理 */
    } finally {
      if (seq === requestSeq.current) setLoading(false)
    }
  }, [query])

  // 搜索与筛选变化：防抖 400ms 后回到第一页
  useEffect(() => {
    if (debounceTimer.current) clearTimeout(debounceTimer.current)
    debounceTimer.current = setTimeout(() => load(1), 400)
    return () => debounceTimer.current && clearTimeout(debounceTimer.current)
  }, [load])

  useDidShow(() => {
    if (firstShow.current) { firstShow.current = false; return }
    load(1) // 从详情页改完状态回来要刷新
  })

  usePullDownRefresh(() => {
    load(1).then(() => Taro.stopPullDownRefresh())
  })

  useReachBottom(() => {
    if (!loading && list.length < total) load(page + 1)
  })

  function toggleChip(chip) {
    setFilters((prev) => ({
      ...prev,
      [chip.field]: prev[chip.field] === chip.value ? '' : chip.value
    }))
  }

  const isAll = !filters.ship_status && !filters.pay_status && !filters.source

  // 按创建日期分组，组标题吸顶
  const groups = useMemo(() => {
    const out = []
    list.forEach((order) => {
      const day = formatDate(order.created_at, 'YYYY-MM-DD')
      const last = out[out.length - 1]
      if (last && last.day === day) last.orders.push(order)
      else out.push({ day, label: friendlyDay(order.created_at) || day, orders: [order] })
    })
    return out
  }, [list])

  function openDetail(order) {
    Taro.navigateTo({ url: `/pages/detail/index?id=${order.id}` })
  }

  return (
    <View className='page'>
      <DemoBanner />

      <View className='orders__bar'>
        <Input
          className='orders__search'
          value={keyword}
          confirmType='search'
          placeholder='姓名 / 手机 / 单号 / 运单号'
          onInput={(e) => setKeyword(e.detail.value)}
        />
        <Text
          className='orders__filter'
          onClick={() => { setDraft(filters); setPanelOpen(true) }}
        >
          筛选
        </Text>
      </View>

      <View className='orders__chips'>
        {CHIPS.map((chip) => (
          <Text
            key={chip.key}
            className={`orders__chip ${filters[chip.field] === chip.value ? 'orders__chip--on' : ''}`}
            onClick={() => toggleChip(chip)}
          >
            {chip.label}
          </Text>
        ))}
        <Text
          className={`orders__chip ${isAll ? 'orders__chip--on' : ''}`}
          onClick={() => setFilters(EMPTY_FILTERS)}
        >
          全部
        </Text>
      </View>

      {groups.length === 0 && !loading ? (
        <Empty text={keyword || !isAll ? '这些条件下没有记录。' : '还什么都没记。'}>
          <View className='btn btn--primary' onClick={() => Taro.switchTab({ url: '/pages/record/index' })}>
            记一笔
          </View>
        </Empty>
      ) : null}

      {groups.map((group) => (
        <View className='orders__group' key={group.day}>
          <View className='orders__group-title'>{group.label}</View>
          <View className='orders__group-body'>
            {group.orders.map((order) => (
              <OrderCard key={order.id} order={order} onClick={openDetail} />
            ))}
          </View>
        </View>
      ))}

      {list.length > 0 ? (
        <Text className='orders__foot sub'>
          {list.length < total ? '往下拉，还有' : `一共 ${total} 笔`}
        </Text>
      ) : null}

      <Sheet
        visible={panelOpen}
        title='筛选'
        onClose={() => setPanelOpen(false)}
        footer={
          <>
            <View
              className='btn btn--ghost'
              onClick={() => { setDraft(EMPTY_FILTERS) }}
            >
              清空
            </View>
            <View
              className='btn btn--primary btn--block'
              onClick={() => { setFilters(draft); setPanelOpen(false) }}
            >
              看结果
            </View>
          </>
        }
      >
        <OptionRow
          title='发货状态'
          options={SHIP_OPTIONS}
          value={draft.ship_status}
          onPick={(v) => setDraft((d) => ({ ...d, ship_status: v }))}
        />
        <OptionRow
          title='收款状态'
          options={PAY_OPTIONS}
          value={draft.pay_status}
          onPick={(v) => setDraft((d) => ({ ...d, pay_status: v }))}
        />
        <OptionRow
          title='来源'
          options={SOURCE_OPTIONS}
          value={draft.source}
          onPick={(v) => setDraft((d) => ({ ...d, source: v }))}
        />
      </Sheet>
    </View>
  )
}

function OptionRow({ title, options, value, onPick }) {
  return (
    <View className='orders__option'>
      <Text className='sub'>{title}</Text>
      <View className='orders__option-list'>
        {options.map(([key, label]) => (
          <Text
            key={key || 'any'}
            className={`orders__chip ${value === key ? 'orders__chip--on' : ''}`}
            onClick={() => onPick(key)}
          >
            {label}
          </Text>
        ))}
      </View>
    </View>
  )
}
