import {
	infiniteQueryOptions,
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { ApiError, api } from "@/api/client";
import type { components } from "@/api/schema";
import { errorMessage } from "@/components/form";

export type SmartShelf = components["schemas"]["SmartShelf"];
export type SmartShelfRequest = components["schemas"]["SmartShelfRequest"];

const PAGE_SIZE = 60;

export const smartShelvesQuery = queryOptions({
	queryKey: ["smart-shelves"],
	queryFn: async () => (await api.GET("/smart-shelves")).data?.shelves ?? [],
});

export function smartShelfQuery(id: string) {
	return queryOptions({
		queryKey: ["smart-shelves", id],
		queryFn: async () =>
			(
				await api.GET("/smart-shelves/{shelfId}", {
					params: { path: { shelfId: id } },
				})
			).data,
	});
}

/** What is on the shelf for the signed-in person now, a page at a time. */
export function smartShelfBooksQuery(id: string) {
	return infiniteQueryOptions({
		// Under "books", so that a change of a book or of the person's status
		// refreshes it as it does the library.
		queryKey: ["books", "smart-shelf", id],
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/smart-shelves/{shelfId}/books", {
					params: {
						path: { shelfId: id },
						query: { limit: PAGE_SIZE, cursor: pageParam || undefined },
					},
				})
			).data ?? { books: [] },
		initialPageParam: "",
		getNextPageParam: (last) => last.nextCursor || undefined,
	});
}

/** How many books the person may see match a rule tree. */
export function bookCountQuery(filter: string) {
	return queryOptions({
		queryKey: ["books", "count", filter],
		queryFn: async () =>
			(await api.GET("/books/count", { params: { query: { filter } } })).data
				?.count ?? 0,
		placeholderData: (previous) => previous,
		retry: false,
	});
}

/** What is wrong with a rule tree or a shelf, in the server's words. */
export function shelfProblem(error: unknown): string {
	if (error instanceof ApiError && Object.keys(error.fields).length > 0) {
		return Object.values(error.fields).join(" ");
	}
	return errorMessage(error);
}

function useRefresh() {
	const queryClient = useQueryClient();
	return (id?: string) => {
		queryClient.invalidateQueries({ queryKey: ["smart-shelves"] });
		if (id) {
			queryClient.invalidateQueries({ queryKey: ["books", "smart-shelf", id] });
		}
	};
}

export function useCreateSmartShelf() {
	const refresh = useRefresh();
	return useMutation({
		mutationFn: async (body: SmartShelfRequest) =>
			(await api.POST("/smart-shelves", { body })).data,
		onSuccess: () => refresh(),
	});
}

export function useUpdateSmartShelf(id: string) {
	const refresh = useRefresh();
	return useMutation({
		mutationFn: async (body: SmartShelfRequest) =>
			(
				await api.PUT("/smart-shelves/{shelfId}", {
					params: { path: { shelfId: id } },
					body,
				})
			).data,
		onSuccess: () => refresh(id),
	});
}

export function useDeleteSmartShelf(id: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async () => {
			await api.DELETE("/smart-shelves/{shelfId}", {
				params: { path: { shelfId: id } },
			});
		},
		onSuccess: () => {
			queryClient.removeQueries({ queryKey: ["smart-shelves", id] });
			queryClient.invalidateQueries({ queryKey: ["smart-shelves"] });
		},
	});
}
