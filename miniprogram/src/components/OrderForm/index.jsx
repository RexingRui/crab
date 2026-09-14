import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { View, Text, Input, Textarea, Picker } from '@tarojs/components'
import DemoBanner from '../DemoBanner'
import SpecPicker from '../SpecPicker'
import api from '../../utils/api'
import { parseAddress, looksLikeAddressText } from '../../utils/address'
import { fenToYuan, yuanToFen, todayStr } from '../../utils/format'
import { openidTail, whenReady } from '../../utils/session'
import { guardDemo } from '../../hooks/useDemoMode'
import { toast } from '../../utils/request'
import './index.scss'

/* 整个产品的价值就集中在这一页能不能在 20 秒内录完一单。
 * 所以主角是顶部那个粘贴框，其余字段都排在它后面。
 *
 * 录单页和改单页共用这个组件：
 * - 不带 orderId  → 新记一笔（tabBar 里的「记一笔」）
 * - 带 orderId    → 改单（详情页「编辑」进来。tabBar 页不能 navigateTo 带参数，
 *                   所以改单单独开了 pages/edit）
 * 生命周期钩子留在宿主页面里，这里只认 clipboardTick 的变化。 */

function newRequestId() {
  const rand = String(Math.floor(Math.random() * 10000)).padStart(4, '0')
  return `${openidTail()}-${Date.now()}-${rand}`
}

