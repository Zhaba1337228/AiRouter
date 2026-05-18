import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
// NOTE: base is intentionally kept as '/' so Vite outputs assets at /assets/...
// The secret URL prefix (VITE_APP_BASE) is used only as BrowserRouter's basename
// in App.tsx and as the nginx location prefix — not as a Vite build base.
export default defineConfig({
  plugins: [react()],
})
