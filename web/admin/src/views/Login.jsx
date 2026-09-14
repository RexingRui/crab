import { useState } from 'react'
import { Button, Card, Form, Input, Typography, App } from 'antd'
import { useNavigate } from 'react-router-dom'
import { api, setToken } from '../api/client'

export default function Login() {
  const nav = useNavigate()
  const { message } = App.useApp()
  const [loading, setLoading] = useState(false)

  async function submit(values) {
    setLoading(true)
    try {
      const data = await api.login(values)
      setToken(data.token)
      nav('/orders', { replace: true })
    } catch (e) {
      // 42900 是登录失败次数超限，其余一律当成用户名或密码不对
      message.error(e.code === 42900 ? '试得太频繁了，等一分钟再来' : '用户名或密码不对')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', padding: 24 }}>
      <Card style={{ width: 360 }} variant='borderless'>
        <Typography.Title level={4} style={{ textAlign: 'center', letterSpacing: '.08em' }}>
          蟹记
        </Typography.Title>
        <Typography.Paragraph type='secondary' style={{ textAlign: 'center', marginTop: -8 }}>
          后台
        </Typography.Paragraph>
        <Form layout='vertical' onFinish={submit} autoComplete='off'>
          <Form.Item label='用户名' name='username' rules={[{ required: true, message: '填一下用户名' }]}>
            <Input size='large' autoFocus />
          </Form.Item>
          <Form.Item label='密码' name='password' rules={[{ required: true, message: '填一下密码' }]}>
            <Input.Password size='large' />
          </Form.Item>
          <Button type='primary' size='large' htmlType='submit' loading={loading} block>
            进去
          </Button>
        </Form>
      </Card>
    </div>
  )
}
