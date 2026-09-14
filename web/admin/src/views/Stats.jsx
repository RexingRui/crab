import { useEffect, useMemo, useRef, useState } from 'react'
import { Card, Col, DatePicker, Row, Segmented, Statistic, Table, Typography } from 'antd'
import dayjs from 'dayjs'
import echarts from '../utils/echarts'
import { api } from '../api/client'
import { fenToYuan } from '../utils/format'
import { AXIS, GRID_LINE, INK_MUTED, SERIES, TOOLTIP } from '../utils/charts'

/* 默认看本季——大闸蟹的生意就是按季过的 */
function thisQuarter() {
  const now = dayjs()
  return [now.startOf('quarter'), now.endOf('quarter')]
}

export default function Stats() {
  const [range, setRange] = useState(thisQuarter)
  const [data, setData] = useState(null)
  const [view, setView] = useState('图')

  useEffect(() => {
    api.stats({
      from: range[0].format('YYYY-MM-DD'),
      to: range[1].format('YYYY-MM-DD')
    }).then(setData).catch(() => setData(null))
  }, [range])

  const specRows = (data && data.by_spec) || []
  const dayRows = (data && data.by_day) || []

  return (
    <>
      <div className='page-head'>
        <h1 className='page-title'>统计</h1>
        <div style={{ display: 'flex', gap: 12 }}>
          <Segmented options={['图', '数字']} value={view} onChange={setView} />
          <DatePicker.RangePicker
            value={range}
            allowClear={false}
            onChange={(v) => v && setRange(v)}
            presets={[
              { label: '本季', value: thisQuarter() },
              { label: '近 30 天', value: [dayjs().subtract(29, 'day'), dayjs()] },
              { label: '今年', value: [dayjs().startOf('year'), dayjs().endOf('year')] }
            ]}
          />
        </div>
      </div>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Tile label='订单数' value={(data && data.order_count) || 0} />
        <Tile label='应收合计' value={fenToYuan((data && data.total_amount) || 0)} />
        <Tile label='已收合计' value={fenToYuan((data && data.paid_amount) || 0)} />
        <Tile
          label='未收合计'
          value={fenToYuan((data && data.unpaid_amount) || 0)}
          due={(data && data.unpaid_amount) > 0}
        />
        <Tile label='蟹只数' value={(data && data.crab_count) || 0} />
      </Row>

      {view === '图' ? (
        <Row gutter={16}>
          <Col span={12}>
            <Card title='各规格销量' variant='borderless'>
              <SpecBar rows={specRows} />
            </Card>
          </Col>
          <Col span={12}>
            <Card title='按日销售额' variant='borderless'>
              <DayLine rows={dayRows} />
            </Card>
          </Col>
        </Row>
      ) : (
        <Row gutter={16}>
          <Col span={12}>
            <Card title='各规格销量' variant='borderless'>
              <Table
                rowKey={(r) => `${r.gender}-${r.size}`}
                size='small'
                pagination={false}
                dataSource={specRows}
                columns={[
                  { title: '规格', render: (_, r) => `${r.gender === 'male' ? '公' : '母'} ${r.size}` },
                  { title: '只数', dataIndex: 'quantity', align: 'right', render: (v) => <span className='money-cell'>{v}</span> },
                  { title: '金额', dataIndex: 'amount', align: 'right', render: (v) => <span className='money-cell'>{fenToYuan(v)}</span> }
                ]}
              />
            </Card>
          </Col>
          <Col span={12}>
            <Card title='按日销售额' variant='borderless'>
              <Table
                rowKey='date'
                size='small'
                pagination={{ pageSize: 10 }}
                dataSource={dayRows}
                columns={[
                  { title: '日期', dataIndex: 'date', render: (v) => <span className='num'>{v}</span> },
                  { title: '金额', dataIndex: 'amount', align: 'right', render: (v) => <span className='money-cell'>{fenToYuan(v)}</span> }
                ]}
              />
            </Card>
          </Col>
        </Row>
      )}

      <Typography.Paragraph className='hint' style={{ marginTop: 12 }}>
        统计只算没取消的单。切到「数字」可以看同一份数据的表格。
      </Typography.Paragraph>
    </>
  )
}

