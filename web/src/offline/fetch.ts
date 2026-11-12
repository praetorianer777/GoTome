import { META_CACHE } from "./names";
import { offlineSupported } from "./support";

let urls: Promise<Set<string>> | undefined;

/** The addresses whose answers are kept for books kept offline. */
function metaUrls(): Promise<Set<string>> {
	urls ??= caches
		.open(META_CACHE)
		.then((c) => c.keys())
		.then((keys) => new Set(keys.map((k) => k.url)));
	return urls;
}

/** Forgets what was read of the cache; after it was changed from outside. */
export function forgetMetaUrls() {
	urls = undefined;
}

/** Keeps the answer to a GET for offline use, and keeps it up to date. */
export async function rememberAnswer(url: string): Promise<void> {
	const res = await fetch(url, { credentials: "same-origin" });
	if (!res.ok) throw new Error(`${url}: ${res.status}`);
	await (await caches.open(META_CACHE)).put(url, res);
	(await metaUrls()).add(url);
}

export async function forgetAnswer(url: string): Promise<void> {
	await (await caches.open(META_CACHE)).delete(url);
	(await metaUrls()).delete(url);
}

/** Every address with an answer kept. */
export async function rememberedAnswers(): Promise<string[]> {
	return [...(await metaUrls())];
}

/**
 * The API client's fetch. A GET whose answer was kept for a book kept offline
 * is renewed when the network answers and read from the copy when it does
 * not; nothing else is ever kept, and anything else goes as it is. Done here
 * rather than in the service worker so that the worker never sees an API
 * answer it was not asked to keep.
 */
export async function offlineFetch(request: Request): Promise<Response> {
	if (request.method !== "GET" || !offlineSupported()) {
		return globalThis.fetch(request);
	}
	const known = await metaUrls().catch(() => new Set<string>());
	if (!known.has(request.url)) {
		return globalThis.fetch(request);
	}
	const cache = await caches.open(META_CACHE);
	let res: Response;
	try {
		res = await globalThis.fetch(request);
	} catch (error) {
		const copy = await cache.match(request.url);
		if (copy) return copy;
		throw error;
	}
	if (res.ok) {
		await cache.put(request.url, res.clone());
	} else if (res.status === 401) {
		await cache.delete(request.url);
		known.delete(request.url);
	}
	return res;
}
