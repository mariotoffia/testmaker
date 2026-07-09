/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";

// The production build emits into the Go embed directory, so `make webui`
// followed by `go build` ships the SPA inside the binary (ADR-0005). The dev
// server proxies /api to a locally running `testmaker -serve` so the two-
// terminal dev loop (make serve + make webui-dev) needs no CORS.
export default defineConfig({
  plugins: [react(), tailwind()],
  build: {
    outDir: "../cmd/testmaker/webui/dist",
    emptyOutDir: true, // wipes dist (incl. .keep — the Makefile touches it back)
  },
  server: {
    port: 5173,
    proxy: { "/api": "http://localhost:8080" },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
  },
});
