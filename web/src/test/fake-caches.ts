import { vi } from "vitest";

/** The browser's Cache Storage, in memory: enough for what kept books use. */
export function installFakeCaches() {
	const stores = new Map<string, Map<string, Response>>();
	const urlOf = (r: RequestInfo | URL) => (r instanceof Request ? r.url : new URL(String(r), window.location.href).href);
	const open = async (name: string) => {
		const entries = stores.get(name) ?? new Map<string, Response>();
		stores.set(name, entries);
		return {
			put: async (r: RequestInfo | URL, res: Response) => void entries.set(urlOf(r), res.clone()),
			match: async (r: RequestInfo | URL) => entries.get(urlOf(r))?.clone(),
			delete: async (r: RequestInfo | URL) => entries.delete(urlOf(r)),
			keys: async () => [...entries.keys()].map((url) => new Request(url)),
		};
	};
	vi.stubGlobal("caches", {
		open,
		delete: async (name: string) => stores.delete(name),
		has: async (name: string) => stores.has(name),
		keys: async () => [...stores.keys()],
	});
	Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: {} });
	return stores;
}

export function removeFakeServiceWorker() {
	Reflect.deleteProperty(navigator, "serviceWorker");
}
