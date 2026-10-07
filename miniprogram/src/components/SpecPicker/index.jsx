import { useEffect, useMemo, useState } from 'react'
import { View, Text, Input } from '@tarojs/components'
import Sheet from '../Sheet'
import { fenToYuan, milliToYuan, yuanToMilli, lineAmount, itemName, PACK_HINT } from '../../utils/format'
import './index.scss'

/**
 * 半屏规格选择器：选一种蟹（性别 + 克重 + 品相），填几只。按只计价，没有「盒」。
 *
 * 单只价默认带出价目表的价，但必须可改——熟客让价、大客户批发价是常态，
 * 不要因为有价目表就设成只读。单只价精确到厘（23.625 元），金额 = 只数 × 单只价，
 * 向上取整到元，和后端同一个公式（utils/format.lineAmount）。
 *
 * 注意：后端的订单明细是**快照**，不关联 specs 表（改价不影响历史单），
 * 所以这里要把整条规格带过去，不能只传 spec_id。
 */
/* 母蟹卖得多，排前面。哪些组出现由价目表决定，没有公蟹就不摆空页签。 */
const GROUPS = [['female', '母'], ['male', '公']]

export default function SpecPicker({ visible, specs = [], onClose, onConfirm }) {
  const [gender, setGender] = useState('')
  const [specId, setSpecId] = useState(null)
  const [quantity, setQuantity] = useState(PACK_HINT)
  const [priceInput, setPriceInput] = useState('')

  const enabled = useMemo(() => specs.filter((s) => s.enabled !== false), [specs])

  const groups = useMemo(
    () => GROUPS.filter(([key]) => enabled.some((s) => s.gender === key)),
    [enabled]
  )

  // 价目表是异步来的，第一次拿到时把页签落在第一个有东西的组上
  const activeGender = gender || (groups.length ? groups[0][0] : '')

  const list = useMemo(
    () => enabled.filter((s) => s.gender === activeGender),
    [enabled, activeGender]
  )

  const current = useMemo(
    () => specs.find((s) => s.id === specId) || null,
    [specs, specId]
  )

  useEffect(() => {
    if (!visible) return
    setGender('')
    setSpecId(null)
    setQuantity(PACK_HINT)
    setPriceInput('')
  }, [visible])

  function pick(spec) {
    setSpecId(spec.id)
    setPriceInput(milliToYuan(spec.unit_price_milli, { symbol: false }))
  }

  function step(delta) {
    setQuantity((q) => Math.max(1, Math.min(999, q + delta)))
  }

  const unitPriceMilli = priceInput === '' ? (current ? current.unit_price_milli : 0) : yuanToMilli(priceInput)
  const amount = lineAmount(quantity, unitPriceMilli)

  function confirm() {
    if (!current) return
    onConfirm({
      gender: current.gender,
      gender_text: current.gender_text,
      spec_gram: current.spec_gram,
      grade: current.grade,
      grade_text: current.grade_text,
      spec_label: current.spec_label,
      title: current.title,
      unit_price_milli: unitPriceMilli,
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
      {groups.length > 1 ? (
        <View className='spec-picker__segment'>
          {groups.map(([key, label]) => (
            <Text
              key={key}
              className={`spec-picker__seg ${activeGender === key ? 'spec-picker__seg--on' : ''}`}
              onClick={() => setGender(key)}
            >
              {label}
            </Text>
          ))}
        </View>
      ) : null}

      <View className='spec-picker__grid'>
        {list.map((spec) => (
          <View
            key={spec.id}
            className={`spec-picker__cell ${specId === spec.id ? 'spec-picker__cell--on' : ''}`}
            onClick={() => pick(spec)}
          >
            <Text className='spec-picker__size'>{itemName(spec)}</Text>
            <Text className='spec-picker__price num'>
              {fenToYuan(spec.pack_hint_amount != null
                ? spec.pack_hint_amount
                : lineAmount(PACK_HINT, spec.unit_price_milli))}/{PACK_HINT}只
            </Text>
            <Text className='spec-picker__unit num'>{milliToYuan(spec.unit_price_milli)}/只</Text>
          </View>
        ))}
        {list.length === 0 ? <Text className='sub'>价目表还是空的，去设置里加一档。</Text> : null}
      </View>

      <View className='spec-picker__line'>
        <Text className='spec-picker__label'>数量</Text>
        <View className='spec-picker__stepper'>
          <Text className='spec-picker__step' onClick={() => step(-1)}>−</Text>
          <Text className='spec-picker__count num'>{quantity}</Text>
          <Text className='spec-picker__step' onClick={() => step(1)}>+</Text>
        </View>
        <Text className='sub'>只</Text>
      </View>

      <View className='spec-picker__line'>
        <Text className='spec-picker__label'>单只价</Text>
        <Input
          className='spec-picker__input num'
          type='digit'
          value={priceInput}
          placeholder='0.000'
          onInput={(e) => setPriceInput(e.detail.value)}
        />
        <Text className='sub'>改了只影响本单</Text>
      </View>
    </Sheet>
  )
}
