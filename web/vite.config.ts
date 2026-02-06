import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { writeFileSync } from 'fs'
import { resolve } from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: 'restore-gitkeep',
      writeBundle({ dir }) {
        if (dir) {
          writeFileSync(resolve(dir, '.gitkeep'), '')
        }
      },
    },
  ],
  build: {
    outDir: '../internal/web/static/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
