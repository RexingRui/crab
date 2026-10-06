import { useState } from 'react'
import { View } from '@tarojs/components'
import Sheet from '../Sheet'
import FreightFields from '../FreightFields'

/** 单独填 / 改运费：发货时没填的补上，或者重量复核后改价。 */
export default function FreightSheet({ visible, order, submitting, onClose, onSubmit }) {
  const [freight, setFreight] = useState({ empty: true, problem: '', payload: null })
  const ready = !freight.empty && !freight.problem
  const filled = order && order.freight_cost != null

  return (
    <Sheet
      visible={visible}
      title={filled ? '改运费' : '填运费'}
      onClose={onClose}
      footer={
        <View
          className={`btn btn--primary btn--block ${ready && !submitting ? '' : 'btn--off'}`}
          onClick={ready && !submitting ? () => onSubmit(freight.payload) : undefined}
        >
          {submitting ? '提交中' : '记下'}
        </View>
      }
    >
      <FreightFields order={order} visible={visible} onChange={setFreight} />
    </Sheet>
  )
}
