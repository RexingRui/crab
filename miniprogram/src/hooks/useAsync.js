import { useCallback, useEffect, useRef, useState } from 'react'
import { whenReady } from '../utils/session'

/**
 * 读接口的统一入口：等登录就绪 → 取数 → 存下来。
 * 错误提示已经在 request.js 里统一处理，这里只管状态。
 */
export function useAsync(loader, deps = [], { auto = true, initial = null } = {}) {
  const [data, setData] = useState(initial)
  const [loading, setLoading] = useState(false)
  const alive = useRef(true)
  const loaderRef = useRef(loader)
  loaderRef.current = loader

  useEffect(() => () => { alive.current = false }, [])

  const run = useCallback(async () => {
    setLoading(true)
    try {
      await whenReady()
      const result = await loaderRef.current()
      if (alive.current) setData(result)
      return result
    } catch (e) {
      return null
    } finally {
      if (alive.current) setLoading(false)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => {
    if (auto) run()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run, auto])

  return { data, loading, reload: run, setData }
}
