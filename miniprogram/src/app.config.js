export default defineAppConfig({
  pages: [
    'pages/home/index',
    'pages/record/index',
    'pages/orders/index',
    'pages/detail/index',
    'pages/edit/index',
    'pages/settings/index'
  ],
  window: {
    backgroundTextStyle: 'dark',
    navigationBarBackgroundColor: '#2F4739',
    navigationBarTitleText: '蟹记',
    navigationBarTextStyle: 'white',
    backgroundColor: '#EFF2EE'
  },
  tabBar: {
    color: '#6B7670',
    selectedColor: '#2F4739',
    backgroundColor: '#FFFFFF',
    borderStyle: 'white',
    list: [
      { pagePath: 'pages/home/index', text: '今天' },
      { pagePath: 'pages/record/index', text: '记一笔' },
      { pagePath: 'pages/orders/index', text: '全部' }
    ]
  },
  style: 'v2',
  lazyCodeLoading: 'requiredComponents'
})
