import { defineConfig, loadEnv } from 'vite'
import { fileURLToPath } from 'url'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

export default defineConfig(({ command, mode }) => {
  const isProd = mode === 'production'
  const { VITE_ENV_BASE_API } = loadEnv(mode, process.cwd());
  return {
    plugins: [vue(), AutoImport({
      resolvers: [ElementPlusResolver({ importStyle: 'css' })],
    }),
    Components({
      resolvers: [ElementPlusResolver({ importStyle: 'css' })],
    }),],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url))
      }
    },
    css: {
      preprocessorOptions: {
        scss: {
          api: 'modern-compiler', // sass BANBEN 1.60.0 以上版本才支持
          additionalData: '@use "@/assets/css/whole.scss";'
        }
      }
    },
    build: {
      outDir: 'dist',
      sourcemap: !isProd,
      rollupOptions: {
        maxParallelFileOps: 8,
        output: {
          chunkFileNames: 'static/js/[name]-[hash].js',
          entryFileNames: 'static/js/[name]-[hash].js',
          assetFileNames: 'static/[ext]/[name]-[hash].[ext]',
          manualChunks(id) {
            if (!id.includes('node_modules')) return
            if (id.includes('element-plus')) return 'vendor-element-plus'
            if (id.includes('pdfjs-dist')) return 'vendor-pdf'
            if (id.includes('docx-preview')) return 'vendor-docx'
            return 'vendor'
          },
        },
      },
    },
    // esbuild: {
    //   drop: command === 'build' ? ["console"] : [],
    // },
    // base: command === 'serve' ? '' : "/" + VITE_ENV_BASE_URL + "/",
    base: command === 'serve' ? '' : "./",
    server: {
      proxy: {
        // API 请求保持 /api 前缀
        '/api': {
          target: 'http://localhost:6065',
          changeOrigin: true
        },
        // WebDAV 使用 /dav 前缀
        '/dav': {
          target: 'http://localhost:6065',
          changeOrigin: true
        }
      },
    },
  }
})
