// Leaves a gzip and a brotli copy beside each built file that compresses,
// so the server sends them as they are and compresses nothing as it answers
// (backend/internal/webui). Run after vite build.
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { brotliCompressSync, constants, gzipSync } from "node:zlib";

const dist = fileURLToPath(new URL("../dist", import.meta.url));
const compressible = /\.(js|mjs|css|html|svg|json|webmanifest|wasm|txt|ico)$/;
// Below this, a copy saves less than a request's headers.
const smallest = 1024;

function* files(dir) {
	for (const entry of readdirSync(dir, { withFileTypes: true })) {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) yield* files(path);
		else yield path;
	}
}

let plain = 0;
let brotli = 0;
for (const path of files(dist)) {
	if (!compressible.test(path) || statSync(path).size < smallest) continue;
	const data = readFileSync(path);
	const copies = {
		".gz": gzipSync(data, { level: 9 }),
		".br": brotliCompressSync(data, {
			params: {
				[constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY,
				[constants.BROTLI_PARAM_SIZE_HINT]: data.length,
			},
		}),
	};
	for (const [suffix, copy] of Object.entries(copies)) {
		// A copy hardly smaller than the file is not worth a decoder's work.
		if (copy.length < data.length * 0.9) writeFileSync(path + suffix, copy);
	}
	plain += data.length;
	brotli += Math.min(data.length, copies[".br"].length);
}
console.log(`compressed ${(plain / 1024).toFixed(0)} KiB of assets to ${(brotli / 1024).toFixed(0)} KiB (brotli)`);
