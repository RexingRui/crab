import { Tag } from 'antd'
import { statusColor, statusText } from '../utils/format'

// 完成态一律灰，只有需要动手的才有颜色
const STYLES = {
  claw: { color: '#A68620', background: 'rgba(200,162,39,.14)', borderColor: 'rgba(200,162,39,.35)' },
  shell: { color: '#2F4739', background: 'rgba(47,71,57,.10)', borderColor: 'rgba(47,71,57,.25)' },
  roe: { color: '#AC7B16', background: 'rgba(232,180,74,.18)', borderColor: 'rgba(232,180,74,.4)' },
  boiled: { color: '#D2542A', background: 'rgba(210,84,42,.12)', borderColor: 'rgba(210,84,42,.3)' },
  muted: { color: '#6B7670', background: 'rgba(107,118,112,.10)', borderColor: 'rgba(107,118,112,.25)' }
}

export default function StatusTag({ kind, value, text, muted }) {
  if (!value) return null
  // 单子取消了就没有要动手的事，收款状态一并淡化
  const tone = muted ? 'muted' : statusColor(kind, value)
  return <Tag style={{ ...STYLES[tone], marginInlineEnd: 4 }}>{statusText(kind, value, text)}</Tag>
}
