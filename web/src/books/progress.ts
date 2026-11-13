import {
	type QueryClient,
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { flushQueue, hasQueued, queueProgress } from "@/offline/queue";

export type ProgressState = components["schemas"]["ProgressState"];
export type Progress = components["schemas"]["ProgressView"];
export type ProgressUpdate = components["schemas"]["ProgressUpdate"];
export type Medium = "ebook" | "audio";

/** Where the signed-in person is in a book, in its text and its audio. */
export function progressQuery(bookId: string) {
	return queryOptions({
		queryKey: ["progress", bookId],
		queryFn: async () =>
			(
				await api.GET("/books/{bookId}/progress", {
					params: { path: { bookId } },
				})
			).data ?? { finishes: 0 },
	});
}

const CLIENT_KEY = "gotome.clientId";

/**
 * The name this browser writes positions under, the same on every visit,
 * so that the server can tell this browser going back from another one
 * that is behind.
 */
export function clientId(): string {
	try {
		const known = localStorage.getItem(CLIENT_KEY);
		if (known) {
			return known;
		}
		const made = crypto.randomUUID();
		localStorage.setItem(CLIENT_KEY, made);
		return made;
	} catch {
		return "browser";
	}
}

export type ProgressSaved = components["schemas"]["ProgressSaved"];

/**
 * Records a position, based on the one this browser last read. When another
 * device wrote a further one since, nothing is written: the answer has saved
 * false and that position, for the player or reader to offer; force writes
 * anyway. keepalive lets the request outlive the page, for the last save as
 * it closes.
 */
export async function saveProgress(
	queryClient: QueryClient,
	bookId: string,
	medium: Medium,
	update: Omit<ProgressUpdate, "clientId" | "basedOn">,
	keepalive = false,
): Promise<ProgressSaved | undefined> {
	// Positions written offline go first: this one is based on theirs.
	if (await hasQueued(bookId, medium)) {
		await flushQueue(queryClient);
	}
	const known = queryClient.getQueryData<ProgressState>(["progress", bookId]);
	const basedOn = known?.[medium]?.updatedAt;
	let answer: ProgressSaved | undefined;
	try {
		if (navigator.onLine === false) {
			throw new TypeError("offline");
		}
		answer = (
			await api.PUT("/books/{bookId}/progress/{medium}", {
				params: { path: { bookId, medium } },
				body: { ...update, clientId: clientId(), basedOn },
				keepalive,
			})
		).data;
	} catch (error) {
		// A TypeError is fetch's way to say no server answered; an ApiError
		// is the server refusing, which is not for the queue.
		if (!(error instanceof TypeError)) {
			throw error;
		}
		await queueProgress({ bookId, medium, update, clientId: clientId(), basedOn });
		return undefined;
	}
	if (answer?.saved && answer.progress) {
		knowProgress(queryClient, bookId, medium, answer.progress);
		// A place may change the book's status: the first makes it reading,
		// the end completed. The book page asks again when it is next shown.
		void queryClient.invalidateQueries({ queryKey: ["book", bookId] });
	}
	return answer;
}

/** Takes a position as the one this browser last read. */
export function knowProgress(queryClient: QueryClient, bookId: string, medium: Medium, progress: Progress) {
	queryClient.setQueryData<ProgressState>(["progress", bookId], (state) => ({
		finishes: state?.finishes ?? 0,
		...state,
		[medium]: progress,
	}));
}

/** saveProgress as a mutation, for a reader of one book. */
export function useSaveProgress(bookId: string, medium: Medium) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: (update: Omit<ProgressUpdate, "clientId" | "basedOn">) =>
			saveProgress(queryClient, bookId, medium, update),
	});
}
