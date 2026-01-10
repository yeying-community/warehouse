import { defineConfig, loadEnv } from 'vite'
import { fileURLToPath } from 'url';
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

export default defineConfig(({ command, mode }) => {
  const { VITE_ENV_BASE_API, VITE_ENV_BASE_URL } = loadEnv(mode, process.cwd());
  return {
    plugins: [vue(), AutoImport({
      resolvers: [ElementPlusResolver()],
    }),
    Components({
      resolvers: [ElementPlusResolver()],
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
      outDir: VITE_ENV_BASE_URL,
      sourcemap: true,
      rollupOptions: {
        output: {
          chunkFileNames: 'static/js/[name]-[hash].js',
          entryFileNames: 'static/js/[name]-[hash].js',
          assetFileNames: 'static/[ext]/[name]-[hash].[ext]',
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
        // API 请求保持 /api 前缀（必须在前面）
        '/api/v1': {
          target: 'http://localhost:6065',
          changeOrigin: true
        },
        // WebDAV 请求去掉 /api 前缀
        '/api': {
          target: 'http://localhost:6065',
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/api/, '')
        }
      },
    },
  }
})