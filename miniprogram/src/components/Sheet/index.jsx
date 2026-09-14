import { View, Text } from '@tarojs/components'
import './index.scss'

/** 通用半屏浮层。浮层才有阴影，列表卡片只描边。 */
export default function Sheet({ visible, title, onClose, children, footer }) {
  if (!visible) return null
  return (
    <View className='sheet-root'>
      <View className='sheet-mask' onClick={onClose} />
      <View className='sheet' catchMove>
        <View className='sheet__head'>
          <Text className='sheet__title'>{title}</Text>
          <Text className='sheet__close' onClick={onClose}>关闭</Text>
        </View>
        <View className='sheet__body'>{children}</View>
        {footer ? <View className='sheet__foot'>{footer}</View> : null}
      </View>
    </View>
  )
}
