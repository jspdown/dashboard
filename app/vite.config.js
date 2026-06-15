import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Where vite proxies /api requests during `npm run dev`. The default points
// at the standard `dashboard serve` port; set DASHBOARD_API_URL to retarget
// (e.g. at `dashboard-mock` on :18080 when capturing PR screenshots).
const apiUrl = process.env.DASHBOARD_API_URL || 'http://localhost:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': apiUrl,
    },
  },
})
