import { View } from '@tarojs/components'
import './index.scss'

/** 空状态。写清楚「现在是什么情况」，不写「暂无数据」。 */
export default function Empty({ text, children }) {
  return (
    <View className='empty'>
      <View className='empty__text'>{text}</View>
      {children ? <View className='empty__action'>{children}</View> : null}
    </View>
  )
}
