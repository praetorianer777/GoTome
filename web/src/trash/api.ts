import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type TrashedFile = components["schemas"]["TrashedFile"];

export const trashQuery = queryOptions({
	queryKey: ["trash"],
	queryFn: async () => (await api.GET("/trash")).data?.files ?? [],
});

function useAfter() {
	const queryClient = useQueryClient();
	return () => {
		queryClient.invalidateQueries({ queryKey: ["trash"] });
		queryClient.invalidateQueries({ queryKey: ["book"] });
		queryClient.invalidateQueries({ queryKey: ["books"] });
	};
}

/** Moves a file into its library's trash. */
export function useTrashFile() {
	return useMutation({
		mutationFn: async (id: string) =>
			api.POST("/files/{fileId}/trash", { params: { path: { fileId: id } } }),
		onSuccess: useAfter(),
	});
}

/** Brings a trashed file back to its book. */
export function useRestoreFile() {
	return useMutation({
		mutationFn: async (id: string) =>
			api.POST("/files/{fileId}/restore", { params: { path: { fileId: id } } }),
		onSuccess: useAfter(),
	});
}

/** Deletes a trashed file for good. */
export function usePurgeFile() {
	return useMutation({
		mutationFn: async (id: string) =>
			api.DELETE("/files/{fileId}", { params: { path: { fileId: id } } }),
		onSuccess: useAfter(),
	});
}
