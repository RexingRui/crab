import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ConfigProvider, App as AntApp } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import dayjs from 'dayjs'
import quarterOfYear from 'dayjs/plugin/quarterOfYear'
import 'dayjs/locale/zh-cn'

// 统计页默认看本季，startOf('quarter') 要这个插件才有效
dayjs.extend(quarterOfYear)
import App from './App'
import './styles/tokens.css'

// antd 的主题对齐蟹壳青那一套
const theme = {
  token: {
    colorPrimary: '#2F4739',
    colorError: '#D2542A',
    colorWarning: '#C8A227',
    colorTextBase: '#1C231E',
    colorBgLayout: '#EFF2EE',
    borderRadius: 8,
    fontSize: 14
  },
  components: {
    Table: { headerBg: '#F5F7F4', rowHoverBg: '#F2F5F1' },
    Layout: { siderBg: '#2F4739', headerBg: '#2F4739' },
    Menu: {
      darkItemBg: '#2F4739',
      darkItemSelectedBg: '#1C231E',
      darkSubMenuItemBg: '#2F4739'
    }
  }
}

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <ConfigProvider locale={zhCN} theme={theme}>
      <AntApp>
        <BrowserRouter basename='/admin'>
          <App />
        </BrowserRouter>
      </AntApp>
    </ConfigProvider>
  </React.StrictMode>
)
