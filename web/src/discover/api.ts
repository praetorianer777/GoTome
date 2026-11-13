import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type DiscoverItem = components["schemas"]["DiscoverItem"];
export type DiscoverList = components["schemas"]["DiscoverList"];
export type DiscoverProgress = NonNullable<DiscoverList["progress"]>;
export type DiscoverReason = DiscoverItem["reason"];

/** What the person is offered beyond their libraries, and how far the search is. */
export const discoverQuery = queryOptions({
	queryKey: ["discover"],
	queryFn: async (): Promise<DiscoverList> =>
		(await api.GET("/me/discover")).data ?? { state: "never", items: [] },
	// A run takes minutes; the page looks again while one is under way.
	refetchInterval: (query) => (query.state.data?.state === "running" ? 10_000 : false),
});

/** Asks for a run now. */
export function useDiscoverRun() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async () => {
			const { error } = await api.POST("/me/discover/run");
			if (error) throw error;
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["discover"] }),
	});
}

function leave(queryClient: ReturnType<typeof useQueryClient>, id: string) {
	queryClient.setQueryData<DiscoverList>(["discover"], (list) =>
		list ? { ...list, items: list.items.filter((i) => i.id !== id) } : list,
	);
}

/** "Not interested": the book is left out of the person's discoveries for good. */
export function useDismissDiscovered() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (id: string) => {
			await api.POST("/me/discover/{candidateId}/dismiss", {
				params: { path: { candidateId: id } },
			});
			return id;
		},
		onSuccess: (id) => leave(queryClient, id),
	});
}

/** "Add to wishlist": the book is wished for and leaves the page. */
export function useWishDiscovered() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (item: DiscoverItem) => {
			const { error } = await api.POST("/books/wishes", { body: item.wish });
			if (error) throw error;
			return item.id;
		},
		onSuccess: (id) => {
			leave(queryClient, id);
			queryClient.invalidateQueries({ queryKey: ["books"] });
		},
	});
}