export default function OrderForm({ orderId, clipboardTick = 0 }) {
  const [form, setForm] = useState({
    receiver_name: '',
    phone: '',
    address: '',
    wechat_remark: '',
    freight_fee: '',
    discount: '',
    expect_ship_date: '',
    remark: ''
  })
  const [items, setItems] = useState([])
  const [specs, setSpecs] = useState([])
  const [clip, setClip] = useState(null)
  const [highlight, setHighlight] = useState({})
  const [pickerOpen, setPickerOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const editId = orderId
  const [editing, setEditing] = useState(null)

  // 提交失败重试复用同一个 request_id，成功后才换新的
  const requestId = useRef(newRequestId())
  const clipChecked = useRef('')

  useEffect(() => {
    whenReady().then(() => api.specs()).then((res) => {
      setSpecs((res && res.list) || [])
    }).catch(() => {})
  }, [])

  useEffect(() => {
    if (!editId) return
    whenReady().then(() => api.order(editId)).then((o) => {
      if (!o) return
      setEditing(o)
      setForm({
        receiver_name: o.receiver_name || '',
        phone: o.phone || '',
        address: o.address || '',
        wechat_remark: o.wechat_remark || '',
        freight_fee: o.freight_fee ? fenToYuan(o.freight_fee, { symbol: false }) : '',
        discount: o.discount ? fenToYuan(o.discount, { symbol: false }) : '',
        expect_ship_date: o.expect_ship_date || '',
        remark: o.remark || ''
      })
      setItems(o.items || [])
    }).catch(() => {})
  }, [editId])

  // 每次页面显示都看一眼剪贴板，但绝不静默填入
  useEffect(() => {
    if (editId) return // 改单时不打扰
    if (!clipboardTick) return
    Taro.getClipboardData().then(({ data }) => {
      const text = String(data || '').trim()
      if (!text || text === clipChecked.current) return
      if (!looksLikeAddressText(text)) return
      const parsed = parseAddress(text)
      if (!parsed.name && !parsed.phone && !parsed.address) return
      setClip({ text, parsed })
    }).catch(() => {})
  }, [clipboardTick, editId])

  const update = useCallback((key, value) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }, [])

  function applyClip() {
    if (!clip) return
    const { parsed } = clip
    setForm((prev) => ({
      ...prev,
      receiver_name: parsed.name || prev.receiver_name,
      phone: parsed.phone || prev.phone,
      address: parsed.address || prev.address
    }))
    // 填进来的字段亮一秒，提醒核对
    setHighlight({
      receiver_name: Boolean(parsed.name),
      phone: Boolean(parsed.phone),
      address: Boolean(parsed.address)
    })
    setTimeout(() => setHighlight({}), 1000)
    clipChecked.current = clip.text
    setClip(null)
    toast('填好了，核对一下')
  }

  function dismissClip() {
    if (clip) clipChecked.current = clip.text
    setClip(null)
  }

  function addItem(item) {
    setItems((prev) => [...prev, item])
    setPickerOpen(false)
  }

  function removeItem(index) {
    setItems((prev) => prev.filter((_, i) => i !== index))
  }

  // 前端只做展示计算，最终金额以后端返回为准
  const preview = useMemo(() => {
    const crabTotal = items.reduce((sum, it) => sum + it.unit_price * it.quantity, 0)
    return crabTotal + yuanToFen(form.freight_fee) - yuanToFen(form.discount)
  }, [items, form.freight_fee, form.discount])

  function validate() {
    if (!form.receiver_name.trim()) return '写一下收货人'
    if (!/^1[3-9]\d{9}$/.test(form.phone.trim())) return '手机号看着不对'
    if (!form.address.trim()) return '地址还没填'
    if (!items.length) return '还没挑规格'
    return ''
  }

  async function submit() {
    if (submitting) return
    if (guardDemo(toast)) return
    const problem = validate()
    if (problem) { toast(problem); return }

    setSubmitting(true)
    try {
      const body = {
        receiver_name: form.receiver_name.trim(),
        phone: form.phone.trim(),
        address: form.address.trim(),
        wechat_remark: form.wechat_remark.trim(),
        // 明细是快照，整条带过去；后端不认 spec_id
        items: items.map((it) => ({
          gender: it.gender,
          spec_gram: it.spec_gram,
          spec_label: it.spec_label,
          unit: it.unit,
          quantity: it.quantity,
          unit_price: it.unit_price
        })),
        freight_fee: yuanToFen(form.freight_fee),
        discount: yuanToFen(form.discount),
        expect_ship_date: form.expect_ship_date,
        remark: form.remark.trim()
      }

      if (editId) {
        // 带上 updated_at 就是乐观锁：这期间别人改过就返回 40903
        await api.updateOrder(editId, { ...body, updated_at: editing && editing.updated_at })
        Taro.showToast({ title: '改好了', icon: 'success' })
        setTimeout(() => Taro.navigateBack(), 600)
        return
      }

      await api.createOrder({ request_id: requestId.current, ...body })

      requestId.current = newRequestId() // 成功了才换新的
      setForm({
        receiver_name: '', phone: '', address: '', wechat_remark: '',
        freight_fee: '', discount: '', expect_ship_date: '', remark: ''
      })
      setItems([])
      Taro.showToast({ title: '记下了', icon: 'success' })
      setTimeout(() => Taro.switchTab({ url: '/pages/home/index' }), 600)
    } catch (err) {
      // 失败保留 request_id，下次重试后端按幂等处理
      toast((err && err.msg) || (editId ? '没改上，再试一次' : '没记上，再试一次'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <View className='page page--with-bar'>
      <DemoBanner />

      {clip ? (
        <View className='clip section'>
          <Text className='clip__title'>剪贴板里有一段地址</Text>
          <Text className='clip__text'>{clip.text}</Text>
          <View className='clip__actions'>
            <Text className='clip__ignore' onClick={dismissClip}>不用</Text>
            <Text className='clip__fill' onClick={applyClip}>填进来</Text>
          </View>
        </View>
      ) : null}

      <View className='section card'>
        <Field label='收货人'>
          <Input
            className={`field__input ${highlight.receiver_name ? 'field__input--highlight' : ''}`}
            value={form.receiver_name}
            placeholder='张三'
            onInput={(e) => update('receiver_name', e.detail.value)}
          />
        </Field>
        <Field label='手机'>
          <Input
            className={`field__input num ${highlight.phone ? 'field__input--highlight' : ''}`}
            type='number'
            maxlength={11}
            value={form.phone}
            placeholder='13800138000'
            onInput={(e) => update('phone', e.detail.value)}
          />
        </Field>
        <Field label='地址' textarea>
          <Textarea
            className={`field__input field__textarea ${highlight.address ? 'field__input--highlight' : ''}`}
            value={form.address}
            placeholder='省市区街道门牌'
            autoHeight
            onInput={(e) => update('address', e.detail.value)}
          />
        </Field>
        <Field label='微信备注'>
          <Input
            className='field__input'
            value={form.wechat_remark}
            placeholder='老张'
            onInput={(e) => update('wechat_remark', e.detail.value)}
          />
        </Field>
      </View>

      <View className='section'>
        <View className='section__head'>
          <Text className='group-title'>蟹</Text>
          <Text className='record__add' onClick={() => setPickerOpen(true)}>+ 加</Text>
        </View>

        <View className='card'>
          {items.length === 0 ? (
            <Text className='record__no-item'>还没挑规格，点右上角「+ 加」。</Text>
          ) : (
            items.map((it, index) => (
              <View className='record__item' key={`${it.spec_label}-${index}`}>
                <Text className='record__item-name'>{it.gender_text} {it.spec_label}</Text>
                <Text className='record__item-qty num'>×{it.quantity}</Text>
                <Text className='record__item-price num'>{fenToYuan(it.unit_price)}</Text>
                <Text className='record__item-amount num'>{fenToYuan(it.unit_price * it.quantity)}</Text>
                <Text className='record__item-del' onClick={() => removeItem(index)}>删</Text>
              </View>
            ))
          )}
        </View>
      </View>

      <View className='section card'>
        <View className='record__pair'>
          <View className='record__pair-cell'>
            <Text className='sub'>运费</Text>
            <Input
              className='record__pair-input num'
              type='digit'
              value={form.freight_fee}
              placeholder='0'
              onInput={(e) => update('freight_fee', e.detail.value)}
            />
          </View>
          <View className='record__pair-cell'>
            <Text className='sub'>优惠</Text>
            <Input
              className='record__pair-input num'
              type='digit'
              value={form.discount}
              placeholder='0'
              onInput={(e) => update('discount', e.detail.value)}
            />
          </View>
        </View>

        <Picker
          mode='date'
          value={form.expect_ship_date || todayStr()}
          onChange={(e) => update('expect_ship_date', e.detail.value)}
        >
          <Field label='约定发货'>
            <Text className={`field__input num ${form.expect_ship_date ? '' : 'field__input--empty'}`}>
              {form.expect_ship_date || '选个日期'}
            </Text>
          </Field>
        </Picker>

        <Field label='备注' textarea>
          <Textarea
            className='field__input field__textarea'
            value={form.remark}
            placeholder='周五之前务必发出'
            autoHeight
            onInput={(e) => update('remark', e.detail.value)}
          />
        </Field>
      </View>

      <View className='bar-bottom'>
        <View className='record__total'>
          <Text className='sub'>应收</Text>
          <Text className='money-lg'>{fenToYuan(preview)}</Text>
        </View>
        <View
          className={`btn btn--primary ${submitting ? 'btn--off' : ''}`}
          onClick={submitting ? undefined : submit}
        >
          {submitting ? (editId ? '保存中' : '记录中') : (editId ? '存下来' : '记下')}
        </View>
      </View>

      <SpecPicker
        visible={pickerOpen}
        specs={specs}
        onClose={() => setPickerOpen(false)}
        onConfirm={addItem}
      />
    </View>
  )
}

function Field({ label, textarea, children }) {
  return (
    <View className={`field ${textarea ? 'field--textarea' : ''}`}>
      <Text className='field__label'>{label}</Text>
      {children}
    </View>
  )
}
