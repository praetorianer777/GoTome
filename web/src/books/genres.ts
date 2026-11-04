import {
	infiniteQueryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components } from "@/api/schema";

export type GenreSuggestion = components["schemas"]["GenreSuggestion"];

/** The genres of the fixed list, in its order: fiction, then non-fiction. */
export const GENRES = [
	"contemporary",
	"classics",
	"crime",
	"thriller",
	"horror",
	"science-fiction",
	"fantasy",
	"dystopia",
	"romance",
	"erotica",
	"historical",
	"family-saga",
	"war",
	"adventure",
	"western",
	"humour",
	"short-stories",
	"myths",
	"poetry",
	"drama",
	"comics",
	"children",
	"young-adult",
	"biography",
	"true-crime",
	"history",
	"society",
	"business",
	"law",
	"philosophy",
	"religion",
	"psychology",
	"science",
	"health",
	"technology",
	"computing",
	"self-help",
	"cooking",
	"travel",
	"nature",
	"arts",
	"language",
	"sport",
	"hobbies",
] as const;

export type Genre = (typeof GENRES)[number];

/** Whether a value is a genre of the list. */
export function isGenre(value: string): value is Genre {
	return (GENRES as readonly string[]).includes(value);
}

/** The suggestions that wait for a person, of one genre when named, a page at a time. */
export const genreSuggestionsQuery = (genre?: Genre) =>
	infiniteQueryOptions({
		queryKey: ["genre-suggestions", genre ?? ""],
		initialPageParam: "",
		queryFn: async ({ pageParam }) =>
			(
				await api.GET("/genres/suggestions", {
					params: {
						query: {
							...(genre ? { genre } : {}),
							...(pageParam ? { cursor: pageParam } : {}),
						},
					},
				})
			).data,
		getNextPageParam: (last) => last?.nextCursor || undefined,
	});

/** Accepts or rejects a genre of a book. */
export function useDecideGenre() {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (body: {
			bookId: string;
			genre: string;
			state: "accepted" | "rejected";
		}) => {
			await api.PUT("/books/{bookId}/genres/{genre}", {
				params: { path: { bookId: body.bookId, genre: body.genre } },
				body: { state: body.state },
			});
		},
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["genre-suggestions"] }),
	});
}
