import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Collection = components["schemas"]["Collection"];
export type CollectionDetail = components["schemas"]["CollectionDetail"];
export type CollectionRequest = components["schemas"]["CollectionRequest"];
export type CollectionAdd = components["schemas"]["CollectionAddRequest"];
export type Visibility = Collection["visibility"];

/** The person's own collections and everyone's shared ones; with a book, which hold it. */
export function collectionsQuery(book?: string) {
	return queryOptions({
		queryKey: ["collections", book ?? null],
		queryFn: async () =>
			(
				await api.GET("/collections", {
					params: { query: book ? { book } : {} },
				})
			).data?.collections ?? [],
	});
}

export function collectionQuery(id: string) {
	return queryOptions({
		queryKey: ["collection", id],
		queryFn: async () =>
			(
				await api.GET("/collections/{collectionId}", {
					params: { path: { collectionId: id } },
				})
			).data,
	});
}

/** Refreshes every list of collections and the one collection changed. */
function useRefresh() {
	const queryClient = useQueryClient();
	return (id?: string, detail?: CollectionDetail) => {
		if (id && detail) {
			queryClient.setQueryData(["collection", id], detail);
		} else if (id) {
			queryClient.invalidateQueries({ queryKey: ["collection", id] });
		}
		queryClient.invalidateQueries({ queryKey: ["collections"] });
	};
}

export function useCreateCollection() {
	const refresh = useRefresh();
	return useMutation({
		mutationFn: async (body: CollectionRequest) =>
			(await api.POST("/collections", { body })).data,
		onSuccess: (created) => refresh(created?.id, created),
	});
}

export function useUpdateCollection(id: string) {
	const refresh = useRefresh();
	return useMutation({
		mutationFn: async (body: CollectionRequest) =>
			(
				await api.PUT("/collections/{collectionId}", {
					params: { path: { collectionId: id } },
					body,
				})
			).data,
		onSuccess: (updated) => refresh(id, updated),
	});
}

export function useDeleteCollection(id: string) {
	const refresh = useRefresh();
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async () => {
			await api.DELETE("/collections/{collectionId}", {
				params: { path: { collectionId: id } },
			});
		},
		onSuccess: () => {
			queryClient.removeQueries({ queryKey: ["collection", id] });
			refresh();
		},
	});
}

/** Puts books at the end of a collection: those named, or a list's library and filter. */
export function useAddToCollection() {
	const refresh = useRefresh();
	return useMutation({
		mutationFn: async ({ id, ...body }: CollectionAdd & { id: string }) =>
			(
				await api.POST("/collections/{collectionId}/books", {
					params: { path: { collectionId: id } },
					body,
				})
			).data,
		onSuccess: (_, { id }) => refresh(id),
	});
}

export function useRemoveFromCollection() {
	const refresh = useRefresh();
	return useMutation({
		mutationFn: async ({ id, book }: { id: string; book: string }) => {
			await api.DELETE("/collections/{collectionId}/books/{bookId}", {
				params: { path: { collectionId: id, bookId: book } },
			});
		},
		onSuccess: (_, { id }) => refresh(id),
	});
}

/** Puts the collection's books, as the person sees them, into the order given. */
export function useReorderCollection(id: string) {
	const refresh = useRefresh();
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (books: string[]) =>
			(
				await api.PUT("/collections/{collectionId}/order", {
					params: { path: { collectionId: id } },
					body: { books },
				})
			).data,
		onMutate: (books) => {
			queryClient.setQueryData<CollectionDetail>(["collection", id], (c) =>
				c
					? {
							...c,
							bookList: books.flatMap(
								(b) => c.bookList.find((x) => x.id === b) ?? [],
							),
						}
					: c,
			);
		},
		onSuccess: (updated) => refresh(id, updated),
		onError: () => refresh(id),
	});
}

/** The order with the book at index moved one place up (-1) or down (1). */
export function moved(ids: string[], index: number, step: -1 | 1): string[] {
	const to = index + step;
	if (to < 0 || to >= ids.length) {
		return ids;
	}
	const out = ids.filter((_, i) => i !== index);
	out.splice(to, 0, ...ids.slice(index, index + 1));
	return out;
}
