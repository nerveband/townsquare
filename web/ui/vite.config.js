import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [svelte()],
  build: { outDir: '../dist', emptyOutDir: true },
  server: { proxy: { '/api': process.env.TOWNSQUARE_API || 'http://127.0.0.1:8890' } },
})
