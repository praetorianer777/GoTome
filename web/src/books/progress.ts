import { queryOptions } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type ProgressState = components["schemas"]["ProgressState"];
export type Progress = components["schemas"]["ProgressView"];

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
