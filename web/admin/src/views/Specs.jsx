import { useEffect, useState } from 'react'
import {
  Button, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Typography, App
} from 'antd'
import { api } from '../api/client'
import { fenToYuan, yuanToFen } from '../utils/format'

export default function Specs() {
  const { message, modal } = App.useApp()
  const [rows, setRows] = useState([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form] = Form.useForm()

  async function load() {
    setLoading(true)
    try {
      const data = await api.specs()
      setRows((data && data.list) || [])
    } catch (e) {
      /* 已统一提示 */
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  function open(row) {
    setEditing(row || {})
    form.setFieldsValue(row ? {
      ...row,
      unit_price: row.unit_price / 100
    } : { gender: 'male', unit: '只', enabled: true, sort: rows.length + 1 })
  }

  async function save() {
    const v = await form.validateFields()
    const body = {
      gender: v.gender,
      size: v.size.trim(),
      unit: v.unit || '只',
      unit_price: yuanToFen(v.unit_price),
      enabled: v.enabled !== false,
      sort: Number(v.sort) || 0
    }
    try {
      if (editing && editing.id) await api.updateSpec(editing.id, body)
      else await api.createSpec(body)
      message.success('存好了')
      setEditing(null)
      load()
    } catch (e) {
      /* 已统一提示 */
    }
  }

  function remove(row) {
    modal.confirm({
      title: '删掉这一档？',
      content: '已经记过的单不受影响',
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.removeSpec(row.id)
        load()
      }
    })
  }

  const columns = [
    {
      title: '公母',
      dataIndex: 'gender',
      width: 80,
      render: (v, r) => r.gender_text || (v === 'male' ? '公' : '母')
    },
    { title: '规格', dataIndex: 'size', width: 120 },
    { title: '单位', dataIndex: 'unit', width: 80 },
    {
      title: '单价',
      dataIndex: 'unit_price',
      width: 120,
      align: 'right',
      render: (v) => <span className='money-cell'>{fenToYuan(v)}</span>
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 100,
      render: (v, r) => (
        <Switch
          size='small'
          checked={v !== false}
          onChange={async (checked) => {
            await api.updateSpec(r.id, { ...r, enabled: checked })
            load()
          }}
        />
      )
    },
    { title: '排序', dataIndex: 'sort', width: 80, render: (v) => <span className='num'>{v}</span> },
    {
      title: '',
      width: 120,
      render: (_, row) => (
        <Space>
          <Button type='link' size='small' onClick={() => open(row)}>改</Button>
          <Button type='link' size='small' danger onClick={() => remove(row)}>删</Button>
        </Space>
      )
    }
  ]

  return (
    <>
      <div className='page-head'>
        <h1 className='page-title'>价目表</h1>
        <Button type='primary' onClick={() => open(null)}>加一档</Button>
      </div>

      <Typography.Paragraph className='hint'>
        改价即时生效于新订单，<b>不影响已经记过的单</b>。单价在记单时还能单独改，熟客让价不用动这里。
      </Typography.Paragraph>

      <div className='table-card'>
        <Table
          rowKey='id'
          size='middle'
          loading={loading}
          columns={columns}
          dataSource={rows}
          pagination={false}
        />
      </div>

      <Modal
        open={Boolean(editing)}
        title={editing && editing.id ? '改一档' : '加一档'}
        onCancel={() => setEditing(null)}
        onOk={save}
        okText='存下来'
      >
        <Form form={form} layout='vertical'>
          <Form.Item label='公母' name='gender' rules={[{ required: true }]}>
            <Select options={[{ value: 'male', label: '公' }, { value: 'female', label: '母' }]} />
          </Form.Item>
          <Form.Item label='规格' name='size' rules={[{ required: true, message: '比如 4.5两' }]}>
            <Input placeholder='4.5两' />
          </Form.Item>
          <Form.Item label='单位' name='unit'><Input placeholder='只' /></Form.Item>
          <Form.Item label='单价（元）' name='unit_price' rules={[{ required: true, message: '填一下单价' }]}>
            <InputNumber className='num' style={{ width: '100%' }} min={0} precision={2} />
          </Form.Item>
          <Form.Item label='排序' name='sort'>
            <InputNumber className='num' style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label='启用' name='enabled' valuePropName='checked'>
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
