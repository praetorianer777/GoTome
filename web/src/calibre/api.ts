import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type CalibreImport = components["schemas"]["CalibreImport"];

/** An import of a Calibre library; while it runs, it is asked for again. */
export function calibreImportQuery(id: string | undefined) {
	return queryOptions({
		queryKey: ["calibre-import", id],
		queryFn: async () =>
			(
				await api.GET("/calibre/imports/{importId}", {
					params: { path: { importId: id ?? "" } },
				})
			).data,
		enabled: Boolean(id),
		refetchInterval: (query) => (query.state.data?.state === "running" ? 1000 : false),
	});
}

/** Reads the library's metadata.db and answers what an import would do. */
export function usePreviewCalibre() {
	return useMutation({
		mutationFn: async (library: string) =>
			(await api.POST("/calibre/imports", { body: { library } })).data,
	});
}

export function useStartCalibre(id: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: { ratings: boolean; columns: Record<string, string> }) =>
			(
				await api.POST("/calibre/imports/{importId}/start", {
					params: { path: { importId: id } },
					body,
				})
			).data,
		onSuccess: (started) => {
			queryClient.setQueryData(["calibre-import", id], started);
			queryClient.invalidateQueries({ queryKey: ["books"] });
		},
	});
}
