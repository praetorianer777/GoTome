import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Library = components["schemas"]["LibraryResponse"];
export type Scan = components["schemas"]["Scan"];
export type NewLibrary = components["schemas"]["CreateLibraryRequest"];
export type LibraryChanges = components["schemas"]["UpdateLibraryRequest"];

/** Whether the scan is still waiting or running. */
export function scanIsActive(scan: Scan | undefined): boolean {
	return scan?.state === "queued" || scan?.state === "running";
}

/** Whether books are still arriving in the library. */
export function libraryIsBusy(library: Library): boolean {
	return scanIsActive(library.lastScan) || library.filesPending > 0;
}

const SCAN_POLL_MS = 2000;

/** The libraries the signed-in person may see, by name. */
export const librariesQuery = queryOptions({
	queryKey: ["libraries"],
	queryFn: async (): Promise<Library[]> =>
		(await api.GET("/libraries")).data?.libraries ?? [],
	// A scan reports nothing on its own, so while one is under way, or files
	// it found are still being read, the list is asked for again.
	refetchInterval: (query) =>
		query.state.data?.some(libraryIsBusy) ? SCAN_POLL_MS : false,
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

export function useScanLibrary() {
	return useMutation({
		mutationFn: async (id: string) =>
			(
				await api.POST("/libraries/{libraryId}/scans", {
					params: { path: { libraryId: id } },
				})
			).data,
		onSuccess: useInvalidate(),
	});
}

export type Member = components["schemas"]["Member"];

/** Who may see a private library besides administrators and its owner. */
export function membersQuery(libraryId: string) {
	return queryOptions({
		queryKey: ["libraries", libraryId, "members"],
		queryFn: async (): Promise<Member[]> =>
			(
				await api.GET("/libraries/{libraryId}/members", {
					params: { path: { libraryId } },
				})
			).data?.members ?? [],
	});
}

function useInvalidateMembers(libraryId: string) {
	const queryClient = useQueryClient();
	return () =>
		queryClient.invalidateQueries({
			queryKey: membersQuery(libraryId).queryKey,
		});
}

export function useAddMember(libraryId: string) {
	return useMutation({
		mutationFn: async (userId: string) => {
			await api.PUT("/libraries/{libraryId}/members/{userId}", {
				params: { path: { libraryId, userId } },
			});
		},
		onSuccess: useInvalidateMembers(libraryId),
	});
}

export function useRemoveMember(libraryId: string) {
	return useMutation({
		mutationFn: async (userId: string) => {
			await api.DELETE("/libraries/{libraryId}/members/{userId}", {
				params: { path: { libraryId, userId } },
			});
		},
		onSuccess: useInvalidateMembers(libraryId),
	});
}
