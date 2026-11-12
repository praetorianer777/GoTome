import type { QueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/api/client";
import type { components } from "@/api/schema";
import { store } from "./db";

type Medium = "ebook" | "audio";
type Update = Omit<components["schemas"]["ProgressUpdate"], "clientId" | "basedOn">;

/** A position written while the server could not be reached. */
export interface Queued {
	bookId: string;
	medium: Medium;
	update: Update;
	clientId: string;
	/** The position this browser had last read from the server when it
	 * first wrote offline: the rule that a further place another device
	 * saved since is not overwritten applies as it does online. */
	basedOn?: string;
	queuedAt: string;
}

const keyOf = (bookId: string, medium: Medium) => `${bookId}:${medium}`;

/**
 * Queues a position for later. One is kept per book and medium: the latest,
 * on the original basedOn, as what was written between is no longer a place
 * anyone is at.
 */
export async function queueProgress(item: Omit<Queued, "queuedAt">): Promise<void> {
	const key = keyOf(item.bookId, item.medium);
	const earlier = await store.get<Queued>("queue", key);
	await store.put("queue", key, {
		...item,
		basedOn: earlier ? earlier.basedOn : item.basedOn,
		queuedAt: new Date().toISOString(),
	});
}

export async function queuedPositions(): Promise<Queued[]> {
	return (await store.all<Queued>("queue").catch(() => [])).sort((a, b) =>
		a.queuedAt.localeCompare(b.queuedAt),
	);
}

export async function hasQueued(bookId: string, medium: Medium): Promise<boolean> {
	return (await store.get("queue", keyOf(bookId, medium)).catch(() => undefined)) !== undefined;
}

let flushing: Promise<void> | undefined;

/**
 * Sends the queued positions, oldest first. Stops at the first the network
 * does not take, or the server refuses for want of a session, and keeps the
 * rest; a position the server refuses for another reason is dropped, as
 * sending it again would not change the answer. One the server did not write
 * because another device is further is dropped too: the reader offers that
 * one the next time the book is opened.
 */
export function flushQueue(queryClient: QueryClient): Promise<void> {
	flushing ??= (async () => {
		try {
			for (const item of await queuedPositions()) {
				try {
					await api.PUT("/books/{bookId}/progress/{medium}", {
						params: { path: { bookId: item.bookId, medium: item.medium } },
						body: { ...item.update, clientId: item.clientId, basedOn: item.basedOn },
					});
					void queryClient.invalidateQueries({ queryKey: ["progress", item.bookId] });
					void queryClient.invalidateQueries({ queryKey: ["book", item.bookId] });
				} catch (error) {
					if (!(error instanceof ApiError)) return;
					if (error.status === 401 || error.status === 403 || error.status >= 500) return;
				}
				const key = keyOf(item.bookId, item.medium);
				// Written again while this was on its way: that one is still to send.
				if ((await store.get<Queued>("queue", key))?.queuedAt === item.queuedAt) {
					await store.remove("queue", key);
				}
			}
		} finally {
			flushing = undefined;
		}
	})();
	return flushing;
}
