import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// 开发时把 /api 代理到本地 Go 后端，避免跨域。
export default defineConfig({
  plugins: [react()],
  // hls.js 是仅在播放页按需加载的独立引擎包，大小不计入应用首屏。
  build: { chunkSizeWarningLimit: 550 },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:3001",
        changeOrigin: true,
      },
    },
  },
});
