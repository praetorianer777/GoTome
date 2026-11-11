import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type AutoCleanupCounts = components["schemas"]["AutoCleanupCounts"];
export type AutoMatchCounts = components["schemas"]["AutoMatchCounts"];
export type AutoMatchGroup = components["schemas"]["AutoMatchGroup"];
export type AutoRunStarted = components["schemas"]["AutoRunStarted"];

/** The kinds of clean-up that a run may apply by itself. */
export const AUTO_KINDS = ["placeholders", "titles", "authors", "series"] as const;
export type AutoKind = (typeof AUTO_KINDS)[number];

/** How many suggestions of each kind a run would apply by itself. */
export const autoCleanupQuery = queryOptions({
	queryKey: ["curation", "auto", "cleanup"],
	queryFn: async () => (await api.GET("/curation/cleanup")).data,
});

/** What a run over the matches that wait would do, and the groups of the rest. */
export const autoMatchesQuery = queryOptions({
	queryKey: ["curation", "auto", "matches"],
	queryFn: async () => (await api.GET("/curation/matches")).data,
});

function useAfter() {
	const queryClient = useQueryClient();
	return () => {
		queryClient.invalidateQueries({ queryKey: ["curation"] });
		queryClient.invalidateQueries({ queryKey: ["cleanup"] });
		queryClient.invalidateQueries({ queryKey: ["review"] });
	};
}

/** Queues a run, or a dry run, of the kinds of clean-up named. */
export function useStartAutoCleanup() {
	return useMutation({
		mutationFn: async (body: { kinds: AutoKind[]; dryRun: boolean }) =>
			(await api.POST("/curation/cleanup", { body })).data,
		onSuccess: useAfter(),
	});
}

/** Queues a run, or a dry run, over the matches, or over one group of them. */
export function useStartAutoMatches() {
	return useMutation({
		mutationFn: async (body: {
			dryRun: boolean;
			provider?: string;
			band?: number;
		}) => (await api.POST("/curation/matches", { body })).data,
		onSuccess: useAfter(),
	});
}
