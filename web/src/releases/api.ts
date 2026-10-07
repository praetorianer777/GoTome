import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type Tracker = components["schemas"]["Tracker"];
export type Release = components["schemas"]["Release"];
export type FollowKind = Tracker["kind"];

/** The authors and series the person follows. */
export const trackersQuery = queryOptions({
	queryKey: ["trackers"],
	queryFn: async () => (await api.GET("/trackers")).data?.trackers ?? [],
});

/** Books still to come of what the person follows, or those out lately. */
export const releasesQuery = (when: "upcoming" | "recent") =>
	queryOptions({
		queryKey: ["releases", when],
		queryFn: async () =>
			(await api.GET("/releases", { params: { query: { when } } })).data?.releases ?? [],
	});

/** Follows an author or a series by its name. */
export function useFollow() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: { kind: FollowKind; name: string }) =>
			(await api.POST("/trackers", { body })).data,
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["trackers"] });
			queryClient.invalidateQueries({ queryKey: ["releases"] });
		},
	});
}

/** Stops following. */
export function useUnfollow() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (id: string) => {
			await api.DELETE("/trackers/{trackerId}", { params: { path: { trackerId: id } } });
		},
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["trackers"] });
			queryClient.invalidateQueries({ queryKey: ["releases"] });
		},
	});
}

/**
 * The tracker of an author or a series, if the person follows it. Names are
 * compared as the server compares them: without case, accents or
 * punctuation, and an author's in either order, "Cornwell, Bernard" being
 * Bernard Cornwell.
 */
export function trackerOf(trackers: Tracker[] | undefined, kind: FollowKind, name: string) {
	const key = nameKey(kind, name);
	return trackers?.find((tr) => tr.kind === kind && nameKey(kind, tr.name) === key);
}

function nameKey(kind: FollowKind, name: string): string {
	let natural = name.trim();
	const [last, first, ...more] = natural.split(",");
	if (kind === "author" && first !== undefined && more.length === 0 && first.trim() !== "") {
		natural = `${first.trim()} ${(last ?? "").trim()}`;
	}
	return natural
		.normalize("NFKD")
		.replace(/\p{M}/gu, "")
		.toLowerCase()
		.replace(/[^\p{L}\p{N}]+/gu, " ")
		.trim();
}
