import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { View, Text, Input, Switch } from '@tarojs/components'
import DemoBanner from '../../components/DemoBanner'
import Sheet from '../../components/Sheet'
import api from '../../utils/api'
import { fenToYuan, yuanToFen } from '../../utils/format'
import { VERSION } from '../../utils/config'
import {
  clearToken, getBaseUrl, getTrackUrl, setBaseUrl, setTrackUrl, whenReady, isDemoMode
} from '../../utils/session'
import { guardDemo, useDemoMode } from '../../hooks/useDemoMode'
import { toast, login } from '../../utils/request'
import './index.scss'

const EMPTY_SPEC = {
  id: null, gender: 'mixed', spec_gram: '', spec_label: '',
  unit: 'box', unit_price: '', pack_size: 8, enabled: true, sort_no: 0
}

/* 一档要么是按盒卖的套餐（一盒里公母都有），要么是按只卖的单规格。
 * 选哪种决定了 gender / unit / pack_size 三个字段，所以在一个地方切。 */
const SPEC_KINDS = [['mixed', '套餐'], ['male', '公'], ['female', '母']]

function kindPatch(kind) {
  return kind === 'mixed'
    ? { gender: 'mixed', unit: 'box', pack_size: 8 }
    : { gender: kind, unit: 'piece', pack_size: 0 }
}

