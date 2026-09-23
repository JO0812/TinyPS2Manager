import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  server: {
    // Local dev talks to a real backend: run `oplbm serve` (default
    // 127.0.0.1:41337) and the Vite dev server proxies /api there.
    proxy: {
      '/api': 'http://127.0.0.1:41337',
    },
  },
})
