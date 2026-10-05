import {
	infiniteQueryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components, paths } from "@/api/schema";

export type DuplicatePair = components["schemas"]["DuplicatePair"];
export type Evidence = components["schemas"]["DuplicateEvidence"];
export type EvidenceKind = Evidence["kind"];
type ListQuery = NonNullable<
	paths["/duplicates"]["get"]["parameters"]["query"]
>;
export type PairState = NonNullable<ListQuery["state"]>;

export const EVIDENCE_KINDS: EvidenceKind[] = [
	"sha256",
	"content",
	"isbn",
	"title_author",
	"overlap",
];

export interface DuplicateParams {
	state?: PairState;
	kind?: EvidenceKind;
	/** The least score, in percent. */
	least?: number;
	library?: string;
}

const PAGE_SIZE = 30;

/** The pairs, the strongest first, a page at a time. */
export function duplicatesQuery(params: DuplicateParams) {
	return infiniteQueryOptions({
		queryKey: ["duplicates", params],
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/duplicates", {
					params: {
						query: {
							...params,
							limit: PAGE_SIZE,
							cursor: pageParam || undefined,
						},
					},
				})
			).data ?? { pairs: [] },
		initialPageParam: "",
		getNextPageParam: (last) => last.nextCursor || undefined,
	});
}

/** Keeps both books of a pair, or opens it again. */
export function useSetPairState() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async ({
			id,
			state,
		}: {
			id: string;
			state: "kept_both" | "open";
		}) =>
			api.PUT("/duplicates/{pairId}/state", {
				params: { path: { pairId: id } },
				body: { state },
			}),
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["duplicates"] }),
	});
}

/** The figures of overlap evidence, as the server writes them. */
export interface Overlap {
	jaccard: number;
	aInB: number;
	bInA: number;
}

export function overlapOf(detail: string): Overlap | undefined {
	const m = /jaccard=([\d.]+) a_in_b=([\d.]+) b_in_a=([\d.]+)/.exec(detail);
	return m
		? { jaccard: Number(m[1]), aInB: Number(m[2]), bInA: Number(m[3]) }
		: undefined;
}

/** Merges one book into another, which survives with what was chosen. */
export function useMergeBooks() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async ({ into, from, take }: { into: string; from: string; take: string[] }) =>
			(
				await api.POST("/books/{bookId}/merge", {
					params: { path: { bookId: into } },
					body: { from, take },
				})
			).data,
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["duplicates"] });
			queryClient.invalidateQueries({ queryKey: ["book"] });
			queryClient.invalidateQueries({ queryKey: ["books"] });
		},
	});
}
