import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type WishRequest = components["schemas"]["WishRequest"];

export interface WishSearch {
	title?: string;
	author?: string;
	isbn?: string;
}

/** What the providers know by title, author or ISBN. Slow, so asked once. */
export function wishSearchQuery(search: WishSearch) {
	return queryOptions({
		queryKey: ["wish-search", search],
		queryFn: async () =>
			(await api.GET("/metadata/search", { params: { query: search } })).data ?? {
				candidates: [],
				failures: [],
			},
		enabled: Boolean(search.title || search.isbn),
		staleTime: 10 * 60_000,
		retry: false,
	});
}

export function useWish() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (wish: WishRequest) => (await api.POST("/books/wishes", { body: wish })).data,
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["books"] }),
	});
}
