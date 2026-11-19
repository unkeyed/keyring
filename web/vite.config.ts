import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// `npm run dev` serves the UI on :5173 and forwards API calls to the Go
// server from `make dev` on :8080.
const api = "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/v1": api,
      "/auth": api,
      "/mcp": api,
      "/webhooks": api,
      "/healthz": api,
      "/openapi.yaml": api,
    },
  },
});
