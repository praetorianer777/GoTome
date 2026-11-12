import { queryOptions } from "@tanstack/react-query";
import { type BookDetail, coverUrl, downloadUrl } from "@/books/api";
import { assetUrl } from "@/api/client";
import { FILES_CACHE, META_CACHE } from "./names";
import { forgetAnswer, forgetMetaUrls, rememberAnswer, rememberedAnswers } from "./fetch";
import { store } from "./db";
import { offlineSupported } from "./support";
import { warmApp } from "./worker";

export type BookFile = BookDetail["files"][number];

/** A file kept in this browser. */
export interface KeptFile {
	fileId: string;
	bookId: string;
	title: string;
	authors: string;
	name: string;
	format: string;
	kind: string;
	size: number;
	keptAt: string;
}

const absolute = (path: string) => new URL(assetUrl(path), window.location.origin).href;

/** The addresses the readers and the player ask for a book, which are kept
 * with it: nothing else about the book, and nothing of other books. */
export function bookAnswers(book: BookDetail): string[] {
	const urls = [
		absolute(`/books/${book.id}`),
		absolute(`/books/${book.id}/progress`),
	];
	if (book.files.some((f) => f.kind === "audio")) {
		urls.push(absolute(`/books/${book.id}/audio`));
	}
	for (const size of ["small", "large"] as const) {
		const cover = coverUrl(book, size);
		if (cover) urls.push(new URL(cover, window.location.origin).href);
	}
	return urls;
}

const SESSION_ANSWERS = () => [absolute("/auth/me"), absolute("/setup")];

export const keptQuery = queryOptions({
	queryKey: ["offline", "kept"],
	queryFn: listKept,
});

export async function listKept(): Promise<KeptFile[]> {
	const all = await store.all<KeptFile>("kept").catch(() => []);
	return all.sort((a, b) => b.keptAt.localeCompare(a.keptAt));
}

let claiming: Promise<void> = Promise.resolve();

/**
 * Notes whose account the kept books belong to. When another account is
 * the one signed in, what the last one kept goes: it was fetched with their
 * rights.
 */
export function claimOffline(userId: string): Promise<void> {
	claiming = claiming.then(async () => {
		try {
			const owner = await store.get<string>("owner", "user");
			if (owner !== undefined && owner !== userId) await clearOffline();
			if (owner !== userId) await store.put("owner", "user", userId);
		} catch {
			// Storage this browser refuses holds nothing to clear.
		}
	});
	return claiming;
}

/** Keeps a file with what the readers need to open it. */
export async function keepFile(book: BookDetail, file: BookFile): Promise<KeptFile> {
	await claiming;
	void navigator.storage?.persist?.().catch(() => {});
	for (const url of [...SESSION_ANSWERS(), ...bookAnswers(book)]) {
		await rememberAnswer(url);
	}
	const url = new URL(downloadUrl(file), window.location.origin).href;
	const cache = await caches.open(FILES_CACHE);
	const res = await fetch(url, { credentials: "same-origin" });
	if (!res.ok) throw new Error(`${url}: ${res.status}`);
	await cache.put(url, res);
	const entry: KeptFile = {
		fileId: file.id,
		bookId: book.id,
		title: book.title,
		authors: book.contributors
			.filter((c) => c.role === "author")
			.map((c) => c.name)
			.join(", "),
		name: file.name,
		format: file.format,
		kind: file.kind,
		size: file.size,
		keptAt: new Date().toISOString(),
	};
	await store.put("kept", file.id, entry);
	await warmApp().catch(() => {});
	return entry;
}

/** Takes a kept file away, and with the last of a book's its answers. */
export async function removeKept(fileId: string): Promise<void> {
	const entry = await store.get<KeptFile>("kept", fileId);
	if (!entry) return;
	await (await caches.open(FILES_CACHE)).delete(
		new URL(downloadUrl({ id: fileId }), window.location.origin).href,
	);
	await store.remove("kept", fileId);
	const left = await listKept();
	if (!left.some((k) => k.bookId === entry.bookId)) {
		const prefix = absolute(`/books/${entry.bookId}`);
		for (const url of await rememberedAnswers()) {
			if (url === prefix || url.startsWith(`${prefix}/`)) await forgetAnswer(url);
		}
	}
	if (left.length === 0) {
		for (const url of SESSION_ANSWERS()) await forgetAnswer(url);
	}
}

/** Forgets everything kept: files, answers and positions not yet sent. */
export async function clearOffline(): Promise<void> {
	if (offlineSupported()) {
		await Promise.all([caches.delete(FILES_CACHE), caches.delete(META_CACHE)]).catch(() => {});
	}
	forgetMetaUrls();
	await Promise.all([store.clear("kept"), store.clear("queue")]).catch(() => {});
}

export async function storageEstimate(): Promise<{ used: number; quota: number } | undefined> {
	const e = await navigator.storage?.estimate?.().catch(() => undefined);
	return e?.usage !== undefined && e.quota !== undefined
		? { used: e.usage, quota: e.quota }
		: undefined;
}
