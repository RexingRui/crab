import { useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { View, Text } from '@tarojs/components'
import OrderForm from '../../components/OrderForm'
import api from '../../utils/api'
import { getTrackUrl } from '../../utils/session'
import { formatDate } from '../../utils/format'
import { guardDemo } from '../../hooks/useDemoMode'
import { toast } from '../../utils/request'
import './index.scss'

/* tabBar 里的「记一笔」。表单本体在 OrderForm，改单页共用同一个组件。
 * 顶上那条是同一件事的另一种做法：把链接发给买家，让他自己填收货信息。
 * 入口放这儿而不是首页——它和录单是替代关系，摆在一起才看得出是二选一。 */
export default function Record() {
  const [clipboardTick, setClipboardTick] = useState(0)

  // 每次切回这个 tab 都重新看一眼剪贴板
  useDidShow(() => setClipboardTick((n) => n + 1))

  async function sendLink() {
    if (guardDemo(toast)) return

    // 备注名先写好，买家那头改不了，省得回来一堆「张先生」对不上人
    const { confirm, content } = await Taro.showModal({
      title: '发条链接让买家自己填',
      content: '',
      editable: true,
      placeholderText: '给这位买家记个备注名（可不填）',
      confirmText: '生成'
    }).catch(() => ({ confirm: false }))
    if (!confirm) return

    const res = await api.createRegLink(String(content || '').trim()).catch(() => null)
    if (!res || !res.path) return

    // 域名和查单页共用一个，在「设置」里改
    const link = `${getTrackUrl()}${res.path}`
    const deadline = formatDate(res.expires_at, 'M月D日')
    const text =
      `麻烦在这儿填一下收货信息：\n${link}\n` +
      (deadline ? `（${deadline} 前有效，填完我跟你确认）` : '（填完我跟你确认）')

    Taro.setClipboardData({ data: text }).then(() => toast('已复制，去微信粘贴给买家'))
  }

  return (
    <View>
      <View className='record__link' onClick={sendLink}>
        <Text className='record__link-text'>让买家自己填 · 发条登记链接</Text>
        <Text className='record__link-arrow'>›</Text>
      </View>
      <OrderForm clipboardTick={clipboardTick} />
    </View>
  )
}
