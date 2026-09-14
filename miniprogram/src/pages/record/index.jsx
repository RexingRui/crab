import { useState } from 'react'
import { useDidShow } from '@tarojs/taro'
import OrderForm from '../../components/OrderForm'

/* tabBar 里的「记一笔」。表单本体在 OrderForm，改单页共用同一个组件。 */
export default function Record() {
  const [clipboardTick, setClipboardTick] = useState(0)

  // 每次切回这个 tab 都重新看一眼剪贴板
  useDidShow(() => setClipboardTick((n) => n + 1))

  return <OrderForm clipboardTick={clipboardTick} />
}
