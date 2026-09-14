import { View } from '@tarojs/components'
import { useDemoMode } from '../../hooks/useDemoMode'
import { DEMO_NOTICE } from '../../utils/demo'
import './index.scss'

/** 演示模式下的顶部提示，非演示模式什么都不渲染 */
export default function DemoBanner() {
  const demo = useDemoMode()
  if (!demo) return null
  return <View className='demo-banner'>{DEMO_NOTICE}</View>
}
