import { BASE_PATH } from "@/base";

/** What is kept in IndexedDB: the files kept, positions waiting to be sent,
 * and whose account the kept things belong to. */
export type StoreName = "kept" | "queue" | "owner";
const STORES: StoreName[] = ["kept", "queue", "owner"];

interface Backend {
	all(store: StoreName): Promise<unknown[]>;
	get(store: StoreName, key: string): Promise<unknown>;
	put(store: StoreName, key: string, value: unknown): Promise<void>;
	remove(store: StoreName, key: string): Promise<void>;
	clear(store: StoreName): Promise<void>;
}

function memory(): Backend {
	const data = new Map<StoreName, Map<string, unknown>>(STORES.map((s) => [s, new Map()]));
	const of = (s: StoreName) => data.get(s) as Map<string, unknown>;
	return {
		all: async (s) => [...of(s).values()],
		get: async (s, k) => of(s).get(k),
		put: async (s, k, v) => void of(s).set(k, v),
		remove: async (s, k) => void of(s).delete(k),
		clear: async (s) => of(s).clear(),
	};
}

function request<T>(r: IDBRequest<T>): Promise<T> {
	return new Promise((resolve, reject) => {
		r.onsuccess = () => resolve(r.result);
		r.onerror = () => reject(r.error);
	});
}

function indexed(): Backend {
	let opened: Promise<IDBDatabase> | undefined;
	const open = () => {
		opened ??= new Promise<IDBDatabase>((resolve, reject) => {
			const r = indexedDB.open(`gotome:${BASE_PATH}`, 1);
			r.onupgradeneeded = () => {
				for (const s of STORES) r.result.createObjectStore(s);
			};
			r.onsuccess = () => resolve(r.result);
			r.onerror = () => reject(r.error);
		});
		return opened;
	};
	const store = async (s: StoreName, mode: IDBTransactionMode) =>
		(await open()).transaction(s, mode).objectStore(s);
	return {
		all: async (s) => request((await store(s, "readonly")).getAll()),
		get: async (s, k) => request((await store(s, "readonly")).get(k)),
		put: async (s, k, v) => void (await request((await store(s, "readwrite")).put(v, k))),
		remove: async (s, k) => void (await request((await store(s, "readwrite")).delete(k))),
		clear: async (s) => void (await request((await store(s, "readwrite")).clear())),
	};
}

let backend: Backend | undefined;

function db(): Backend {
	backend ??= typeof indexedDB === "undefined" ? memory() : indexed();
	return backend;
}

/** Starts from nothing; for tests, which keep it in memory. */
export function resetDb() {
	backend = memory();
}

export const store = {
	all: <T>(s: StoreName) => db().all(s) as Promise<T[]>,
	get: <T>(s: StoreName, key: string) => db().get(s, key) as Promise<T | undefined>,
	put: (s: StoreName, key: string, value: unknown) => db().put(s, key, value),
	remove: (s: StoreName, key: string) => db().remove(s, key),
	clear: (s: StoreName) => db().clear(s),
};
