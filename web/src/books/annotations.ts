import {
	infiniteQueryOptions,
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api, assetUrl } from "@/api/client";
import type { components } from "@/api/schema";

export type Annotation = components["schemas"]["AnnotationView"];
export type AnnotationCreate = components["schemas"]["AnnotationCreate"];
export type AnnotationRect = components["schemas"]["AnnotationRect"];

/** The colours a highlight may have, the first the default. */
export const COLORS = [
	"yellow",
	"orange",
	"red",
	"green",
	"blue",
	"purple",
	"gray",
] as const;
export type HighlightColor = (typeof COLORS)[number];

/** How each colour is drawn over a page; the same in every theme. */
export const COLOR_CSS: Record<HighlightColor, string> = {
	yellow: "#facc15",
	orange: "#fb923c",
	red: "#f87171",
	green: "#4ade80",
	blue: "#60a5fa",
	purple: "#c084fc",
	gray: "#9ca3af",
};

export function cssColor(name: string): string {
	return COLOR_CSS[name as HighlightColor] ?? COLOR_CSS.yellow;
}

const PAGE = 50;

/** A person's highlights of every book they see, searched by words, newest first. */
export function annotationsQuery(words: string) {
	return infiniteQueryOptions({
		queryKey: ["annotations", "all", words],
		initialPageParam: undefined as string | undefined,
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/annotations", {
					params: {
						query: { q: words || undefined, after: pageParam, limit: PAGE },
					},
				})
			).data ?? { annotations: [] },
		getNextPageParam: (last) => last.next,
	});
}

/** One book's highlights, in the order the book runs. */
export function bookAnnotationsQuery(bookId: string) {
	return queryOptions({
		queryKey: ["annotations", "book", bookId],
		queryFn: async () => {
			const found: Annotation[] = [];
			let after: string | undefined;
			do {
				const page = (
					await api.GET("/annotations", {
						params: { query: { book: bookId, after, limit: 200 } },
					})
				).data;
				found.push(...(page?.annotations ?? []));
				after = page?.next;
			} while (after);
			return inBookOrder(found);
		},
	});
}

/** Highlights as the book runs: by how far in, those of unknown place last. */
export function inBookOrder(list: Annotation[]): Annotation[] {
	return [...list].sort(
		(a, b) =>
			(a.fraction ?? 2) - (b.fraction ?? 2) ||
			a.createdAt.localeCompare(b.createdAt),
	);
}

/** Where a download of the highlights is: all, matching words, or of one book. */
export function exportUrl(
	format: "md" | "csv",
	filter: { q?: string; book?: string } = {},
): string {
	const query = new URLSearchParams();
	if (filter.q) query.set("q", filter.q);
	if (filter.book) query.set("book", filter.book);
	const rest = query.toString();
	return assetUrl(`/annotations/export.${format}${rest ? `?${rest}` : ""}`);
}

function useAfter() {
	const queryClient = useQueryClient();
	return () => queryClient.invalidateQueries({ queryKey: ["annotations"] });
}

/** Keeps a highlight made in a reader. */
export function useCreateAnnotation(bookId: string) {
	const after = useAfter();
	return useMutation({
		mutationFn: async (body: AnnotationCreate) => {
			return (
				await api.POST("/books/{bookId}/annotations", {
					params: { path: { bookId } },
					body,
				})
			).data;
		},
		onSuccess: after,
	});
}

/** Changes a highlight's note or colour. */
export function useChangeAnnotation() {
	const after = useAfter();
	return useMutation({
		mutationFn: async (change: {
			id: string;
			note?: string;
			color?: string;
		}) =>
			(
				await api.PATCH("/annotations/{annotationId}", {
					params: { path: { annotationId: change.id } },
					body: { note: change.note, color: change.color },
				})
			).data,
		onSuccess: after,
	});
}

export function useDeleteAnnotation() {
	const after = useAfter();
	return useMutation({
		mutationFn: async (id: string) =>
			api.DELETE("/annotations/{annotationId}", {
				params: { path: { annotationId: id } },
			}),
		onSuccess: after,
	});
}
