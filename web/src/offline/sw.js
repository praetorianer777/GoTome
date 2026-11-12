// The service worker (#410). It is plain JavaScript with no imports, so the
// build can write it as one file at the root of the app (vite.config.ts fills
// in the three constants), where its scope is the path the app is served
// under. It keeps the app's own files and serves the copies of books a person
// kept offline; it never answers API requests, which the page does itself
// (offline/fetch.ts), so nothing here sees a response with personal data
// except the files and covers the person asked to keep.

const BUILD = "__BUILD__";
const CORE = __CORE__;
const ALL = __ALL__;

const SCOPE = self.registration.scope;
const SCOPE_PATH = new URL(SCOPE).pathname;
const API_PATH = `${SCOPE_PATH}api/v1/`;
const SHELL_PREFIX = `gotome-shell:${SCOPE}:`;
const SHELL = `${SHELL_PREFIX}${BUILD}`;
const FILES = `gotome-kept:${SCOPE}:files`;
const META = `gotome-kept:${SCOPE}:meta`;
const STATIC = [
	"manifest.webmanifest",
	"icon-192.png",
	"icon-512.png",
	"apple-touch-icon.png",
	"favicon.ico",
].map((p) => SCOPE_PATH + p);

const DOWNLOAD = new RegExp(`^${API_PATH}files/[^/]+/download$`);
const COVER = new RegExp(`^${API_PATH}books/[^/]+/covers/[^/]+$`);

self.addEventListener("install", (event) => {
	event.waitUntil(
		(async () => {
			const cache = await caches.open(SHELL);
			await cache.addAll([SCOPE, ...STATIC, ...CORE.map((p) => new URL(p, SCOPE).href)]);
			await self.skipWaiting();
		})(),
	);
});

self.addEventListener("activate", (event) => {
	event.waitUntil(
		(async () => {
			for (const name of await caches.keys()) {
				if (name.startsWith(SHELL_PREFIX) && name !== SHELL) await caches.delete(name);
			}
			await self.clients.claim();
		})(),
	);
});

// The page asks for the rest of the app's files when a book is kept, so that
// the readers, whose code loads with the first book opened, open offline too.
self.addEventListener("message", (event) => {
	if (event.data?.type !== "warm") return;
	const reply = (ok) => event.ports[0]?.postMessage(ok);
	event.waitUntil(
		warm().then(
			() => reply(true),
			() => reply(false),
		),
	);
});

async function warm() {
	const cache = await caches.open(SHELL);
	for (const path of ALL) {
		const url = new URL(path, SCOPE).href;
		if (await cache.match(url, { ignoreVary: true })) continue;
		const res = await fetch(url);
		if (!res.ok) throw new Error(`${url}: ${res.status}`);
		await cache.put(url, res);
	}
}

self.addEventListener("fetch", (event) => {
	const request = event.request;
	if (request.method !== "GET") return;
	const url = new URL(request.url);
	if (url.origin !== self.location.origin) return;
	if (request.mode === "navigate") {
		if (url.pathname.startsWith(API_PATH)) return;
		event.respondWith(navigation(request));
	} else if (DOWNLOAD.test(url.pathname)) {
		event.respondWith(keptFile(request));
	} else if (COVER.test(url.pathname)) {
		event.respondWith(keptCover(request));
	} else if (url.pathname.startsWith(`${SCOPE_PATH}assets/`)) {
		event.respondWith(assetFirst(request));
	} else if (STATIC.includes(url.pathname)) {
		event.respondWith(networkFirst(request));
	}
});

async function shellMatch(request) {
	const cache = await caches.open(SHELL);
	return cache.match(request, { ignoreVary: true, ignoreSearch: false });
}

// The page is the same for every address; a new build is fetched when its
// worker installs, so the copy here is the build the assets belong to.
async function navigation(request) {
	try {
		return await fetch(request);
	} catch (error) {
		const shell = await shellMatch(SCOPE);
		if (shell) return shell;
		throw error;
	}
}

async function assetFirst(request) {
	const hit = await shellMatch(request);
	if (hit) return hit;
	const res = await fetch(request);
	if (res.ok) {
		const cache = await caches.open(SHELL);
		await cache.put(request, res.clone());
	}
	return res;
}

async function networkFirst(request) {
	const cache = await caches.open(SHELL);
	try {
		const res = await fetch(request);
		if (res.ok) await cache.put(request, res.clone());
		return res;
	} catch (error) {
		const hit = await cache.match(request, { ignoreVary: true });
		if (hit) return hit;
		throw error;
	}
}

async function keptCover(request) {
	const kept = await caches.open(META);
	const hit = await kept.match(request.url, { ignoreVary: true });
	return hit ?? fetch(request);
}

// A kept file answers ranges itself: PDF.js and the audio element ask for
// parts, and the Cache API stores whole responses only.
async function keptFile(request) {
	const kept = await caches.open(FILES);
	const hit = await kept.match(request.url, { ignoreVary: true });
	if (!hit) return fetch(request);
	const range = request.headers.get("range");
	if (!range) return hit;
	const blob = await hit.blob();
	const size = blob.size;
	const m = /^bytes=(\d*)-(\d*)$/.exec(range.trim());
	if (!m || (m[1] === "" && m[2] === "")) return hit;
	let start;
	let end;
	if (m[1] === "") {
		start = Math.max(size - Number(m[2]), 0);
		end = size - 1;
	} else {
		start = Number(m[1]);
		end = m[2] === "" ? size - 1 : Math.min(Number(m[2]), size - 1);
	}
	if (start >= size || start > end) {
		return new Response(null, {
			status: 416,
			headers: { "Content-Range": `bytes */${size}` },
		});
	}
	return new Response(blob.slice(start, end + 1), {
		status: 206,
		headers: {
			"Content-Type": hit.headers.get("content-type") ?? "application/octet-stream",
			"Content-Range": `bytes ${start}-${end}/${size}`,
			"Content-Length": String(end - start + 1),
			"Accept-Ranges": "bytes",
		},
	});
}
