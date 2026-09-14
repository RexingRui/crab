import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Button, DatePicker, Input, Select, Space, Table, Typography, App
} from 'antd'
import dayjs from 'dayjs'
import { api, downloadExport } from '../api/client'
import { fenToYuan, overdueDays } from '../utils/format'
import StatusTag from '../components/StatusTag'
import OrderDrawer from '../components/OrderDrawer'
import BatchShipModal from '../components/BatchShipModal'

/* 小程序管单笔，后台管批量：这一页的重点是筛选、对账和批量发货 */

const SHIP_OPTIONS = [
  { value: 'pending', label: '待发货' },
  { value: 'shipped', label: '已发货' },
  { value: 'received', label: '已收货' },
  { value: 'cancelled', label: '已取消' }
]
const PAY_OPTIONS = [
  { value: 'unpaid', label: '未收款' },
  { value: 'partial', label: '收了定金' },
  { value: 'paid', label: '已收清' }
]

const EMPTY = { keyword: '', ship_status: undefined, pay_status: undefined, created: null, plan_ship_date: null }

export default function Orders() {
  const { message } = App.useApp()
  const [form, setForm] = useState(EMPTY)
  const [rows, setRows] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState([])
  const [current, setCurrent] = useState(null)
  const [batchOpen, setBatchOpen] = useState(false)

  const query = useMemo(() => ({
    keyword: form.keyword.trim(),
    ship_status: form.ship_status,
    pay_status: form.pay_status,
    created_from: form.created ? form.created[0].format('YYYY-MM-DD') : '',
    created_to: form.created ? form.created[1].format('YYYY-MM-DD') : '',
    plan_ship_date: form.plan_ship_date ? form.plan_ship_date.format('YYYY-MM-DD') : ''
  }), [form])

  const load = useCallback(async (p = page, size = pageSize) => {
    setLoading(true)
    try {
      const data = await api.orders({ ...query, page: p, page_size: size })
      setRows((data && data.list) || [])
      setTotal((data && data.total) || 0)
      setPage(p)
      setPageSize(size)
    } catch (e) {
      /* 已统一提示 */
    } finally {
      setLoading(false)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])

  useEffect(() => { load(1, pageSize) // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])

  const selectedRows = rows.filter((r) => selected.includes(r.id))

  const columns = [
    { title: '单号', dataIndex: 'order_no', width: 140, render: (v) => <span className='num'>{v}</span> },
    {
      title: '创建',
      dataIndex: 'created_at',
      width: 110,
      render: (v) => <span className='num muted'>{v ? dayjs(v).format('MM-DD HH:mm') : ''}</span>
    },
    { title: '收货人', dataIndex: 'receiver_name', width: 90 },
    { title: '手机', dataIndex: 'receiver_phone', width: 120, render: (v) => <span className='num'>{v}</span> },
    { title: '明细', dataIndex: 'items_summary', ellipsis: true },
    {
      title: '应收',
      dataIndex: 'total_amount',
      width: 100,
      align: 'right',
      render: (v) => <span className='money-cell'>{fenToYuan(v)}</span>
    },
    {
      title: '已收',
      dataIndex: 'paid_amount',
      width: 100,
      align: 'right',
      render: (v) => <span className='money-cell muted'>{fenToYuan(v)}</span>
    },
    {
      title: '未收',
      dataIndex: 'unpaid_amount',
      width: 100,
      align: 'right',
      // 取消掉的单没有钱要追，别用告警色喊人
      render: (v, row) => (
        <span className={`money-cell ${v > 0 && row.ship_status !== 'cancelled' ? 'money-cell--due' : 'muted'}`}>
          {fenToYuan(v)}
        </span>
      )
    },
    {
      title: '发货',
      dataIndex: 'ship_status',
      width: 110,
      render: (v, row) => (
        <>
          <StatusTag kind='ship' value={v} text={row.ship_status_text} />
          {overdueDays(row.plan_ship_date, v) > 0 ? (
            <div style={{ color: 'var(--boiled)', fontSize: 12 }}>
              超期{overdueDays(row.plan_ship_date, v)}天
            </div>
          ) : null}
        </>
      )
    },
    {
      title: '收款',
      dataIndex: 'pay_status',
      width: 100,
      render: (v, row) => (
        <StatusTag kind='pay' value={v} text={row.pay_status_text} muted={row.ship_status === 'cancelled'} />
      )
    },
    {
      title: '运单号',
      dataIndex: 'tracking_no',
      width: 160,
      render: (v, row) => v ? <span className='num'>{row.ship_company} {v}</span> : <span className='muted'>—</span>
    },
    {
      title: '约定发货',
      dataIndex: 'plan_ship_date',
      width: 110,
      render: (v) => <span className='num muted'>{v || ''}</span>
    }
  ]

  async function exportCsv() {
    try {
      await downloadExport(query)
    } catch (e) {
      message.error('导出失败')
    }
  }

  return (
    <>
      <div className='page-head'>
        <h1 className='page-title'>订单</h1>
        <Space>
          <Button
            type='primary'
            disabled={!selectedRows.length}
            onClick={() => setBatchOpen(true)}
          >
            批量发货{selectedRows.length ? `（${selectedRows.length}）` : ''}
          </Button>
          <Button onClick={exportCsv}>导出 CSV</Button>
        </Space>
      </div>

      <div className='filter-bar'>
        <Space wrap size='middle'>
          <Input
            allowClear
            style={{ width: 240 }}
            placeholder='姓名 / 手机 / 单号 / 运单号'
            value={form.keyword}
            onChange={(e) => setForm((f) => ({ ...f, keyword: e.target.value }))}
          />
          <Select
            allowClear
            style={{ width: 130 }}
            placeholder='发货状态'
            options={SHIP_OPTIONS}
            value={form.ship_status}
            onChange={(v) => setForm((f) => ({ ...f, ship_status: v }))}
          />
          <Select
            allowClear
            style={{ width: 130 }}
            placeholder='收款状态'
            options={PAY_OPTIONS}
            value={form.pay_status}
            onChange={(v) => setForm((f) => ({ ...f, pay_status: v }))}
          />
          <DatePicker.RangePicker
            placeholder={['创建起', '创建止']}
            value={form.created}
            onChange={(v) => setForm((f) => ({ ...f, created: v }))}
          />
          <DatePicker
            placeholder='约定发货日'
            value={form.plan_ship_date}
            onChange={(v) => setForm((f) => ({ ...f, plan_ship_date: v }))}
          />
          <Button onClick={() => setForm(EMPTY)}>清空</Button>
        </Space>
      </div>

      <div className='table-card'>
        <Table
          rowKey='id'
          size='middle'
          loading={loading}
          columns={columns}
          dataSource={rows}
          scroll={{ x: 1400 }}
          rowSelection={{
            selectedRowKeys: selected,
            onChange: setSelected,
            getCheckboxProps: (row) => ({ disabled: row.ship_status !== 'pending' })
          }}
          onRow={(row) => ({
            onClick: () => setCurrent(row),
            style: { cursor: 'pointer', opacity: row.ship_status === 'cancelled' ? 0.55 : 1 }
          })}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            showTotal: (t) => `一共 ${t} 笔`,
            onChange: (p, size) => load(p, size)
          }}
        />
      </div>

      <Typography.Paragraph className='hint' style={{ marginTop: 12 }}>
        只有待发货的单能勾选批量发货。行点开是详情，可以改单、记收款、发货。
      </Typography.Paragraph>

      <OrderDrawer
        orderId={current && current.id}
        onClose={() => setCurrent(null)}
        onChanged={() => load(page, pageSize)}
      />

      <BatchShipModal
        open={batchOpen}
        rows={selectedRows}
        onClose={() => setBatchOpen(false)}
        onDone={() => {
          setBatchOpen(false)
          setSelected([])
          load(page, pageSize)
        }}
      />
    </>
  )
}
