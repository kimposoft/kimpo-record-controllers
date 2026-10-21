import { defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';

export default defineConfig({
  plugins: [vue()],
  resolve: {
    // 强制 vue / naive-ui 只解析出一份，避免共享包各自带一份 Vue / Naive 实例。
    dedupe: ['vue', 'naive-ui']
  },
  test: {
    environment: 'jsdom'
  }
});
