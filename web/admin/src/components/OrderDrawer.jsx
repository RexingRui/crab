import { useCallback, useEffect, useState } from 'react'
import {
  Button, Descriptions, Drawer, Form, Input, InputNumber, Select, Space,
  Table, Tabs, Typography, App
} from 'antd'
import dayjs from 'dayjs'
import { api } from '../api/client'
import { fenToYuan, yuanToFen } from '../utils/format'
import StatusTag from '../components/StatusTag'

const COMPANIES = ['顺丰速运', '京东物流', '中通快递', '圆通速递', '韵达快递', '德邦快递']
const METHODS = [
  { value: 'wechat', label: '微信' },
  { value: 'alipay', label: '支付宝' },
  { value: 'cash', label: '现金' },
  { value: 'transfer', label: '转账' },
  { value: 'other', label: '其他' }
]  // 后台不受小程序审核约束，方式可以写全

export default function OrderDrawer({ orderId, onClose, onChanged }) {
  const { message, modal } = App.useApp()
  const [order, setOrder] = useState(null)
  const [loading, setLoading] = useState(false)
  const [shipForm] = Form.useForm()
  const [payForm] = Form.useForm()
  const [editForm] = Form.useForm()

  const load = useCallback(async () => {
    if (!orderId) { setOrder(null); return }
    setLoading(true)
    try {
      const data = await api.order(orderId)
      setOrder(data)
      shipForm.setFieldsValue({
        ship_company: data.ship_company || COMPANIES[0],
        tracking_no: data.tracking_no || ''
      })
      payForm.setFieldsValue({
        amount: data.unpaid_amount > 0 ? data.unpaid_amount / 100 : undefined,
        method: 'wechat',
        remark: ''
      })
      editForm.setFieldsValue({
        receiver_name: data.receiver_name,
        receiver_phone: data.receiver_phone,
        receiver_address: data.receiver_address,
        wechat_note: data.wechat_note,
        plan_ship_date: data.plan_ship_date,
        remark: data.remark
      })
    } catch (e) {
      /* 已统一提示 */
    } finally {
      setLoading(false)
    }
  }, [orderId, shipForm, payForm, editForm])

  useEffect(() => { load() }, [load])

  async function run(fn, okText) {
    try {
      await fn()
      message.success(okText)
      await load()
      onChanged && onChanged()
    } catch (e) {
      /* 已统一提示 */
    }
  }

  const doShip = (v) => run(() => api.ship(order.id, v), '发出去了')
  const doPay = (v) => run(
    () => api.addPayment(order.id, { ...v, amount: yuanToFen(v.amount) }),
    '记上了'
  )
  const doEdit = (v) => run(() => api.updateOrder(order.id, v), '改好了')
  const doReceive = () => modal.confirm({
    title: '确认收货？',
    content: '买家已经收到货了',
    onOk: () => run(() => api.confirmReceive(order.id), '已收货')
  })

  const itemColumns = [
    { title: '规格', render: (_, r) => `${r.gender_text || ''} ${r.size}` },
    { title: '数量', dataIndex: 'quantity', align: 'right', width: 70, render: (v) => <span className='num'>{v}</span> },
    { title: '单价', dataIndex: 'unit_price', align: 'right', width: 90, render: (v) => <span className='money-cell'>{fenToYuan(v)}</span> },
    { title: '小计', dataIndex: 'amount', align: 'right', width: 100, render: (v) => <span className='money-cell'>{fenToYuan(v)}</span> }
  ]

  return (
    <Drawer
      open={Boolean(orderId)}
      onClose={onClose}
      width={720}
      loading={loading}
      title={order ? <span className='num'>{order.order_no}</span> : '订单'}
      extra={order ? (
        <Space>
          <StatusTag kind='ship' value={order.ship_status} text={order.ship_status_text} />
          <StatusTag
            kind='pay'
            value={order.pay_status}
            text={order.pay_status_text}
            muted={order.ship_status === 'cancelled'}
          />
          {order.ship_status === 'shipped' ? <Button onClick={doReceive}>确认收货</Button> : null}
        </Space>
      ) : null}
    >
      {order ? (
        <Tabs
          items={[
            {
              key: 'info',
              label: '详情',
              children: (
                <>
                  <Descriptions column={1} size='small' bordered>
                    <Descriptions.Item label='收货人'>{order.receiver_name}</Descriptions.Item>
                    <Descriptions.Item label='手机'><span className='num'>{order.receiver_phone}</span></Descriptions.Item>
                    <Descriptions.Item label='地址'>{order.receiver_address}</Descriptions.Item>
                    <Descriptions.Item label='微信备注'>{order.wechat_note || '—'}</Descriptions.Item>
                    <Descriptions.Item label='约定发货'>
                      <span className='num'>{order.plan_ship_date || '—'}</span>
                    </Descriptions.Item>
                    <Descriptions.Item label='备注'>{order.remark || '—'}</Descriptions.Item>
                    {order.tracking_no ? (
                      <Descriptions.Item label='运单号'>
                        <span className='num'>{order.ship_company} {order.tracking_no}</span>
                      </Descriptions.Item>
                    ) : null}
                  </Descriptions>

                  <Table
                    style={{ marginTop: 16 }}
                    rowKey={(r, i) => `${r.spec_id}-${i}`}
                    size='small'
                    pagination={false}
                    columns={itemColumns}
                    dataSource={order.items || []}
                    summary={() => (
                      <Table.Summary>
                        {order.freight_fee ? (
                          <Table.Summary.Row>
                            <Table.Summary.Cell colSpan={3}>运费</Table.Summary.Cell>
                            <Table.Summary.Cell align='right'>
                              <span className='money-cell'>+{fenToYuan(order.freight_fee)}</span>
                            </Table.Summary.Cell>
                          </Table.Summary.Row>
                        ) : null}
                        {order.discount ? (
                          <Table.Summary.Row>
                            <Table.Summary.Cell colSpan={3}>优惠</Table.Summary.Cell>
                            <Table.Summary.Cell align='right'>
                              <span className='money-cell'>−{fenToYuan(order.discount)}</span>
                            </Table.Summary.Cell>
                          </Table.Summary.Row>
                        ) : null}
                        <Table.Summary.Row>
                          <Table.Summary.Cell colSpan={3}><b>应收</b></Table.Summary.Cell>
                          <Table.Summary.Cell align='right'>
                            <b className='money-cell'>{fenToYuan(order.total_amount)}</b>
                          </Table.Summary.Cell>
                        </Table.Summary.Row>
                        <Table.Summary.Row>
                          <Table.Summary.Cell colSpan={3}>已收</Table.Summary.Cell>
                          <Table.Summary.Cell align='right'>
                            <span className='money-cell muted'>{fenToYuan(order.paid_amount)}</span>
                          </Table.Summary.Cell>
                        </Table.Summary.Row>
                        <Table.Summary.Row>
                          <Table.Summary.Cell colSpan={3}>还差</Table.Summary.Cell>
                          <Table.Summary.Cell align='right'>
                            <span className={`money-cell ${order.unpaid_amount > 0 ? 'money-cell--due' : 'muted'}`}>
                              {fenToYuan(order.unpaid_amount)}
                            </span>
                          </Table.Summary.Cell>
                        </Table.Summary.Row>
                      </Table.Summary>
                    )}
                  />

                  {(order.payments || []).length ? (
                    <>
                      <Typography.Title level={5} style={{ marginTop: 24 }}>收款记录</Typography.Title>
                      <Table
                        rowKey='id'
                        size='small'
                        pagination={false}
                        dataSource={order.payments}
                        columns={[
                          { title: '时间', dataIndex: 'paid_at', width: 140, render: (v) => <span className='num'>{v ? dayjs(v).format('MM-DD HH:mm') : ''}</span> },
                          { title: '金额', dataIndex: 'amount', align: 'right', width: 100, render: (v) => <span className='money-cell'>{fenToYuan(v)}</span> },
                          { title: '方式', dataIndex: 'method_text', width: 80, render: (v, r) => v || r.method },
                          { title: '备注', dataIndex: 'remark' }
                        ]}
                      />
                    </>
                  ) : null}
                </>
              )
            },
            {
              key: 'ship',
              label: '发货',
              disabled: order.ship_status === 'cancelled',
              children: (
                <Form form={shipForm} layout='vertical' onFinish={doShip} style={{ maxWidth: 420 }}>
                  <Form.Item label='快递公司' name='ship_company' rules={[{ required: true }]}>
                    <Select options={COMPANIES.map((c) => ({ value: c, label: c }))} />
                  </Form.Item>
                  <Form.Item label='运单号' name='tracking_no' rules={[{ required: true, message: '填一下运单号' }]}>
                    <Input className='num' placeholder='SF1234567890' />
                  </Form.Item>
                  <Button type='primary' htmlType='submit'>
                    {order.ship_status === 'pending' ? '发货' : '改运单号'}
                  </Button>
                </Form>
              )
            },
            {
              key: 'pay',
              label: '记收款',
              children: (
                <Form form={payForm} layout='vertical' onFinish={doPay} style={{ maxWidth: 420 }}>
                  <Form.Item label='金额（元，填负数就是退回去的钱）' name='amount' rules={[{ required: true, message: '填一下金额' }]}>
                    <InputNumber className='num' style={{ width: '100%' }} precision={2} />
                  </Form.Item>
                  <Form.Item label='方式' name='method' rules={[{ required: true }]}>
                    <Select options={METHODS} />
                  </Form.Item>
                  <Form.Item label='备注' name='remark'>
                    <Input placeholder='比如：定金' />
                  </Form.Item>
                  <Button type='primary' htmlType='submit'>记下</Button>
                </Form>
              )
            },
            {
              key: 'edit',
              label: '改单',
              children: (
                <Form form={editForm} layout='vertical' onFinish={doEdit} style={{ maxWidth: 480 }}>
                  <Form.Item label='收货人' name='receiver_name' rules={[{ required: true }]}>
                    <Input />
                  </Form.Item>
                  <Form.Item
                    label='手机'
                    name='receiver_phone'
                    rules={[{ pattern: /^1[3-9]\d{9}$/, message: '手机号看着不对' }]}
                  >
                    <Input className='num' />
                  </Form.Item>
                  <Form.Item label='地址' name='receiver_address' rules={[{ required: true }]}>
                    <Input.TextArea autoSize={{ minRows: 2 }} />
                  </Form.Item>
                  <Form.Item label='微信备注' name='wechat_note'><Input /></Form.Item>
                  <Form.Item label='约定发货日' name='plan_ship_date'>
                    <Input className='num' placeholder='2026-09-16' />
                  </Form.Item>
                  <Form.Item label='备注' name='remark'>
                    <Input.TextArea autoSize={{ minRows: 2 }} />
                  </Form.Item>
                  <Button type='primary' htmlType='submit'>存下来</Button>
                </Form>
              )
            }
          ]}
        />
      ) : null}
    </Drawer>
  )
}
