import {
	infiniteQueryOptions,
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { API_BASE, api, ApiError } from "@/api/client";
import type { components, paths } from "@/api/schema";
import type { BookDetail } from "@/books/api";

export type HistoryEntry = components["schemas"]["HistoryEntry"];
export type ReadingImport = components["schemas"]["ReadingImport"];

type HistoryQuery = NonNullable<paths["/me/reading"]["get"]["parameters"]["query"]>;

export type HistoryFilter = Pick<HistoryQuery, "status" | "year" | "stars" | "q">;

/** Everything the signed-in person read, owned or not, a page at a time. */
export function historyQuery(filter: HistoryFilter) {
	return infiniteQueryOptions({
		queryKey: ["reading-history", filter],
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/me/reading", {
					params: { query: { ...filter, after: pageParam || undefined } },
				})
			).data ?? { entries: [], years: [] },
		initialPageParam: "",
		getNextPageParam: (last) => last.nextCursor || undefined,
	});
}

/** Writes the signed-in person's private note on a book; "" removes it. */
export function useSetNote(id: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (note: string) => {
			await api.PUT("/books/{bookId}/note", {
				params: { path: { bookId: id } },
				body: { note },
			});
			return note;
		},
		onSuccess: (note) => {
			queryClient.setQueryData<BookDetail>(["book", id], (book) =>
				book
					? { ...book, reading: { ...book.reading, note: note || undefined } }
					: book,
			);
		},
	});
}

/**
 * Sends an export for a preview. Like an upload this is not through the typed
 * client, which has no multipart bodies.
 */
export function previewImport(file: File, library?: string): Promise<ReadingImport> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open("POST", new URL(`${API_BASE}/me/reading-imports`, window.location.origin).toString());
		xhr.withCredentials = true;
		xhr.responseType = "text";
		xhr.onload = () => {
			let body: unknown;
			try {
				body = JSON.parse(xhr.responseText);
			} catch {
				body = undefined;
			}
			if (xhr.status >= 200 && xhr.status < 300) {
				resolve(body as ReadingImport);
				return;
			}
			const error = (body as { error?: components["schemas"]["APIError"] } | undefined)?.error;
			reject(
				new ApiError(xhr.status, error ?? { code: "error", message: `The server answered ${xhr.status}.` }),
			);
		};
		xhr.onerror = () => reject(new TypeError("Failed to upload"));
		const form = new FormData();
		if (library) {
			form.append("library", library);
		}
		form.append("file", file);
		xhr.send(form);
	});
}

/** An import: its preview, then, while it runs, what it has come to. */
export function importQuery(id: string | undefined) {
	return queryOptions({
		queryKey: ["reading-import", id],
		queryFn: async () =>
			(
				await api.GET("/me/reading-imports/{importId}", {
					params: { path: { importId: id ?? "" } },
				})
			).data,
		enabled: Boolean(id),
		refetchInterval: (query) => (query.state.data?.state === "running" ? 1000 : false),
	});
}

export function useChooseRow(id: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (v: { row: number; book?: string; skip?: boolean }) =>
			api.PUT("/me/reading-imports/{importId}/rows/{row}", {
				params: { path: { importId: id, row: String(v.row) } },
				body: { book: v.book, skip: v.skip },
			}),
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["reading-import", id] }),
	});
}

export function useStartImport(id: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (overwrite: boolean) =>
			(
				await api.POST("/me/reading-imports/{importId}/start", {
					params: { path: { importId: id } },
					body: { overwrite },
				})
			).data,
		onSuccess: (started) => {
			queryClient.setQueryData(["reading-import", id], started);
			queryClient.invalidateQueries({ queryKey: ["reading-history"] });
			queryClient.invalidateQueries({ queryKey: ["books"] });
			queryClient.invalidateQueries({ queryKey: ["stats"] });
		},
	});
}
