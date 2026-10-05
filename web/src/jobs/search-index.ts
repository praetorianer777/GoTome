import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type SearchStatus = components["schemas"]["IndexStatus"];

const STATUS_POLL_MS = 3000;

/** Whether search knows all of the text and its index is built. */
export function searchIsCurrent(status: SearchStatus): boolean {
	return status.indexed >= status.files && !status.rebuilding;
}

/**
 * How much of the books' text search knows, of a library or of all, asked
 * for again while some of it is still being read or the index rebuilt.
 */
export function searchStatusQuery(library?: string) {
	return queryOptions({
		queryKey: ["search", "status", library ?? "all"],
		queryFn: async () =>
			(await api.GET("/search/status", { params: { query: { library } } }))
				.data,
		refetchInterval: (query) =>
			query.state.data && !searchIsCurrent(query.state.data)
				? STATUS_POLL_MS
				: false,
	});
}

function useAfter() {
	const queryClient = useQueryClient();
	return () => {
		queryClient.invalidateQueries({ queryKey: ["search", "status"] });
		queryClient.invalidateQueries({ queryKey: ["jobs"] });
	};
}

/** Reads the text of a book, a library or every book again. */
export function useRereadText() {
	return useMutation({
		mutationFn: async (target: { library?: string; book?: string }) =>
			(await api.POST("/search/reread", { body: target })).data,
		onSuccess: useAfter(),
	});
}

/** Builds the search index again from the text already read. */
export function useRebuildIndex() {
	return useMutation({
		mutationFn: async () => (await api.POST("/search/rebuild")).data,
		onSuccess: useAfter(),
	});
}
