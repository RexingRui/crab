import { useEffect, useState } from 'react'
import { isDemoMode, onSessionChange } from '../utils/session'

/** 订阅演示模式开关，登录返回 40300 时会自动翻转 */
export function useDemoMode() {
  const [demo, setDemo] = useState(isDemoMode())
  useEffect(() => onSessionChange((s) => setDemo(s.demoMode)), [])
  return demo
}

/** 写操作前挡一道：演示模式下只提示，不发请求 */
export function guardDemo(toastFn) {
  if (isDemoMode()) {
    toastFn('演示模式下不能修改')
    return true
  }
  return false
}
