import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Library = components["schemas"]["LibraryResponse"];
export type NewLibrary = components["schemas"]["CreateLibraryRequest"];
export type LibraryChanges = components["schemas"]["UpdateLibraryRequest"];

/** The libraries the signed-in person may see, by name. */
export const librariesQuery = queryOptions({
	queryKey: ["libraries"],
	queryFn: async (): Promise<Library[]> =>
		(await api.GET("/libraries")).data?.libraries ?? [],
});

function useInvalidate() {
	const queryClient = useQueryClient();
	return () =>
		queryClient.invalidateQueries({ queryKey: librariesQuery.queryKey });
}

export function useCreateLibrary() {
	return useMutation({
		mutationFn: async (body: NewLibrary) =>
			(await api.POST("/libraries", { body })).data,
		onSuccess: useInvalidate(),
	});
}

export function useUpdateLibrary() {
	return useMutation({
		mutationFn: async ({
			id,
			changes,
		}: {
			id: string;
			changes: LibraryChanges;
		}) =>
			(
				await api.PATCH("/libraries/{libraryId}", {
					params: { path: { libraryId: id } },
					body: changes,
				})
			).data,
		onSuccess: useInvalidate(),
	});
}

export function useDeleteLibrary() {
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/libraries/{libraryId}", {
				params: { path: { libraryId: id } },
			});
		},
		onSuccess: useInvalidate(),
	});
}
