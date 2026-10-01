import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";
import type { BookDetail } from "@/books/api";
import type { MessageKey } from "@/i18n";

export type Reading = components["schemas"]["ReadingState"];
export type ReadingChange = components["schemas"]["ReadingChange"];
export type ReadingStatus = Reading["status"];
export type ReadingBulk = components["schemas"]["ReadingBulkRequest"];

export const STATUSES: ReadingStatus[] = [
	"unread",
	"reading",
	"completed",
	"abandoned",
	"wishlist",
];

export const STATUS_LABELS: Record<ReadingStatus, MessageKey> = {
	unread: "status.unread",
	reading: "status.reading",
	completed: "status.completed",
	abandoned: "status.abandoned",
	wishlist: "status.wishlist",
};

export const MAX_RATING = 5;

/** Changes where the signed-in person stands with a book. */
export function useSetReading(id: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (change: ReadingChange) =>
			(
				await api.PUT("/books/{bookId}/reading", {
					params: { path: { bookId: id } },
					body: change,
				})
			).data,
		onSuccess: (reading) => {
			if (reading) {
				queryClient.setQueryData<BookDetail>(["book", id], (book) =>
					book ? { ...book, reading } : book,
				);
			}
			queryClient.invalidateQueries({ queryKey: ["books"] });
		},
	});
}

/** Changes where the signed-in person stands with many books at once. */
export function useSetReadingBulk() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: ReadingBulk) =>
			(await api.POST("/books/reading", { body })).data,
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["books"] });
			queryClient.invalidateQueries({ queryKey: ["book"] });
		},
	});
}
