// vitest/config re-exports Vite's defineConfig with the `test` block typed,
// which keeps one config file instead of two that can drift apart.
import { type Plugin, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { copyFileSync, existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
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

// The server writes the path it is served under into these two tags (see
// internal/webui), so one build serves any path: the assets and icons are
// relative to the base element, the app's router and API client read the
// meta tag.
function serverBase(): Plugin {
	return {
		name: "server-base",
		transformIndexHtml() {
			return [
				{ tag: "base", attrs: { href: "/" }, injectTo: "head-prepend" },
				{ tag: "meta", attrs: { name: "gotome-base", content: "/" }, injectTo: "head-prepend" },
			];
		},
	};
}

const webRoot = fileURLToPath(new URL(".", import.meta.url));

interface DemoBook {
	id: string;
	files?: { name: string; source: string }[];
}

// The demo's books are fetched by `make sample-books` into demo/files and
// demo/covers, or are the repository's own fixtures (a file's `source`).
// What exists is copied beside the app and listed in demo/files.json, which
// the app reads at start: a book whose file is missing is shown without one.
function demoAssets(outDir: string): Plugin {
	return {
		name: "demo-assets",
		apply: "build",
		closeBundle() {
			const library = JSON.parse(
				readFileSync(join(webRoot, "src/demo/library.json"), "utf8"),
			) as { books: DemoBook[] };
			const out = join(webRoot, outDir, "demo");
			mkdirSync(join(out, "files"), { recursive: true });
			mkdirSync(join(out, "covers"), { recursive: true });
			const files: Record<string, number> = {};
			const covers: Record<string, string> = {};
			const take = (from: string, to: string) => {
				if (!existsSync(from)) return false;
				copyFileSync(from, to);
				return true;
			};
			for (const book of library.books) {
				for (const file of book.files ?? []) {
					const from = join(webRoot, "..", file.source);
					if (take(from, join(out, "files", file.name))) {
						files[file.name] = statSync(from).size;
					}
				}
				const fetched = join(webRoot, "demo/files", `${book.id}.epub`);
				if (take(fetched, join(out, "files", `${book.id}.epub`))) {
					files[`${book.id}.epub`] = statSync(fetched).size;
				}
				for (const ext of ["webp", "jpg", "png"]) {
					const cover = join(webRoot, "demo/covers", `${book.id}.${ext}`);
					if (take(cover, join(out, "covers", `${book.id}.${ext}`))) {
						covers[book.id] = `${book.id}.${ext}`;
						break;
					}
				}
			}
			writeFileSync(join(out, "files.json"), JSON.stringify({ files, covers }));
			// Pages answers an address it has no file for with 404.html; the app
			// then starts at that address.
			copyFileSync(join(webRoot, outDir, "index.html"), join(webRoot, outDir, "404.html"));
		},
	};
}

export default defineConfig(({ mode }) => ({
	// The demo is a Pages project site; the path is the repository's name.
	base: mode === "demo" ? (process.env.DEMO_BASE ?? "/GoTome/") : "./",
	plugins: [
		react(),
		tailwindcss(),
		foliateWithoutPdf(),
		...(mode === "demo" ? [] : [serverBase()]),
		...(mode === "demo" ? [demoAssets("dist-demo")] : []),
	],
	resolve: {
		alias: {
			"@": fileURLToPath(new URL("./src", import.meta.url)),
			"foliate-js": fileURLToPath(new URL("./vendor/foliate-js", import.meta.url)),
		},
	},
	build: {
		outDir: mode === "demo" ? "dist-demo" : "dist",
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
}));
