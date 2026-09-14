/* 图表配色：品牌色直接上图会失败——蟹壳青 #2F4739 太暗太灰（明度 0.374、彩度 0.038），
 * 所以取同色相里能过校验的一档。下面这对跑过了色盲分离度、明度带、彩度下限和对比度：
 *   #2E7D52 ↔ #A6851C  protan ΔE 9.1 · 正常视觉 ΔE 16.0 · 都 ≥ 3:1
 * 类别色按固定顺序分配，不轮转；换了筛选条件也不能给同一个类别换色。 */
export const SERIES = {
  male: '#2E7D52',   // 公 — 蟹壳青的图表档
  female: '#A6851C'  // 母 — 金爪的图表档
}

export const INK = '#1C231E'
export const INK_MUTED = '#6B7670'
export const GRID_LINE = '#E4E8E3'

/** 坐标轴、网格一律退到背景里去，别跟数据抢 */
export const AXIS = {
  axisLine: { lineStyle: { color: GRID_LINE } },
  axisTick: { show: false },
  axisLabel: { color: INK_MUTED, fontSize: 12 },
  splitLine: { lineStyle: { color: GRID_LINE, type: 'dashed' } }
}

export const TOOLTIP = {
  trigger: 'axis',
  backgroundColor: '#FFFFFF',
  borderColor: GRID_LINE,
  borderWidth: 1,
  textStyle: { color: INK, fontSize: 13 },
  extraCssText: 'box-shadow: 0 4px 16px rgba(28,35,30,.12); border-radius: 8px;'
}
