import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components, paths } from "@/api/schema";
import { librariesQuery } from "@/libraries/api";

export type Job = components["schemas"]["JobView"];
export type JobGroup = NonNullable<
	NonNullable<paths["/jobs"]["get"]["parameters"]["query"]>["state"]
>;

const JOBS_POLL_MS = 2000;

/** Whether the queue may still run the job. */
export function jobIsActive(job: Job): boolean {
	return ["available", "scheduled", "retryable", "pending", "running"].includes(job.state);
}

/** The newest jobs, asked for again while any of them is still to run. */
export function jobsQuery(group: JobGroup | undefined) {
	return queryOptions({
		queryKey: ["jobs", group ?? "all"],
		queryFn: async (): Promise<Job[]> =>
			(await api.GET("/jobs", { params: { query: { state: group } } })).data?.jobs ?? [],
		refetchInterval: (query) =>
			query.state.data?.some(jobIsActive) ? JOBS_POLL_MS : false,
	});
}

function useAfter() {
	const queryClient = useQueryClient();
	return () => {
		queryClient.invalidateQueries({ queryKey: ["jobs"] });
		queryClient.invalidateQueries({ queryKey: ["book"] });
		queryClient.invalidateQueries({ queryKey: librariesQuery.queryKey });
	};
}

export function useRetryJob() {
	return useMutation({
		mutationFn: async (id: number) =>
			(await api.POST("/jobs/{jobId}/retry", { params: { path: { jobId: String(id) } } })).data,
		onSuccess: useAfter(),
	});
}

export function useCancelJob() {
	return useMutation({
		mutationFn: async (id: number) =>
			(await api.POST("/jobs/{jobId}/cancel", { params: { path: { jobId: String(id) } } })).data,
		onSuccess: useAfter(),
	});
}

/** Has one file read again. */
export function useRereadFile() {
	return useMutation({
		mutationFn: async (fileId: string) =>
			(await api.POST("/files/{fileId}/extraction", { params: { path: { fileId } } })).data,
		onSuccess: useAfter(),
	});
}

/** Has a library's files read again, those that failed or all. */
export function useRereadLibrary() {
	return useMutation({
		mutationFn: async ({ id, failedOnly }: { id: string; failedOnly: boolean }) =>
			(
				await api.POST("/libraries/{libraryId}/extractions", {
					params: { path: { libraryId: id } },
					body: { failedOnly },
				})
			).data,
		onSuccess: useAfter(),
	});
}
