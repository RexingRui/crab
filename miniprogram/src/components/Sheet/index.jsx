import { View, Text } from '@tarojs/components'
import './index.scss'

/**
 * 浮层里的输入框弹键盘时，光标离键盘至少留这么多（px）。
 * 默认是 0：页面只顶到光标那一行，输入框下半截和下面的内容会被键盘盖住。
 */
export const CURSOR_SPACING = 50

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
