import { Layout, Menu, Button } from 'antd'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { clearToken } from '../api/client'

const ITEMS = [
  { key: '/orders', label: '订单' },
  { key: '/specs', label: '价目表' },
  { key: '/stats', label: '统计' }
]

export default function Shell() {
  const nav = useNavigate()
  const { pathname } = useLocation()
  const active = ITEMS.find((i) => pathname.startsWith(i.key))?.key || '/orders'

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Header style={{ display: 'flex', alignItems: 'center', paddingInline: 24 }}>
        <span style={{ color: '#fff', fontWeight: 600, fontSize: 18, letterSpacing: '.08em', marginRight: 40 }}>
          蟹记
        </span>
        <Menu
          theme='dark'
          mode='horizontal'
          selectedKeys={[active]}
          items={ITEMS}
          onClick={({ key }) => nav(key)}
          style={{ flex: 1, minWidth: 0, background: 'transparent' }}
        />
        <Button
          type='text'
          style={{ color: '#EFF2EE' }}
          onClick={() => { clearToken(); nav('/login', { replace: true }) }}
        >
          退出
        </Button>
      </Layout.Header>
      <Layout.Content style={{ padding: 24 }}>
        <Outlet />
      </Layout.Content>
    </Layout>
  )
}