function Tile({ label, value, due }) {
  return (
    <Col flex='1'>
      <Card variant='borderless'>
        <Statistic
          title={label}
          value={value}
          valueStyle={{
            fontVariantNumeric: 'tabular-nums',
            letterSpacing: '.02em',
            fontSize: 22,
            color: due ? 'var(--boiled)' : 'var(--shell-deep)'
          }}
        />
      </Card>
    </Col>
  )
}

/** ECharts 实例挂在 ref 上，尺寸变化时跟着 resize */
function useChart(option, deps) {
  const box = useRef(null)
  const chart = useRef(null)

  useEffect(() => {
    if (!box.current) return undefined
    chart.current = echarts.init(box.current)
    const onResize = () => chart.current && chart.current.resize()
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      chart.current && chart.current.dispose()
      chart.current = null
    }
  }, [])

  useEffect(() => {
    if (chart.current) chart.current.setOption(option, true)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return box
}

/* 两个系列（公/母）→ 图例必须在，颜色按固定顺序分配 */
function SpecBar({ rows }) {
  const { sizes, male, female } = useMemo(() => {
    const order = []
    rows.forEach((r) => { if (!order.includes(r.size)) order.push(r.size) })
    order.sort((a, b) => parseFloat(a) - parseFloat(b))
    const pick = (gender) => order.map((size) => {
      const hit = rows.find((r) => r.size === size && r.gender === gender)
      return hit ? hit.quantity : 0
    })
    return { sizes: order, male: pick('male'), female: pick('female') }
  }, [rows])

  const box = useChart({
    tooltip: { ...TOOLTIP, axisPointer: { type: 'shadow' } },
    legend: { data: ['公', '母'], right: 0, top: 0, textStyle: { color: INK_MUTED } },
    grid: { left: 8, right: 8, top: 40, bottom: 8, containLabel: true },
    xAxis: { type: 'category', data: sizes, ...AXIS, splitLine: { show: false } },
    yAxis: { type: 'value', name: '只', nameTextStyle: { color: INK_MUTED }, ...AXIS },
    series: [
      {
        name: '公',
        type: 'bar',
        data: male,
        itemStyle: { color: SERIES.male, borderRadius: [4, 4, 0, 0] },
        barMaxWidth: 28,
        // 相邻柱之间留 2px 的底色缝
        barGap: '2%'
      },
      {
        name: '母',
        type: 'bar',
        data: female,
        itemStyle: { color: SERIES.female, borderRadius: [4, 4, 0, 0] },
        barMaxWidth: 28
      }
    ]
  }, [sizes, male, female])

  return <div ref={box} style={{ height: 320 }} />
}

/* 单系列不需要图例，标题已经说了这是什么 */
function DayLine({ rows }) {
  const box = useChart({
    tooltip: {
      ...TOOLTIP,
      axisPointer: { type: 'line', lineStyle: { color: GRID_LINE } },
      valueFormatter: (v) => fenToYuan(v)
    },
    // 右边留够，不然最后一个日期标签会被裁掉
    grid: { left: 8, right: 32, top: 24, bottom: 8, containLabel: true },
    xAxis: {
      type: 'category',
      data: rows.map((r) => r.date),
      boundaryGap: false,
      ...AXIS,
      axisLabel: { ...AXIS.axisLabel, hideOverlap: true },
      splitLine: { show: false }
    },
    yAxis: {
      type: 'value',
      ...AXIS,
      axisLabel: { ...AXIS.axisLabel, formatter: (v) => fenToYuan(v, { symbol: false }) }
    },
    series: [{
      type: 'line',
      name: '销售额',
      data: rows.map((r) => r.amount),
      smooth: false,
      showSymbol: rows.length <= 40,
      symbolSize: 8,
      lineStyle: { width: 2, color: SERIES.male },
      itemStyle: { color: SERIES.male, borderColor: '#fff', borderWidth: 2 },
      areaStyle: {
        color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
          { offset: 0, color: 'rgba(46,125,82,.16)' },
          { offset: 1, color: 'rgba(46,125,82,0)' }
        ])
      }
    }]
  }, [rows])

  return <div ref={box} style={{ height: 320 }} />
}
