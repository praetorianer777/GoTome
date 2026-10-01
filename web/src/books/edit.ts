import {
	queryOptions,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "@/api/client";
import type { components, paths } from "@/api/schema";
import type { BookDetail } from "@/books/api";

export type BookEdit = components["schemas"]["EditBookRequest"];
export type FieldState = components["schemas"]["FieldState"];
export type NameKind = NonNullable<
	NonNullable<paths["/books/names"]["get"]["parameters"]["query"]>["kind"]
>;

/** The fields a lock may be put on, in the order the form shows them. */
export const LOCKABLE = [
	"cover",
	"title",
	"subtitle",
	"contributors",
	"series",
	"publisher",
	"published",
	"language",
	"pageCount",
	"tags",
	"identifiers",
	"description",
] as const;
export type Lockable = (typeof LOCKABLE)[number];

/** Names already in use that begin as typed, to pick from or to add to. */
export function namesQuery(kind: NameKind, typed: string) {
	return queryOptions({
		queryKey: ["names", kind, typed],
		queryFn: async () =>
			(await api.GET("/books/names", { params: { query: { kind, q: typed } } }))
				.data?.names ?? [],
		enabled: typed.trim() !== "",
		placeholderData: (previous) => previous,
	});
}

/** Keeps every view of the book, and the lists it is in, current. */
function useAfter(id: string) {
	const queryClient = useQueryClient();
	return (book: BookDetail | undefined) => {
		if (book) {
			queryClient.setQueryData(["book", id], book);
		}
		queryClient.invalidateQueries({ queryKey: ["books"] });
		queryClient.invalidateQueries({ queryKey: ["names"] });
	};
}

export function useEditBook(id: string) {
	return useMutation({
		mutationFn: async (edit: BookEdit) =>
			(
				await api.PATCH("/books/{bookId}", {
					params: { path: { bookId: id } },
					body: edit,
				})
			).data,
		onSuccess: useAfter(id),
	});
}

export function usePutCover(id: string) {
	return useMutation({
		mutationFn: async (image: File) => {
			const form = new FormData();
			form.append("file", image);
			return (
				await api.PUT("/books/{bookId}/cover", {
					params: { path: { bookId: id } },
					// The client sends a FormData as it is, with its boundary.
					body: { file: "" },
					bodySerializer: () => form,
				})
			).data;
		},
		onSuccess: useAfter(id),
	});
}

export function useDeleteCover(id: string) {
	return useMutation({
		mutationFn: async () =>
			(
				await api.DELETE("/books/{bookId}/cover", {
					params: { path: { bookId: id } },
				})
			).data,
		onSuccess: useAfter(id),
	});
}
