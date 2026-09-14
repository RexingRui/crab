import { useEffect, useMemo, useState } from 'react'
import { View, Text, Input } from '@tarojs/components'
import Sheet from '../Sheet'
import { fenToYuan, yuanToFen } from '../../utils/format'
import './index.scss'

/**
 * 半屏规格选择器。单价默认带出价目表的价，但必须可改——
 * 熟客让价、大客户批发价是常态，不要因为有价目表就设成只读。
 *
 * 注意：后端的订单明细是**快照**，不关联 specs 表（改价不影响历史单），
 * 所以这里要把整条规格带过去，不能只传 spec_id。
 */
export default function SpecPicker({ visible, specs = [], onClose, onConfirm }) {
  const [gender, setGender] = useState('male')
  const [specId, setSpecId] = useState(null)
  const [quantity, setQuantity] = useState(1)
  const [priceInput, setPriceInput] = useState('')

  const list = useMemo(
    () => specs.filter((s) => s.gender === gender && s.enabled !== false),
    [specs, gender]
  )

  const current = useMemo(
    () => specs.find((s) => s.id === specId) || null,
    [specs, specId]
  )

  useEffect(() => {
    if (!visible) return
    setGender('male')
    setSpecId(null)
    setQuantity(1)
    setPriceInput('')
  }, [visible])

  function pick(spec) {
    setSpecId(spec.id)
    setPriceInput(fenToYuan(spec.unit_price, { symbol: false, alwaysCents: true }))
  }

  function step(delta) {
    setQuantity((q) => Math.max(1, Math.min(999, q + delta)))
  }

  const unitPrice = priceInput === '' ? (current ? current.unit_price : 0) : yuanToFen(priceInput)
  const amount = unitPrice * quantity

  function confirm() {
    if (!current) return
    onConfirm({
      gender: current.gender,
      gender_text: current.gender_text,
      spec_gram: current.spec_gram,
      spec_label: current.spec_label,
      unit: current.unit,
      unit_text: current.unit_text,
      unit_price: unitPrice,
      quantity,
      amount
    })
  }

  return (
    <Sheet
      visible={visible}
      title='选规格'
      onClose={onClose}
      footer={
        <View
          className={`btn btn--primary btn--block ${current ? '' : 'btn--off'}`}
          onClick={current ? confirm : undefined}
        >
          加进来{current ? ` ${fenToYuan(amount)}` : ''}
        </View>
      }
    >
      <View className='spec-picker__segment'>
        {[['male', '公'], ['female', '母']].map(([key, label]) => (
          <Text
            key={key}
            className={`spec-picker__seg ${gender === key ? 'spec-picker__seg--on' : ''}`}
            onClick={() => setGender(key)}
          >
            {label}
          </Text>
        ))}
      </View>

      <View className='spec-picker__grid'>
        {list.map((spec) => (
          <View
            key={spec.id}
            className={`spec-picker__cell ${specId === spec.id ? 'spec-picker__cell--on' : ''}`}
            onClick={() => pick(spec)}
          >
            <Text className='spec-picker__size'>{spec.spec_label}</Text>
            <Text className='spec-picker__price num'>{fenToYuan(spec.unit_price)}</Text>
          </View>
        ))}
        {list.length === 0 ? <Text className='sub'>这一档还没配规格，去设置里加一个。</Text> : null}
      </View>

      <View className='spec-picker__line'>
        <Text className='spec-picker__label'>数量</Text>
        <View className='spec-picker__stepper'>
          <Text className='spec-picker__step' onClick={() => step(-1)}>−</Text>
          <Text className='spec-picker__count num'>{quantity}</Text>
          <Text className='spec-picker__step' onClick={() => step(1)}>+</Text>
        </View>
      </View>

      <View className='spec-picker__line'>
        <Text className='spec-picker__label'>单价</Text>
        <Input
          className='spec-picker__input num'
          type='digit'
          value={priceInput}
          placeholder='0.00'
          onInput={(e) => setPriceInput(e.detail.value)}
        />
        <Text className='sub'>改了只影响本单</Text>
      </View>
    </Sheet>
  )
}
