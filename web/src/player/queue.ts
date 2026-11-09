import {
	type QueryClient,
	queryOptions,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";

/** The audiobooks lined up after the one playing, as the server keeps them. */
export function queueQuery() {
	return queryOptions({
		queryKey: ["queue"],
		queryFn: async () => (await api.GET("/me/queue")).data?.books ?? [],
	});
}

/**
 * Replaces the queue. The answer is what the server kept: it leaves out a
 * book without audio or one the person may not see.
 */
export async function saveQueue(
	queryClient: QueryClient,
	books: string[],
): Promise<string[]> {
	queryClient.setQueryData(["queue"], books);
	const kept =
		(await api.PUT("/me/queue", { body: { books } })).data?.books ?? books;
	queryClient.setQueryData(["queue"], kept);
	return kept;
}

/**
 * Takes the book that plays next off the queue as the server has it now,
 * since another browser may have changed it.
 */
export async function takeNext(
	queryClient: QueryClient,
): Promise<string | undefined> {
	const queue = await queryClient.fetchQuery({ ...queueQuery(), staleTime: 0 });
	const [next, ...rest] = queue;
	if (next) await saveQueue(queryClient, rest);
	return next;
}

/** Where in the queue a book goes. */
export type Place = "next" | "last";

/** The queue, and what a person does to it. */
export function useQueue() {
	const queryClient = useQueryClient();
	const queue = useQuery(queueQuery());
	const books = queue.data ?? [];
	const change = useMutation({
		mutationFn: async (change: (current: string[]) => string[]) => {
			const current = await queryClient.fetchQuery({
				...queueQuery(),
				staleTime: 0,
			});
			return saveQueue(queryClient, change(current));
		},
	});
	const run = (change_: (current: string[]) => string[]) => change.mutate(change_);
	return {
		books,
		loading: queue.isPending,
		error: queue.error ?? change.error,
		has: (id: string) => books.includes(id),
		/** Adds books, in the order given, moving any already queued. */
		add: (ids: string[], place: Place = "last") =>
			run((current) => {
				const others = current.filter((b) => !ids.includes(b));
				return place === "next" ? [...ids, ...others] : [...others, ...ids];
			}),
		remove: (id: string) => run((current) => current.filter((b) => b !== id)),
		move: (id: string, step: -1 | 1) =>
			run((current) => {
				const at = current.indexOf(id);
				const to = at + step;
				if (at < 0 || to < 0 || to >= current.length) return current;
				const out = [...current];
				out.splice(to, 0, ...out.splice(at, 1));
				return out;
			}),
		clear: () => run(() => []),
	};
}
