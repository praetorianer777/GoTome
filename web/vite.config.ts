// vitest/config re-exports Vite's defineConfig with the `test` block typed,
// which keeps one config file instead of two that can drift apart.
import { type Plugin, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

// foliate-js loads its PDF adapter, and with it a copy of PDF.js, when it is
// handed a PDF. The reader never does, as PDFs have their own, so the adapter
// is not vendored and its import resolves to a module that says so.
function foliateWithoutPdf(): Plugin {
	const stub = "\0foliate-without-pdf";
	return {
		name: "foliate-without-pdf",
		enforce: "pre",
		resolveId(source, importer) {
			return source === "./pdf.js" && importer?.includes("/vendor/foliate-js/") ? stub : null;
		},
		load(id) {
			return id === stub
				? 'export const makePDF = () => { throw new Error("PDFs open in the PDF reader.") }'
				: null;
		},
	};
}

export default defineConfig({
	plugins: [react(), tailwindcss(), foliateWithoutPdf()],
	resolve: {
		alias: {
			"@": fileURLToPath(new URL("./src", import.meta.url)),
			"foliate-js": fileURLToPath(new URL("./vendor/foliate-js", import.meta.url)),
		},
	},
	build: {
		rolldownOptions: {
			output: {
				codeSplitting: {
					// The libraries every page needs, in one file that changes only
					// with them; left to itself, the bundler cuts them into a file per
					// hook the pages share, a request each on the first load.
					groups: [
						{
							name: "libraries",
							test: /node_modules[\\/](react|react-dom|scheduler|@tanstack|openapi-fetch)[\\/]/,
						},
					],
				},
			},
		},
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
