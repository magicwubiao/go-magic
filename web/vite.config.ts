import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  build: {
    outDir: '../internal/server/dist',
    emptyOutDir: true,
    minify: 'esbuild',
    chunkSizeWarningLimit: 3000,
    rollupOptions: {
      output: {
        // ⚠️ 这里**不能**用对象形式 `{ 'naive-ui': ['naive-ui'] }`。
        //
        // manualChunks 的对象形式会把列出的模块当成额外入口（Rollup 文档："the listed
        // modules are used as the starting point for the chunk"），而入口模块的导出必须
        // 保留 —— 于是整包 naive-ui（含所有根本没引用的组件）被强行拉进依赖图，
        // tree-shaking 直接失效，那份 naive-ui chunk 恒定 1.34MB 且被 index.html
        // modulepreload，全部压在首屏关键路径上。
        //
        // 改成函数形式：只钉住首屏必用的框架层（稳定缓存），其余第三方依赖交给 Rollup
        // 默认分块算法 —— 按"被几个动态 chunk 共享"决定独立共享块还是并入使用方。
        // 未被引用的 naive-ui 组件、以及只在个别页面用的 highlight.js / marked 这些，
        // 才会真正被摇掉或留在按需 chunk 里。
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (/[\\/]node_modules[\\/](vue|vue-router|pinia|vue-i18n|@vue|@intlify)[\\/]/.test(id)) {
            return 'vendor'
          }
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:5000',
        changeOrigin: true,
      },
    },
  },
})
