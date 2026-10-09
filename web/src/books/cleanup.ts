import {
	infiniteQueryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type CleanupSuggestion = components["schemas"]["CleanupSuggestion"];
export type CleanupKind = components["schemas"]["CleanupApplyRequest"]["kind"];
export type CleanupCounts = components["schemas"]["CleanupCounts"];

export const CLEANUP_KINDS: CleanupKind[] = [
	"authors",
	"placeholders",
	"clashes",
	"titles",
];

/** The suggestions of a kind, a page at a time, with the counts of every kind. */
export const cleanupQuery = (kind: CleanupKind) =>
	infiniteQueryOptions({
		queryKey: ["cleanup", kind],
		initialPageParam: "",
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/cleanup", {
					params: {
						query: { kind, ...(pageParam ? { cursor: pageParam } : {}) },
					},
				})
			).data,
		getNextPageParam: (last) => last?.nextCursor,
	});

/** Applies suggestions, or every one of a kind when picks are left out. */
export function useApplyCleanup() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: {
			kind: CleanupKind;
			picks?: { subject: string; to?: string }[];
		}) => (await api.POST("/cleanup/apply", { body })).data,
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["cleanup"] }),
	});
}

/** Leaves a suggestion off the page while what it is about stays as it is. */
export function useDismissCleanup() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: { kind: CleanupKind; subject: string }) => {
			await api.POST("/cleanup/dismiss", { body });
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: ["cleanup"] }),
	});
}