export default function Settings() {
  const demo = useDemoMode()
  const [specs, setSpecs] = useState([])
  const [editing, setEditing] = useState(null)
  const [trackUrl, setTrackUrlState] = useState(getTrackUrl())
  const [baseUrl, setBaseUrlState] = useState(getBaseUrl())

  const load = useCallback(async () => {
    await whenReady()
    try {
      const res = await api.allSpecs()
      setSpecs((res && res.list) || [])
    } catch (e) { /* 已统一提示 */ }
  }, [])

  useEffect(() => { load() }, [load])

  function editSpec(spec) {
    setEditing({
      ...(spec || EMPTY_SPEC),
      unit_price: spec ? fenToYuan(spec.unit_price, { symbol: false, alwaysCents: true }) : ''
    })
  }

  async function saveSpec() {
    if (guardDemo(toast)) return
    const isPack = editing.gender === 'mixed'
    if (!String(editing.spec_label).trim()) {
      toast(isPack ? '填一下档名，比如 8只装 母2.5两/公3.5两' : '填一下规格，比如 4.5两')
      return
    }
    const gram = Number(editing.spec_gram)
    if (!gram || gram <= 0) {
      toast(isPack ? '填一下整盒克重，8 只加起来多少克' : '填一下克重，4.5两就是 225')
      return
    }
    const packSize = isPack ? Number(editing.pack_size) : 0
    if (isPack && (!packSize || packSize < 1)) { toast('填一下一盒几只'); return }
    const body = {
      gender: editing.gender,
      spec_gram: gram,
      spec_label: String(editing.spec_label).trim(),
      unit: isPack ? 'box' : 'piece',
      unit_price: yuanToFen(editing.unit_price),
      pack_size: packSize,
      enabled: editing.enabled !== false,
      sort_no: Number(editing.sort_no) || 0
    }
    if (editing.id) await api.updateSpec(editing.id, body)
    else await api.createSpec(body)
    setEditing(null)
    toast('改价只影响以后新记的单')
    load()
  }

  async function disableSpec(spec) {
    if (guardDemo(toast)) return
    const { confirm } = await Taro.showModal({
      title: '停用这一档？',
      content: '以后记单时不再出现，已经记过的单不受影响',
      confirmColor: '#D2542A'
    })
    if (!confirm) return
    // 后端是软停用，不物理删
    await api.disableSpec(spec.id)
    load()
  }

  /* 导出 CSV：下载到本地再用系统程序打开 */
  async function exportCsv() {
    if (isDemoMode()) { toast('演示模式下不能导出'); return }
    Taro.showLoading({ title: '正在导出' })
    try {
      const res = await Taro.downloadFile({
        url: `${getBaseUrl()}/api/orders/export`,
        header: { Authorization: `Bearer ${Taro.getStorageSync('token')}` }
      })
      Taro.hideLoading()
      if (res.statusCode !== 200) { toast('导出失败'); return }
      await Taro.openDocument({ filePath: res.tempFilePath, fileType: 'csv', showMenu: true })
    } catch (e) {
      Taro.hideLoading()
      toast('导出失败，检查一下网络')
    }
  }

  function saveUrls() {
    setTrackUrl(trackUrl.trim().replace(/\/$/, ''))
    setBaseUrl(baseUrl.trim().replace(/\/$/, ''))
    toast('存好了')
  }

  async function relogin() {
    clearToken()
    try {
      await login()
      toast('重新登上了')
    } catch (e) {
      toast('登录没成功')
    }
  }

  return (
    <View className='page'>
      <DemoBanner />

      <View className='section'>
        <View className='section__head'><Text className='group-title'>价目表</Text></View>
        <View className='card'>
          {specs.map((spec) => (
            <View className='settings__spec' key={spec.id}>
              <Text className='settings__spec-name'>
                {/* 套餐的档名里已经写了盒里装什么，不再加「公母」前缀 */}
                {spec.gender === 'mixed' ? '' : `${spec.gender_text || (spec.gender === 'male' ? '公' : '母')} `}
                {spec.spec_label}
              </Text>
              <Text className='settings__spec-price num'>{fenToYuan(spec.unit_price)}</Text>
              {spec.enabled === false ? <Text className='sub'>停用</Text> : null}
              <Text className='settings__link' onClick={() => editSpec(spec)}>改</Text>
              <Text className='settings__del' onClick={() => disableSpec(spec)}>停用</Text>
            </View>
          ))}
          {specs.length === 0 ? <Text className='settings__blank sub'>还没配价目表。</Text> : null}
          <View className='settings__spec-add' onClick={() => editSpec(null)}>+ 加一档</View>
        </View>
        <Text className='settings__hint sub'>改价只影响以后新记的单，已经记过的不动。</Text>
      </View>

      <View className='section'>
        <View className='section__head'><Text className='group-title'>查单页域名</Text></View>
        <View className='card'>
          <View className='field'>
            <Text className='field__label'>查单域名</Text>
            <Input
              className='field__input'
              value={trackUrl}
              placeholder='https://你的域名'
              onInput={(e) => setTrackUrlState(e.detail.value)}
            />
          </View>
          <View className='field'>
            <Text className='field__label'>接口域名</Text>
            <Input
              className='field__input'
              value={baseUrl}
              placeholder='https://你的域名'
              onInput={(e) => setBaseUrlState(e.detail.value)}
            />
          </View>
        </View>
        <View className='settings__row'>
          <View className='btn btn--ghost btn--sm' onClick={saveUrls}>存下来</View>
        </View>
      </View>

      <View className='section'>
        <View className='card'>
          <View className='settings__item' onClick={exportCsv}>
            <Text>导出 CSV</Text>
            <Text className='settings__arrow'>›</Text>
          </View>
          <View className='settings__item' onClick={relogin}>
            <Text>{demo ? '演示模式 · 重新登录' : '重新登录'}</Text>
            <Text className='settings__arrow'>›</Text>
          </View>
          <View className='settings__item'>
            <Text>版本</Text>
            <Text className='sub num'>{VERSION}</Text>
          </View>
        </View>
      </View>

      <Sheet
        visible={Boolean(editing)}
        title={editing && editing.id ? '改一档' : '加一档'}
        onClose={() => setEditing(null)}
        footer={<View className='btn btn--primary btn--block' onClick={saveSpec}>存下来</View>}
      >
        {editing ? (
          <>
            <View className='settings__seg'>
              {SPEC_KINDS.map(([key, label]) => (
                <Text
                  key={key}
                  className={`settings__seg-item ${editing.gender === key ? 'settings__seg-item--on' : ''}`}
                  onClick={() => setEditing({ ...editing, ...kindPatch(key) })}
                >
                  {label}
                </Text>
              ))}
            </View>
            <View className='field'>
              <Text className='field__label'>{editing.gender === 'mixed' ? '档名' : '规格'}</Text>
              <Input
                className='field__input'
                value={editing.spec_label}
                placeholder={editing.gender === 'mixed' ? '8只装 母2.5两/公3.5两' : '4.5两'}
                onInput={(e) => setEditing({ ...editing, spec_label: e.detail.value })}
              />
            </View>
            {editing.gender === 'mixed' ? (
              <View className='field'>
                <Text className='field__label'>一盒几只</Text>
                <Input
                  className='field__input num'
                  type='number'
                  value={String(editing.pack_size)}
                  placeholder='8'
                  onInput={(e) => setEditing({ ...editing, pack_size: e.detail.value })}
                />
                <Text className='sub'>公母比例买家自己调</Text>
              </View>
            ) : null}
            <View className='field'>
              <Text className='field__label'>克重</Text>
              <Input
                className='field__input num'
                type='number'
                value={String(editing.spec_gram)}
                placeholder={editing.gender === 'mixed' ? '整盒加起来多少克' : '4.5两就是 225'}
                onInput={(e) => setEditing({ ...editing, spec_gram: e.detail.value })}
              />
            </View>
            <View className='field'>
              <Text className='field__label'>{editing.gender === 'mixed' ? '每盒' : '单价'}</Text>
              <Input
                className='field__input num'
                type='digit'
                value={String(editing.unit_price)}
                placeholder='0.00'
                onInput={(e) => setEditing({ ...editing, unit_price: e.detail.value })}
              />
            </View>
            <View className='field'>
              <Text className='field__label'>排序</Text>
              <Input
                className='field__input num'
                type='number'
                value={String(editing.sort_no)}
                onInput={(e) => setEditing({ ...editing, sort_no: e.detail.value })}
              />
            </View>
            <View className='field'>
              <Text className='field__label'>启用</Text>
              <Switch
                checked={editing.enabled !== false}
                color='#2F4739'
                onChange={(e) => setEditing({ ...editing, enabled: e.detail.value })}
              />
            </View>
          </>
        ) : null}
      </Sheet>
    </View>
  )
}
