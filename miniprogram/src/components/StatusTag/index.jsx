import { Text } from '@tarojs/components'
import { statusColor, statusText } from '../../utils/format'
import './index.scss'

/**
 * 状态标签。完成态一律灰色，只有需要动手的才有颜色——
 * 这是整套界面唯一的信息层级，别到处上色把它稀释掉。
 */
export default function StatusTag({ kind, value, text, flash, muted }) {
  if (!value) return null
  // 单子取消了就没有要动手的事，收款状态一并淡化
  const tone = muted ? 'muted' : statusColor(kind, value)
  return (
    <Text className={`status-tag status-tag--${tone} ${flash ? 'status-tag--flash' : ''}`}>
      {statusText(kind, value, text)}
    </Text>
  )
}
