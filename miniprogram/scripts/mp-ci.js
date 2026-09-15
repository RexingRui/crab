#!/usr/bin/env node
/* 用微信官方的 miniprogram-ci 上传 / 预览小程序，取代「开发者工具里点上传」。
 *
 *   node scripts/mp-ci.js upload  [--version 0.1.0] [--desc "..."] [--robot 1]
 *   node scripts/mp-ci.js preview [--page pages/home/index] [--robot 1]
 *
 * 密钥来源（二选一，别写进仓库）：
 *   WX_PRIVATE_KEY       上传密钥内容，CI 里放 secret
 *   WX_PRIVATE_KEY_PATH  上传密钥文件路径，本地调试用
 *
 * 上传的是 dist/，所以跑之前必须先 npm run build:weapp。
 */
const fs = require('fs')
const path = require('path')
const { execSync } = require('child_process')

const ROOT = path.resolve(__dirname, '..')
const pkg = require(path.join(ROOT, 'package.json'))

/* ---------- 参数 ---------- */

function parseArgs(argv) {
  const out = { _: [] }
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i]
    if (arg.startsWith('--')) {
      const [key, inline] = arg.slice(2).split('=')
      if (inline !== undefined) out[key] = inline
      else if (argv[i + 1] && !argv[i + 1].startsWith('--')) { out[key] = argv[i + 1]; i += 1 }
      else out[key] = true
    } else {
      out._.push(arg)
    }
  }
  return out
}

function fail(msg) {
  console.error(`\x1b[31m[mp-ci] ${msg}\x1b[0m`)
  process.exit(1)
}

function info(msg) {
  console.log(`\x1b[36m[mp-ci]\x1b[0m ${msg}`)
}

/* ---------- 取配置 ---------- */

function gitShort() {
  try {
    return execSync('git rev-parse --short HEAD', { cwd: ROOT, stdio: ['ignore', 'pipe', 'ignore'] })
      .toString().trim()
  } catch (e) {
    return 'nogit'
  }
}

function readPrivateKey() {
  const inline = process.env.WX_PRIVATE_KEY
  if (inline && inline.trim()) {
    // secret 走环境变量时换行常被转义成字面量 \n，这里还原回去
    return inline.includes('\\n') && !inline.includes('\n') ? inline.replace(/\\n/g, '\n') : inline
  }
  const keyPath = process.env.WX_PRIVATE_KEY_PATH
  if (keyPath) {
    const abs = path.isAbsolute(keyPath) ? keyPath : path.join(ROOT, keyPath)
    if (!fs.existsSync(abs)) fail(`上传密钥文件不存在：${abs}`)
    return fs.readFileSync(abs, 'utf8')
  }
  fail('缺少上传密钥：设置 WX_PRIVATE_KEY（密钥内容）或 WX_PRIVATE_KEY_PATH（密钥文件路径）。\n' +
       '    密钥在「小程序后台 → 开发管理 → 开发设置 → 小程序代码上传密钥」生成。')
  return ''
}

function readAppid() {
  const fromEnv = process.env.WX_APPID && process.env.WX_APPID.trim()
  if (fromEnv) return fromEnv
  try {
    const conf = JSON.parse(fs.readFileSync(path.join(ROOT, 'project.config.json'), 'utf8'))
    if (conf.appid && conf.appid !== 'touristappid') return conf.appid
  } catch (e) { /* 读不到就按缺失处理 */ }
  fail('缺少 appid：project.config.json 里的 appid 不是真实 AppID（还是 touristappid），也没给 WX_APPID。')
  return ''
}

function resolveVersion(args) {
  const raw = String(args.version || process.env.MP_VERSION || pkg.version || '').trim()
  if (!raw) fail('缺少版本号：--version 或 MP_VERSION。')
  if (!/^\d+(\.\d+){0,3}$/.test(raw)) fail(`版本号只能是数字和点，收到：${raw}`)
  return raw
}

function resolveDesc(args, version) {
  const raw = String(args.desc || process.env.MP_DESC || `${version} @ ${gitShort()}`).trim()
  return raw.length > 120 ? `${raw.slice(0, 117)}...` : raw
}

