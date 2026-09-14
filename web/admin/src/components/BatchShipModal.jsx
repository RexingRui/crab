import { useEffect, useState } from 'react'
import { Alert, Input, Modal, Select, Table, Typography, App } from 'antd'
import { api } from '../api/client'

const COMPANIES = ['顺丰速运', '京东物流', '中通快递', '圆通速递', '韵达快递', '德邦快递']

/* 旺季从快递公司拿回一批单号，一次贴完一次提交。
 * 逐单处理，单条失败不影响其余。 */
export default function BatchShipModal({ open, rows, onClose, onDone }) {
  const { message } = App.useApp()
  const [company, setCompany] = useState(COMPANIES[0])
  const [numbers, setNumbers] = useState({})
  const [failed, setFailed] = useState([])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    setCompany(COMPANIES[0])
    setNumbers({})
    setFailed([])
  }, [open])

  const filled = rows.filter((r) => (numbers[r.id] || '').trim())

  async function submit() {
    if (!filled.length) { message.warning('至少填一个运单号'); return }
    setBusy(true)
    try {
      const res = await api.batchShip({
        ship_company: company,
        items: filled.map((r) => ({ order_id: r.id, tracking_no: numbers[r.id].trim() }))
      })
      const bad = (res && res.failed) || []
      setFailed(bad)
      if (!bad.length) {
        message.success(`发出去 ${(res && res.success) || filled.length} 单`)
        onDone()
      } else {
        message.warning(`成功 ${(res && res.success) || 0} 单，有 ${bad.length} 单没成`)
      }
    } catch (e) {
      /* 已统一提示 */
    } finally {
      setBusy(false)
    }
  }

  /* 支持从表格/记事本一次粘贴多行单号，按顺序铺下去 */
  function pasteMany(text, startIndex) {
    const lines = String(text).split(/[\r\n\t]+/).map((s) => s.trim()).filter(Boolean)
    if (lines.length < 2) return false
    const next = { ...numbers }
    lines.forEach((line, i) => {
      const row = rows[startIndex + i]
      if (row) next[row.id] = line
    })
    setNumbers(next)
    return true
  }

  const columns = [
    { title: '收货人', dataIndex: 'receiver_name', width: 90 },
    { title: '单号', dataIndex: 'order_no', width: 140, render: (v) => <span className='num'>{v}</span> },
    { title: '明细', dataIndex: 'items_summary', ellipsis: true },
    {
      title: '运单号',
      width: 260,
      render: (_, row, index) => (
        <Input
          className='num'
          placeholder='粘贴运单号'
          value={numbers[row.id] || ''}
          status={failed.some((f) => f.order_id === row.id) ? 'error' : ''}
          onChange={(e) => setNumbers((n) => ({ ...n, [row.id]: e.target.value }))}
          onPaste={(e) => {
            const text = e.clipboardData.getData('text')
            if (pasteMany(text, index)) e.preventDefault()
          }}
        />
      )
    }
  ]

  return (
    <Modal
      open={open}
      title='批量发货'
      width={880}
      onCancel={onClose}
      onOk={submit}
      okText={`发货（${filled.length}）`}
      confirmLoading={busy}
    >
      <Typography.Paragraph className='hint'>
        先选快递公司，再逐行贴运单号。从表格里复制一列可以一次铺满。
      </Typography.Paragraph>

      <Select
        style={{ width: 200, marginBottom: 16 }}
        value={company}
        onChange={setCompany}
        options={COMPANIES.map((c) => ({ value: c, label: c }))}
      />

      {failed.length ? (
        <Alert
          type='warning'
          showIcon
          style={{ marginBottom: 16 }}
          message='这几单没发出去'
          description={
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {failed.map((f) => {
                const row = rows.find((r) => r.id === f.order_id)
                return <li key={f.order_id}>{(row && row.receiver_name) || f.order_id}：{f.reason}</li>
              })}
            </ul>
          }
        />
      ) : null}

      <Table
        rowKey='id'
        size='small'
        columns={columns}
        dataSource={rows}
        pagination={false}
        scroll={{ y: 360 }}
      />
    </Modal>
  )
}
