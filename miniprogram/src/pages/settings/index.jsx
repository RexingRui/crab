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

const EMPTY_SPEC = { id: null, gender: 'male', size: '', unit: '只', unit_price: '', enabled: true, sort: 0 }

export default function Settings() {
  const demo = useDemoMode()
  const [specs, setSpecs] = useState([])
  const [editing, setEditing] = useState(null)
  const [trackUrl, setTrackUrlState] = useState(getTrackUrl())
  const [baseUrl, setBaseUrlState] = useState(getBaseUrl())

  const load = useCallback(async () => {
    await whenReady()
    try {
      const res = await api.specs()
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
    if (!editing.size.trim()) { toast('填一下规格，比如 4.5两'); return }
    const body = {
      gender: editing.gender,
      size: editing.size.trim(),
      unit: editing.unit || '只',
      unit_price: yuanToFen(editing.unit_price),
      enabled: editing.enabled,
      sort: Number(editing.sort) || 0
    }
    if (editing.id) await api.updateSpec(editing.id, body)
    else await api.createSpec(body)
    setEditing(null)
    toast('改价只影响以后新记的单')
    load()
  }

  async function removeSpec(spec) {
    if (guardDemo(toast)) return
    const { confirm } = await Taro.showModal({
      title: '删掉这一档？',
      content: '已经记过的单不受影响',
      confirmColor: '#D2542A'
    })
    if (!confirm) return
    await api.removeSpec(spec.id)
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
              <Text className='settings__spec-name'>{spec.gender_text || (spec.gender === 'male' ? '公' : '母')} {spec.size}</Text>
              <Text className='settings__spec-price num'>{fenToYuan(spec.unit_price)}</Text>
              {spec.enabled === false ? <Text className='sub'>停用</Text> : null}
              <Text className='settings__link' onClick={() => editSpec(spec)}>改</Text>
              <Text className='settings__del' onClick={() => removeSpec(spec)}>删</Text>
            </View>
          ))}
          {specs.length === 0 ? <Text className='settings__blank sub'>还没配规格。</Text> : null}
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
              {[['male', '公'], ['female', '母']].map(([key, label]) => (
                <Text
                  key={key}
                  className={`settings__seg-item ${editing.gender === key ? 'settings__seg-item--on' : ''}`}
                  onClick={() => setEditing({ ...editing, gender: key })}
                >
                  {label}
                </Text>
              ))}
            </View>
            <View className='field'>
              <Text className='field__label'>规格</Text>
              <Input
                className='field__input'
                value={editing.size}
                placeholder='4.5两'
                onInput={(e) => setEditing({ ...editing, size: e.detail.value })}
              />
            </View>
            <View className='field'>
              <Text className='field__label'>单价</Text>
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
                value={String(editing.sort)}
                onInput={(e) => setEditing({ ...editing, sort: e.detail.value })}
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
