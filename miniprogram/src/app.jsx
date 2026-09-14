import { useLaunch } from '@tarojs/taro'
import { login } from './utils/request'
import { setReady, enterDemoMode } from './utils/session'
import './app.scss'

function App({ children }) {
  useLaunch(() => {
    // 页面请求前会 await 这个 Promise，避免首页比登录先发请求
    const ready = login().catch((err) => {
      // 不在管理员白名单里（审核员就是这种）→ 只读演示模式，页面照常渲染
      if (err && err.code === 40300) enterDemoMode()
      return null
    })
    setReady(ready)
  })

  return children
}

export default App
