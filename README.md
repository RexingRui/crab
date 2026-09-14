# 蟹记

大闸蟹订单记录工具。三个前端：

| 目录 | 是什么 | 谁用 |
|---|---|---|
| `miniprogram/` | 小程序「蟹记」，Taro 4 + React | 只有你 |
| `web/track/` | 买家查单页，单个静态 HTML | 买家，微信里发链接 |
| `web/admin/` | Web 后台，React + Vite + Ant Design | 只有你 |

后端另做，前端依赖的接口约定写在 [`docs/frontend-api.md`](docs/frontend-api.md)。

## 跑起来

```bash
# 小程序：编译后用微信开发者工具打开 miniprogram/ 目录
cd miniprogram
npm install
npm run dev:weapp        # 或 npm run build:weapp
npm test                 # 地址解析与金额换算的单测

# Web 后台
cd web/admin
npm install
npm run dev              # http://localhost:5173/admin/，/api 代理到 127.0.0.1:8080
npm run build            # → web/admin/dist

# 买家查单页：没有构建步骤，直接就是 web/track/index.html
```

接口域名在 `miniprogram/src/utils/config.js` 里改，也可以在小程序「设置」页临时改，
存本地。小程序后台记得配 request 合法域名。

## 界面上的几条规矩

界面是一本秋天的账本，安静、克制，只有需要你动手的地方才亮起来：

- 主色**蟹壳青** `#2F4739`，待办用**金爪**金 `#C8A227`，逾期和危险操作才用**熟蟹红** `#D2542A`。
- **已完成的状态一律用灰**。这是整套界面唯一的信息层级，别到处上色把它稀释掉。
  取消掉的单连收款状态也一并淡化——没有要追的钱，就不该用告警色喊人。
- 金额、数量、单号一律 `tabular-nums` 且右对齐。这不是装饰：列表里几十行金额
  要能竖着对齐扫读，对账时差一位就看出来。
- 动效只在**状态变更时**出现，不做入场动画，并且尊重 `prefers-reduced-motion`。

图表的颜色不能直接照搬品牌色：蟹壳青明度 0.374、彩度 0.038，画到图上又暗又灰。
统计页用的是同色相里能过色觉校验的一档 `#2E7D52` / `#A6851C`（色盲分离度、明度带、
彩度下限、对比度都过），见 `web/admin/src/utils/charts.js`。

## 金额

**所有金额在前端都是「分」为单位的整数**，分转元用整数运算，禁止 `parseFloat` 之后再乘除。
页面上算出来的「应收」只是展示用的预估值，**最终金额以后端返回为准**。

## 小程序的几条硬约束

个人主体小程序 + 笔记类目，设计时直接绕开了这些：

- 全站不出现下单、购买、购物车、支付、收款码这类电商词汇，**包括变量名和注释**（审核会看包体）。
- 没有 `web-view`，所以买家链接只能复制文本让用户去微信粘贴，代码里不写跳转。
- 没有微信支付、没有 `getPhoneNumber`。

### 审核演示模式

审核员的 openid 不在管理员白名单里，直接提审会看到一片空白，必然被驳回。所以
登录返回 `40300` 时小程序自动进入**只读演示模式**：用 `src/static/demo.json` 的本地
假数据渲染页面，顶部挂一条「演示数据 · 登录后记录你自己的内容」，所有写操作提示
「演示模式下不能修改」。假数据用的是泛化内容和公开地标，不含真实手机号。

`demo.json` 里存的是**相对天数**，用的时候才落成具体日期——不管哪天提审，
「今天要发的」都还是今天。

## 部署

两个 web 产物交给 Go 用 `embed.FS` 打进二进制：

```go
//go:embed all:../../web/admin/dist
var adminFS embed.FS
//go:embed ../../web/track/index.html
var trackHTML []byte
```

挂载 `/admin/*`（SPA，404 回落 `index.html`）和 `/t`（响应头加 `X-Robots-Tag: noindex`），
静态资源不走 Auth 中间件。后台的 `vite.config.js` 里 `base` 已经设成 `/admin/`。
