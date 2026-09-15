/* 正式域名写在这儿，改域名改这一处即可；这两个域名都要在小程序后台
 * 配成 request 合法域名。想临时指向别的环境有两条路：构建时用
 * TARO_APP_API_BASE_URL=https://other.domain npm run build:weapp 覆盖
 * （见 config/index.js 的 defineConstants），或者在小程序「设置」页改，存本地。 */
export const DEFAULT_BASE_URL = process.env.TARO_APP_API_BASE_URL || 'https://crab-doc.site'
// 买家查单页域名，可在「设置」里改，存本地
export const DEFAULT_TRACK_URL = process.env.TARO_APP_TRACK_URL || 'https://crab-doc.site'
export const VERSION = process.env.TARO_APP_VERSION || '0.1.0'

export const STORAGE_KEYS = {
  token: 'token',
  baseUrl: 'baseUrl',
  trackUrl: 'trackUrl'
}
