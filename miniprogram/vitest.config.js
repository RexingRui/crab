import { defineConfig } from 'vitest/config'

// utils 里都是纯函数，不碰 Taro API，直接在 node 环境跑
export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/utils/__tests__/**/*.test.js']
  }
})
