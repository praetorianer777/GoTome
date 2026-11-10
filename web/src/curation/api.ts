import {
	infiniteQueryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type CurationRun = components["schemas"]["CurationRun"];
export type CurationAction = components["schemas"]["CurationAction"];
export type CurationOutcome = components["schemas"]["CurationOutcome"];

export const RUN_KINDS = ["duplicates", "cleanup", "matches"] as const;
export const RUN_STATES = [
	"planned",
	"running",
	"done",
	"confirmed",
	"undone",
] as const;
export type RunKind = (typeof RUN_KINDS)[number];
export type RunState = (typeof RUN_STATES)[number];

export interface RunFilter {
	kind?: RunKind;
	state?: RunState;
}

/** The runs the person sees, the newest first, a page at a time. */
export function curationRunsQuery(filter: RunFilter) {
	return infiniteQueryOptions({
		queryKey: ["curation", "runs", filter],
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/curation/runs", {
					params: { query: { ...filter, cursor: pageParam || undefined } },
				})
			).data,
		initialPageParam: "",
		getNextPageParam: (last) => last?.nextCursor || undefined,
		// A run goes on after it is asked for: its state is looked at again
		// until none is running.
		refetchInterval: (query) =>
			query.state.data?.pages.some((p) =>
				p?.runs.some((r) => r.state === "running" || r.state === "planned"),
			)
				? 2000
				: false,
	});
}

/** A run's actions, in the order they were made, a page at a time. */
export function curationActionsQuery(runId: string, running = false) {
	return infiniteQueryOptions({
		queryKey: ["curation", "run", runId],
		refetchInterval: running ? 2000 : false,
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/curation/runs/{runId}", {
					params: {
						path: { runId },
						query: { cursor: pageParam || undefined },
					},
				})
			).data,
		initialPageParam: "",
		getNextPageParam: (last) => last?.nextCursor || undefined,
	});
}

function useAfter() {
	const queryClient = useQueryClient();
	return () => {
		queryClient.invalidateQueries({ queryKey: ["curation"] });
		queryClient.invalidateQueries({ queryKey: ["book"] });
		queryClient.invalidateQueries({ queryKey: ["books"] });
		queryClient.invalidateQueries({ queryKey: ["trash"] });
		queryClient.invalidateQueries({ queryKey: ["duplicates"] });
	};
}

/** Undoes every action of a run; those whose books changed since are left. */
export function useUndoRun() {
	return useMutation({
		mutationFn: async (runId: string) =>
			(
				await api.POST("/curation/runs/{runId}/undo", {
					params: { path: { runId } },
				})
			).data?.actions ?? [],
		onSuccess: useAfter(),
	});
}

/** Undoes one action. */
export function useUndoAction() {
	return useMutation({
		mutationFn: async (actionId: string) =>
			(
				await api.POST("/curation/actions/{actionId}/undo", {
					params: { path: { actionId } },
				})
			).data,
		onSuccess: useAfter(),
	});
}

/** Accepts a run that is done. */
export function useConfirmRun() {
	return useMutation({
		mutationFn: async (runId: string) =>
			api.POST("/curation/runs/{runId}/confirm", {
				params: { path: { runId } },
			}),
		onSuccess: useAfter(),
	});
}
