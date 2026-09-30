import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { API_BASE, api } from "@/api/client";
import type { components, paths } from "@/api/schema";

export type BookSummary = components["schemas"]["BookSummary"];
export type BookDetail = components["schemas"]["BookDetail"];
export type BookFile = components["schemas"]["BookFile"];

type ListQuery = NonNullable<paths["/books"]["get"]["parameters"]["query"]>;
export type BookSort = NonNullable<ListQuery["sort"]>;
export type BookOrder = NonNullable<ListQuery["order"]>;

export interface BookListParams {
	library?: string;
	sort: BookSort;
	order: BookOrder;
}

const PAGE_SIZE = 60;

/**
 * The books of one library or all, a page at a time. The server cuts pages by
 * the last book's sort keys, so asking for the next page is handing back the
 * cursor it gave.
 */
export function booksQuery(params: BookListParams, refetchInterval: number | false = false) {
	return infiniteQueryOptions({
		queryKey: ["books", params],
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/books", {
					params: {
						query: {
							...params,
							limit: PAGE_SIZE,
							cursor: pageParam || undefined,
						},
					},
				})
			).data ?? { books: [] },
		initialPageParam: "",
		getNextPageParam: (last) => last.nextCursor || undefined,
		refetchInterval,
	});
}

export function bookQuery(id: string) {
	return queryOptions({
		queryKey: ["book", id],
		queryFn: async () =>
			(
				await api.GET("/books/{bookId}", {
					params: { path: { bookId: id } },
				})
			).data,
	});
}

/**
 * Where a book's cover is. The hash of the image goes along, which lets the
 * browser keep the picture for good: another cover is another address.
 */
export function coverUrl(
	book: { id: string; coverKey?: string },
	size: "small" | "large",
): string | undefined {
	if (!book.coverKey) {
		return undefined;
	}
	return `${API_BASE}/books/${book.id}/covers/${size}?v=${book.coverKey}`;
}

export function downloadUrl(file: { id: string }): string {
	return `${API_BASE}/files/${file.id}/download`;
}
