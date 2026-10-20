import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

const rootDir = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, rootDir, '')
  const baseApi = env.VITE_BASE_API || '/api'
  const basePath = env.VITE_BASE_PATH || 'http://127.0.0.1'
  const serverPort = env.VITE_SERVER_PORT || '8888'

  return {
    root: rootDir,
    resolve: {
      alias: {
        '@': path.resolve(rootDir, 'src')
      }
    },
    server: {
      host: true,
      port: Number(env.VITE_DEV_PORT || '8898'),
      proxy: {
        [baseApi]: {
          target: `${basePath}:${serverPort}/`,
          changeOrigin: true,
          rewrite: (requestPath) => requestPath.replace(new RegExp(`^${baseApi}`), '')
        }
      }
    },
    plugins: [vue()],
    build: {
      outDir: path.resolve(rootDir, 'dist'),
      emptyOutDir: true,
      target: 'es2015',
      rollupOptions: {
        input: path.resolve(rootDir, 'index.html'),
        output: {
          entryFileNames: 'assets/[name]-[hash].js',
          chunkFileNames: 'assets/[name]-[hash].js',
          assetFileNames: 'assets/[name]-[hash][extname]'
        }
      }
    }
  }
})
