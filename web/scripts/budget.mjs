// Fails when the app's first load grows past the budget (#378). The first
// load is what index.html names: the entry script, the chunks it preloads
// and the stylesheet. Sizes are of the files vite wrote, compressed here the
// way compress.mjs does, so the numbers do not depend on its output. Run
// after vite build; the limits are in docs/performance.md.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { brotliCompressSync, constants, gzipSync } from "node:zlib";

const dist = fileURLToPath(new URL("../dist", import.meta.url));

// Raised only on purpose, with the table in docs/performance.md. Today:
// main chunk 148 KiB raw, first load 218 KiB gzip and 185 KiB brotli.
const LIMITS = {
	mainRaw: 165 * 1024,
	firstLoadGzip: 245 * 1024,
	firstLoadBrotli: 210 * 1024,
};

const html = readFileSync(`${dist}/index.html`, "utf8");
const named = [
	...html.matchAll(/<script[^>]*\ssrc="(\/assets\/[^"]+)"/g),
	...html.matchAll(/<link[^>]*rel="modulepreload"[^>]*\shref="(\/assets\/[^"]+)"/g),
	...html.matchAll(/<link[^>]*rel="stylesheet"[^>]*\shref="(\/assets\/[^"]+)"/g),
].map((m) => m[1]);
const main = named[0];
if (!main) {
	console.error("budget: index.html names no entry script");
	process.exit(1);
}

let gzip = 0;
let brotli = 0;
let mainRaw = 0;
for (const path of new Set(named)) {
	const data = readFileSync(dist + path);
	if (path === main) mainRaw = data.length;
	gzip += gzipSync(data, { level: 9 }).length;
	brotli += brotliCompressSync(data, {
		params: { [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY },
	}).length;
}

const kib = (n) => `${(n / 1024).toFixed(1)} KiB`;
const rows = [
	["main chunk", mainRaw, LIMITS.mainRaw],
	["first load, gzip", gzip, LIMITS.firstLoadGzip],
	["first load, brotli", brotli, LIMITS.firstLoadBrotli],
];
let failed = false;
for (const [name, size, limit] of rows) {
	const over = size > limit;
	failed ||= over;
	console.log(`budget: ${name} ${kib(size)} of ${kib(limit)}${over ? " OVER" : ""}`);
}
if (failed) {
	console.error("budget: the first load grew past its limit (docs/performance.md)");
	process.exit(1);
}