function resolveRobot(args) {
  const robot = Number(args.robot || process.env.WX_CI_ROBOT || 1)
  if (!Number.isInteger(robot) || robot < 1 || robot > 30) fail(`robot 只能是 1-30 的整数，收到：${args.robot || process.env.WX_CI_ROBOT}`)
  return robot
}

/* ---------- 产物检查 ---------- */

function assertBuilt() {
  const distDir = path.join(ROOT, 'dist')
  const appJson = path.join(distDir, 'app.json')
  if (!fs.existsSync(appJson)) {
    fail('没找到 dist/app.json，先执行：npm run build:weapp')
  }
  const built = fs.statSync(appJson).mtimeMs
  const srcNewer = newestMtime(path.join(ROOT, 'src'))
  if (srcNewer > built) {
    console.warn('\x1b[33m[mp-ci] 警告：src/ 比 dist/ 新，可能在传旧产物。\x1b[0m')
  }
}

function newestMtime(dir) {
  let newest = 0
  const walk = (d) => {
    for (const entry of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, entry.name)
      if (entry.isDirectory()) walk(p)
      else newest = Math.max(newest, fs.statSync(p).mtimeMs)
    }
  }
  try { walk(dir) } catch (e) { /* 读不到就不提醒 */ }
  return newest
}

/* ---------- 主流程 ---------- */

// 上传的是 dist/（project.config.json 里 miniprogramRoot 指向它），
// 其余目录不必扫，扫了只是白等。
const IGNORES = ['node_modules/**/*', 'src/**/*', 'config/**/*', 'scripts/**/*', '.git/**/*', '.temp/**/*']

function createProject(ci) {
  return new ci.Project({
    appid: readAppid(),
    type: 'miniProgram',
    projectPath: ROOT,
    privateKey: readPrivateKey(),
    ignores: IGNORES
  })
}

function onProgressUpdate(task) {
  const msg = typeof task === 'string' ? task : (task && task.message)
  if (msg) process.stdout.write(`  ${msg}\n`)
}

async function main() {
  const args = parseArgs(process.argv.slice(2))
  const cmd = args._[0]

  let ci
  try {
    ci = require('miniprogram-ci')
  } catch (e) {
    fail('缺少依赖 miniprogram-ci，先执行：npm install')
  }

  if (cmd !== 'upload' && cmd !== 'preview') {
    console.error('用法：node scripts/mp-ci.js upload|preview [--version x.y.z] [--desc "..."] [--robot 1]')
    process.exit(1)
  }

  // 先把参数校验完再建 project：省得扫完文件才因为版本号写错退出
  const version = resolveVersion(args)
  const robot = resolveRobot(args)
  assertBuilt()
  const project = createProject(ci)

  if (cmd === 'preview') {
    const qrcodeOutputDest = path.join(ROOT, 'dist', 'preview.jpg')
    info(`生成预览（robot ${robot}）…`)
    await ci.preview({
      project,
      desc: resolveDesc(args, version),
      version,
      robot,
      setting: { useProjectConfig: true },
      qrcodeFormat: 'image',
      qrcodeOutputDest,
      pagePath: args.page || undefined,
      searchQuery: args.query || undefined,
      onProgressUpdate
    })
    info(`预览二维码已写入 ${path.relative(process.cwd(), qrcodeOutputDest)}，用微信扫码打开开发版。`)
    return
  }

  const desc = resolveDesc(args, version)
  info(`上传体验版 ${version}（robot ${robot}）：${desc}`)
  const result = await ci.upload({
    project,
    version,
    desc,
    robot,
    setting: { useProjectConfig: true },
    onProgressUpdate
  })
  if (result && Array.isArray(result.subPackageInfo)) {
    result.subPackageInfo.forEach((p) => {
      info(`包体 ${p.name || '主包'}：${(p.size / 1024).toFixed(1)} KB`)
    })
  }
  info('上传完成。去「小程序后台 → 版本管理」把这一版设为体验版或提交审核。')
}

main().catch((err) => {
  console.error(`\x1b[31m[mp-ci] 失败：${(err && err.message) || err}\x1b[0m`)
  process.exit(1)
})
