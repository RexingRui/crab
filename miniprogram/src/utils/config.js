/* 域名优先取构建期注入的环境变量（见 config/index.js 的 defineConstants），
 * 没注入就用下面的占位值；用户还可以在「设置」页临时改，存本地。
 * 部署时：TARO_APP_API_BASE_URL=https://your.domain npm run build:weapp
 * 域名同样要在小程序后台配成 request 合法域名。 */
export const DEFAULT_BASE_URL = process.env.TARO_APP_API_BASE_URL || 'https://example.com'
// 买家查单页域名，可在「设置」里改，存本地
export const DEFAULT_TRACK_URL = process.env.TARO_APP_TRACK_URL || 'https://example.com'
export const VERSION = process.env.TARO_APP_VERSION || '0.1.0'

export const STORAGE_KEYS = {
  token: 'token',
  baseUrl: 'baseUrl',
  trackUrl: 'trackUrl'
}
