import { useRouter } from '@tarojs/taro'
import OrderForm from '../../components/OrderForm'

/* 改单页。「记一笔」是 tabBar 页，navigateTo 到 tabBar 页是非法的，
 * 而 switchTab 又带不了参数，所以改单单独开一页，复用同一个表单。 */
export default function Edit() {
  const router = useRouter()
  return <OrderForm orderId={router.params.id} />
}
