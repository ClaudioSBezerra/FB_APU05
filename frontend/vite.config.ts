/// <reference types="vitest" />
import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const target = env.VITE_API_TARGET || "http://localhost:8081";

  console.log(`[Vite] Proxying /api to: ${target}`);

  return {
    server: {
      host: "0.0.0.0",
      port: 3000,
      proxy: {
        "/api": {
          target: target,
          changeOrigin: true,
          secure: false,
        },
      },
    },
    plugins: [
      react(),
    ],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    test: {
      globals: true,
      environment: 'node',
      include: ['src/**/*.{test,spec}.{ts,tsx}'],
    },
  };
});
