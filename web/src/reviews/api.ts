import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type BookReviews = components["schemas"]["BookReviews"];
export type PersonReviews = components["schemas"]["PersonReviews"];

/** What others share about a book, and the person's own review. */
export function bookReviewsQuery(bookId: string) {
	return queryOptions({
		queryKey: ["book-reviews", bookId],
		queryFn: async () =>
			(await api.GET("/books/{bookId}/reviews", { params: { path: { bookId } } })).data,
	});
}

/** The reviews a person shares on the books the signed-in person sees. */
export function personReviewsQuery(username: string) {
	return queryOptions({
		queryKey: ["person-reviews", username],
		queryFn: async () =>
			(await api.GET("/people/{username}/reviews", { params: { path: { username } } }))
				.data,
		retry: false,
	});
}

export const sharingQuery = queryOptions({
	queryKey: ["sharing"],
	queryFn: async () => (await api.GET("/me/sharing")).data,
});

export function useSetSharing() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (shareReviews: boolean) =>
			(await api.PUT("/me/sharing", { body: { shareReviews } })).data,
		onSuccess: (data) => {
			queryClient.setQueryData(sharingQuery.queryKey, data);
			void queryClient.invalidateQueries({ queryKey: ["book-reviews"] });
		},
	});
}

/** Writes the signed-in person's review of a book; "" removes it. */
export function useSetReview(bookId: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (review: { body: string; private: boolean }) => {
			await api.PUT("/books/{bookId}/review", {
				params: { path: { bookId } },
				body: review,
			});
		},
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["book-reviews", bookId] }),
	});
}
