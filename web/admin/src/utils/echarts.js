/* 只注册用得上的图表和组件，别把整个 echarts 打进产物——
 * 后台是 embed 进 Go 二进制的，体积省一点是一点。 */
import * as echarts from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

echarts.use([BarChart, LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

export default echarts
