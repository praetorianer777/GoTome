import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Recommendation = components["schemas"]["Recommendation"];
export type RecommendationList = components["schemas"]["RecommendationList"];

/** Unread books near what the signed-in person liked, with why. */
export function recommendationsQuery(limit: number) {
	return queryOptions({
		queryKey: ["recommendations", limit],
		queryFn: async () =>
			(await api.GET("/me/recommendations", { params: { query: { limit } } })).data ?? {
				hasProfile: false,
				books: [],
			},
		staleTime: 60_000,
	});
}

/** "Not interested": the book is left out of the person's recommendations. */
export function useDismissRecommendation() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (bookId: string) => {
			await api.POST("/me/recommendations/{bookId}/dismiss", {
				params: { path: { bookId } },
			});
			return bookId;
		},
		onSuccess: (bookId) => {
			queryClient.setQueriesData<RecommendationList>({ queryKey: ["recommendations"] }, (list) =>
				list ? { ...list, books: list.books.filter((r) => r.book.id !== bookId) } : list,
			);
		},
	});
}
