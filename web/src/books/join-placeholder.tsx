import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { api } from "@/api/client";
import { searchQuery } from "@/books/api";
import { FormError } from "@/components/form";
import { button } from "@/components/ui";
import { t } from "@/i18n";

const joinedKey = (book: string) => ["placeholder-join", book];

/** The action that undoes a join made in this browser into the book. */
function useJoinedFrom(book: string) {
	return useQuery({
		queryKey: joinedKey(book),
		queryFn: () => null as { actionId: string } | null,
		enabled: false,
		staleTime: Number.POSITIVE_INFINITY,
	});
}

function useJoin(placeholder: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (book: string) => {
			const { data, error } = await api.POST("/books/{bookId}/join-library-book", {
				params: { path: { bookId: placeholder } },
				body: { book },
			});
			if (error) {
				throw error;
			}
			return data;
		},
		onSuccess: (joined) => {
			queryClient.setQueryData(joinedKey(joined.book), { actionId: joined.actionId });
			queryClient.invalidateQueries({ queryKey: ["book"] });
			queryClient.invalidateQueries({ queryKey: ["books"] });
		},
	});
}

function useUndoPlaceholderJoin(book: string) {
	const queryClient = useQueryClient();
	return useMutation({
		mutationFn: async (actionId: string) =>
			(
				await api.POST("/placeholder-joins/{actionId}/undo", {
					params: { path: { actionId } },
				})
			).data,
		onSuccess: () => {
			queryClient.setQueryData(joinedKey(book), null);
			queryClient.invalidateQueries({ queryKey: ["book"] });
			queryClient.invalidateQueries({ queryKey: ["books"] });
		},
	});
}

/**
 * On a book read but not owned: search the libraries for the book it is and
 * join the two.
 */
export function JoinToLibraryBook({ placeholder }: { placeholder: string }) {
	const [open, setOpen] = useState(false);
	const [words, setWords] = useState("");
	const id = useId();
	const asked = words.trim();
	const found = useQuery({ ...searchQuery(asked), enabled: open && asked.length >= 2 });
	const join = useJoin(placeholder);
	const hits = (found.data?.books ?? []).filter((b) => !b.placeholder && b.id !== placeholder);

	if (!open) {
		return (
			<button type="button" className={`${button("secondary", "md")} self-start`} onClick={() => setOpen(true)}>
				{t("book.joinLibrary")}
			</button>
		);
	}
	return (
		<section aria-label={t("book.joinLibrary")} className="flex min-w-0 flex-col gap-2 text-sm">
			<p className="text-slate-600 dark:text-slate-400">{t("book.joinHint")}</p>
			<label htmlFor={id} className="sr-only">
				{t("book.joinSearch")}
			</label>
			<input
				id={id}
				type="search"
				value={words}
				onChange={(e) => setWords(e.target.value)}
				placeholder={t("book.joinSearch")}
				className="min-w-0 max-w-md rounded-md border border-slate-300 bg-white px-2 py-1 dark:border-slate-600 dark:bg-slate-900"
			/>
			{asked.length >= 2 && found.isSuccess && hits.length === 0 && <p>{t("book.joinNone")}</p>}
			<ul className="flex min-w-0 flex-col gap-1">
				{hits.map((b) => (
					<li key={b.id} className="flex min-w-0 flex-wrap items-center gap-2">
						<span className="min-w-0 break-words">{[b.title, b.authors.join(", ")].filter(Boolean).join(" · ")}</span>
						<button
							type="button"
							className={button("secondary")}
							aria-label={t("book.joinPick", { title: b.title })}
							disabled={join.isPending}
							onClick={() => join.mutate(b.id)}
						>
							{t("book.joinLibrary")}
						</button>
					</li>
				))}
			</ul>
			<FormError error={join.error} />
		</section>
	);
}

/** Shown on a book a placeholder was joined to from this browser, for a while. */
export function JoinedNotice({ book }: { book: string }) {
	const joined = useJoinedFrom(book);
	const undo = useUndoPlaceholderJoin(book);
	const action = joined.data?.actionId;
	if (!action) {
		return null;
	}
	const state = undo.data?.state;
	return (
		<div role="status" className="flex flex-wrap items-center gap-3 rounded-md border border-sky-200 bg-sky-50 px-3 py-2 text-sm dark:border-sky-900 dark:bg-sky-950">
			<span>{state === "conflict" ? t("book.joinKept") : t("book.joinedFrom")}</span>
			{state !== "conflict" && (
				<button type="button" className={button("secondary")} disabled={undo.isPending} onClick={() => undo.mutate(action)}>
					{t("book.joinUndo")}
				</button>
			)}
			<FormError error={undo.error} />
		</div>
	);
}
