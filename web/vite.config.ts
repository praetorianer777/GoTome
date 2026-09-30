// vitest/config re-exports Vite's defineConfig with the `test` block typed,
// which keeps one config file instead of two that can drift apart.
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
	plugins: [react(), tailwindcss()],
	resolve: {
		alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
	},
	server: {
		host: "0.0.0.0",
		port: 5173,
		// In production the Go binary serves the app and the API from one origin.
		// Proxying the API here gives development the same: the session cookie is
		// first-party and no CORS preflight is involved.
		proxy: {
			"/api": {
				target: process.env.VITE_API_PROXY || "http://localhost:8080",
				changeOrigin: true,
			},
		},
	},
	test: {
		globals: true,
		environment: "jsdom",
		setupFiles: ["./src/test/setup.ts"],
	},
});
