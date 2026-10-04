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
	/** The rule tree as JSON, from filterTree. */
	filter?: string;
	sort: BookSort;
	order: BookOrder;
	/** "include" lists the books wished for that are not in the library yet. */
	placeholders?: "include";
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

/** How many of the books a list holds have each value of each field. */
export function facetsQuery(
	params: { library?: string; filter?: string },
	refetchInterval: number | false = false,
) {
	return queryOptions({
		queryKey: ["books", "facets", params],
		queryFn: async () =>
			(await api.GET("/books/facets", { params: { query: params } })).data
				?.facets ?? [],
		// The panel keeps showing the old counts while the new ones load,
		// rather than jumping to nothing between two clicks.
		placeholderData: (previous) => previous,
		refetchInterval,
	});
}

export type SearchHit = components["schemas"]["SearchHit"];

/** The books whose title, author or series looks like the words. */
export function searchQuery(words: string) {
	return queryOptions({
		queryKey: ["books", "search", words],
		queryFn: async () =>
			(await api.GET("/books/search", { params: { query: { q: words } } })).data
				?.books ?? [],
		// While the next answer is on its way, the last one stays in view.
		placeholderData: (previous) => previous,
	});
}

/** How often a book whose files are still being read is asked for again. */
const READING_POLL_MS = 3000;

export function bookQuery(id: string) {
	return queryOptions({
		queryKey: ["book", id],
		queryFn: async () =>
			(
				await api.GET("/books/{bookId}", {
					params: { path: { bookId: id } },
				})
			).data,
		// Until its files are read, what the page shows of a book is a guess.
		refetchInterval: (query) =>
			query.state.data?.files.some((f) => f.extractState === "pending")
				? READING_POLL_MS
				: false,
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
