import {
	infiniteQueryOptions,
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type CurationRun = components["schemas"]["CurationRun"];
export type CurationAction = components["schemas"]["CurationAction"];
export type CurationOutcome = components["schemas"]["CurationOutcome"];
export type DuplicateClassCount = components["schemas"]["DuplicateClassCount"];
export type DuplicateClass = DuplicateClassCount["class"];

/** How often the page asks again while a run works or is being undone. */
const WATCH_MS = 2000;

/** Whether a run is still being made or taken back by its job. */
export function isBusy(run: CurationRun): boolean {
	return run.undoing || run.state === "planned" || run.state === "running";
}

export const RUN_KINDS = ["duplicates", "cleanup", "matches", "organise"] as const;
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
		refetchInterval: (query) =>
			query.state.data?.pages.some((p) => p?.runs.some(isBusy))
				? WATCH_MS
				: false,
	});
}

/** A run's actions, in the order they were made, a page at a time. */
export function curationActionsQuery(runId: string, watch = false) {
	return infiniteQueryOptions({
		queryKey: ["curation", "run", runId],
		refetchInterval: watch ? WATCH_MS : false,
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

/**
 * Asks for every action of a run to be undone, which its job does; the run
 * says undoing until then. Those whose books changed since are left.
 */
export function useUndoRun() {
	return useMutation({
		mutationFn: async (runId: string) =>
			api.POST("/curation/runs/{runId}/undo", {
				params: { path: { runId } },
			}),
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

/** How many open pairs each class of duplicates has, for the person's libraries. */
export const duplicateClassesQuery = queryOptions({
	queryKey: ["curation", "duplicates"],
	queryFn: async () => (await api.GET("/curation/duplicates")).data?.classes ?? [],
});

/** Starts a run of the rules over the open pairs of the classes, or a dry run of them. */
export function useStartDuplicateRun() {
	const after = useAfter();
	return useMutation({
		mutationFn: async (v: { classes: DuplicateClass[]; dryRun: boolean }) =>
			(await api.POST("/curation/duplicates", { body: v })).data,
		onSuccess: after,
	});
}
