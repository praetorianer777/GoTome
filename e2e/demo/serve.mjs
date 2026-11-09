// Serves web/dist-demo the way GitHub Pages serves a project site: files
// under /GoTome/, and 404.html (with status 404) for an address with no file.
import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../web/dist-demo", import.meta.url));
const base = "/GoTome/";
const port = Number(process.argv[2] ?? 4173);
const types = {
	".html": "text/html",
	".js": "text/javascript",
	".css": "text/css",
	".json": "application/json",
	".svg": "image/svg+xml",
	".png": "image/png",
	".webp": "image/webp",
	".jpg": "image/jpeg",
	".ico": "image/x-icon",
	".mp3": "audio/mpeg",
	".epub": "application/epub+zip",
	".webmanifest": "application/manifest+json",
};

async function find(pathname) {
	if (!pathname.startsWith(base)) return undefined;
	let file = normalize(join(root, pathname.slice(base.length)));
	if (!file.startsWith(root)) return undefined;
	const info = await stat(file).catch(() => undefined);
	if (info?.isDirectory()) file = join(file, "index.html");
	return (await stat(file).catch(() => undefined)) ? file : undefined;
}

createServer(async (req, res) => {
	const pathname = decodeURIComponent(new URL(req.url, "http://x").pathname);
	const file = await find(pathname);
	const status = file ? 200 : 404;
	const body = await readFile(file ?? join(root, "404.html")).catch(() => "not found");
	res.writeHead(status, { "content-type": types[extname(file ?? "x.html")] ?? "application/octet-stream" });
	res.end(body);
}).listen(port, "127.0.0.1");
