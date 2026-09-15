const path = require('path')

/* 构建期注入的环境变量：部署时把真实域名传进来，源码里不写死。
 * 用法：TARO_APP_API_BASE_URL=https://your.domain npm run build:weapp
 * 没传就退回 src/utils/config.js 里的占位域名，本地开发照常。 */
const envConstants = ['TARO_APP_API_BASE_URL', 'TARO_APP_TRACK_URL'].reduce((acc, key) => {
  acc[`process.env.${key}`] = JSON.stringify(process.env[key] || '')
  return acc
}, {})

/* 设置页显示的版本号与上传体验版的版本号同一个来源：package.json */
envConstants['process.env.TARO_APP_VERSION'] =
  JSON.stringify(process.env.MP_VERSION || require('../package.json').version)

const config = {
  projectName: 'crab-note',
  date: '2026-9-14',
  designWidth: 750,
  deviceRatio: { 640: 2.34 / 2, 750: 1, 375: 2, 828: 1.81 / 2 },
  sourceRoot: 'src',
  outputRoot: 'dist',
  plugins: [],
  defineConstants: envConstants,
  copy: { patterns: [], options: {} },
  framework: 'react',
  compiler: { type: 'webpack5', prebundle: { enable: false } },
  cache: { enable: false },
  alias: {
    '@': path.resolve(__dirname, '..', 'src')
  },
  mini: {
    postcss: {
      pxtransform: { enable: true, config: {} },
      cssModules: { enable: false }
    }
  },
  h5: {
    publicPath: '/',
    staticDirectory: 'static',
    output: {
      filename: 'js/[name].[hash:8].js',
      chunkFilename: 'js/[name].[chunkhash:8].js'
    },
    miniCssExtractPluginOption: {
      ignoreOrder: true,
      filename: 'css/[name].[hash].css',
      chunkFilename: 'css/[name].[chunkhash].css'
    },
    postcss: {
      autoprefixer: { enable: true, config: {} },
      cssModules: { enable: false }
    }
  }
}

module.exports = function (merge) {
  if (process.env.NODE_ENV === 'development') {
    return merge({}, config, require('./dev'))
  }
  return merge({}, config, require('./prod'))
}
